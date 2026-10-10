//go:build linux || darwin

package collect

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const maxRecord = 4 << 20
const maxEnvelope = maxRecord + 1024
const dailyRequests = 10000

var errStore = errors.New("research_store_unavailable")
var errBudget = errors.New("research_budget_exhausted")
var refPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var pendingPattern = regexp.MustCompile(`^\.pending-[0-9a-f-]{36}$`)

// Store persists caller-whitelisted JSON, not arbitrary private API responses.
// Use a dedicated owner-only local volume outside git and all web roots.
// OS locking/link publication require a local filesystem with those semantics.
type Store struct {
	mu       sync.Mutex
	root     *os.Root
	lock     *os.File
	maxBytes int64
	maxFiles int
	// Shared by every archive client using this Store, not per client.
	requestGate chan struct{}
	nextRequest time.Time // accessed only with requestGate held
	// Durable request state below is guarded by mu.
	requestDeadline time.Time
	requestBytes    int64
	requestActive   bool
	requestBroken   bool
}

type storedRecord struct {
	Schema   int             `json:"schema"`
	Payload  json.RawMessage `json:"payload"`
	Checksum string          `json:"checksum"`
}

func OpenStore(dir string, maxBytes int64) (*Store, error) {
	return openStore(dir, maxBytes, 100000)
}

func openStore(dir string, maxBytes int64, maxFiles int) (*Store, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == string(filepath.Separator) || maxBytes <= 0 || maxBytes > 1<<30 || maxFiles < 1 || maxFiles > 100000 {
		return nil, errStore
	}
	// Reject symlink aliases and git ancestors, including linked-worktree .git files.
	for p := dir; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errStore
		}
		if _, err := os.Lstat(filepath.Join(p, ".git")); !errors.Is(err, os.ErrNotExist) {
			return nil, errStore
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errStore
	}
	s := &Store{root: root, maxBytes: maxBytes, maxFiles: maxFiles, requestGate: make(chan struct{}, 1)}
	ok := false
	defer func() {
		if !ok {
			_ = s.Close()
		}
	}()
	directory, err := root.Stat(".")
	if err != nil || directory.Mode().Perm() != 0700 {
		return nil, errStore
	}
	var stat unix.Stat_t
	df, err := root.Open(".")
	if err != nil {
		return nil, errStore
	}
	err = unix.Fstat(int(df.Fd()), &stat)
	_ = df.Close()
	if err != nil || stat.Uid != uint32(os.Geteuid()) {
		return nil, errStore
	}
	if _, err := root.Lstat(".lock"); errors.Is(err, os.ErrNotExist) {
		// Creating the lock is a directory mutation too. Do not poison a full
		// interrupted store before its normal scan can reject this open.
		dirFile, openErr := root.Open(".")
		if openErr != nil {
			return nil, errStore
		}
		entries, readErr := dirFile.ReadDir(maxFiles)
		_ = dirFile.Close()
		if readErr != nil && readErr != io.EOF {
			return nil, errStore
		}
		if len(entries) >= maxFiles {
			return nil, errBudget
		}
	} else if err != nil {
		return nil, errStore
	}
	lock, err := root.OpenFile(".lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, errStore
	}
	s.lock = lock
	if !privateFile(lock) || unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return nil, errStore
	}
	if _, err := s.scan(true, 0); err != nil {
		return nil, err
	}
	if err := s.loadCooldown(); err != nil {
		return nil, err
	}
	ok = true
	return s, nil
}

func privateFile(f *os.File) bool {
	var st unix.Stat_t
	return unix.Fstat(int(f.Fd()), &st) == nil && st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&07777 == 0600 && st.Uid == uint32(os.Geteuid()) && st.Nlink == 1
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var failed bool
	if s.lock != nil {
		failed = s.lock.Close() != nil
		s.lock = nil
	}
	if s.root != nil {
		failed = s.root.Close() != nil || failed
		s.root = nil
	}
	if failed {
		return errStore
	}
	return nil
}

func (s *Store) read(name string, limit int64) ([]byte, error) {
	f, err := s.root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errStore
	}
	defer f.Close()
	if !privateFile(f) {
		return nil, errStore
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errStore
	}
	return b, nil
}

func (s *Store) scan(validate bool, newEntries int) (int64, error) {
	dir, err := s.root.Open(".")
	if err != nil {
		return 0, errStore
	}
	defer dir.Close()
	entries, err := dir.ReadDir(s.maxFiles + 1)
	if err != nil && err != io.EOF || len(entries) > s.maxFiles {
		return 0, errStore
	}
	if len(entries)+newEntries > s.maxFiles {
		return 0, errBudget
	}
	var used int64
	for _, entry := range entries {
		name := entry.Name()
		f, err := s.root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return 0, errStore
		}
		info, err := f.Stat()
		safe := privateFile(f)
		_ = f.Close()
		if err != nil || !safe {
			return 0, errStore
		}
		used += info.Size()
		if used > s.maxBytes {
			return 0, errBudget
		}
		switch {
		case name == ".requests":
			if info.Size() > maxRequestJournal {
				return 0, errStore
			}
		case name == ".lock":
			if info.Size() != 0 {
				return 0, errStore
			}
		case pendingPattern.MatchString(name): // interrupted writes consume budget, never promoted/deleted
		case strings.HasPrefix(name, ".quota-"):
			date := strings.TrimPrefix(name, ".quota-")
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return 0, errStore
			}
			if validate {
				if _, err := s.quota(name); err != nil {
					return 0, err
				}
			}
		case strings.HasSuffix(name, ".json") && refPattern.MatchString(strings.TrimSuffix(name, ".json")):
			if info.Size() > maxEnvelope {
				return 0, errStore
			}
			if validate {
				if _, err := s.get(strings.TrimSuffix(name, ".json")); err != nil {
					return 0, err
				}
			}
		default:
			return 0, errStore
		}
	}
	return used, nil
}

func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

func (s *Store) Get(ref string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil || !refPattern.MatchString(ref) {
		return nil, errStore
	}
	return s.get(ref)
}
func (s *Store) get(ref string) ([]byte, error) {
	b, err := s.read(ref+".json", maxEnvelope)
	if err != nil {
		return nil, err
	}
	dup := json.NewDecoder(bytes.NewReader(b))
	if uniqueJSON(dup, 0) != nil {
		return nil, errStore
	}
	var r storedRecord
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || r.Schema != 1 || len(r.Payload) > maxRecord || r.Checksum != ref || digest(r.Payload) != ref {
		return nil, errStore
	}
	return r.Payload, nil
}

func (s *Store) Put(payload []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil || len(payload) == 0 || len(payload) > maxRecord {
		return "", errStore
	}
	d := json.NewDecoder(bytes.NewReader(payload))
	if uniqueJSON(d, 0) != nil {
		return "", errStore
	}
	var compact bytes.Buffer
	if json.Compact(&compact, payload) != nil {
		return "", errStore
	}
	payload = compact.Bytes()
	ref := digest(payload)
	if _, err := s.root.Lstat(ref + ".json"); err == nil {
		if _, err := s.get(ref); err != nil {
			return "", err
		}
		return ref, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", errStore
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(storedRecord{1, payload, ref}); err != nil {
		return "", errStore
	}
	b := encoded.Bytes()
	// Reserve room for both temporary and published names, including a crash
	// in the brief interval between Link and Remove.
	used, err := s.scan(false, 2)
	if err != nil {
		return "", err
	}
	if used+int64(len(b)) > s.maxBytes {
		return "", errBudget
	}
	temp := ".pending-" + uuid.NewString()
	f, err := s.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return "", errStore
	}
	defer func() { _ = f.Close(); _ = s.root.Remove(temp) }()
	if _, err = f.Write(b); err != nil {
		return "", errStore
	}
	if f.Sync() != nil || f.Close() != nil {
		return "", errStore
	}
	// Link is atomic and refuses an existing destination (unlike Rename).
	if s.root.Link(temp, ref+".json") != nil {
		return "", errStore
	}
	if s.root.Remove(temp) != nil || s.syncDir() != nil {
		return "", errStore
	}
	return ref, nil
}

func (s *Store) syncDir() error {
	f, err := s.root.Open(".")
	if err != nil {
		return errStore
	}
	defer f.Close()
	if f.Sync() != nil {
		return errStore
	}
	return nil
}

func (s *Store) quota(name string) (int, error) {
	b, err := s.read(name, 60000)
	if err != nil {
		return 0, err
	}
	if len(b) == 0 {
		return 0, nil
	} // crash before reservation: no request was permitted
	if b[len(b)-1] != '\n' {
		return 0, errStore
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) > dailyRequests {
		return 0, errStore
	}
	for i, line := range lines {
		if line != strconv.Itoa(i+1) {
			return 0, errStore
		}
	}
	return len(lines), nil
}

// ReserveRequest persists before the caller may send. Crashes can over-count,
// never reset consumed quota. Keep all daily journals; deleting them resets it.
func (s *Store) ReserveRequest(at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil || at.IsZero() || at.Year() < 1970 || at.Year() > 9999 {
		return errStore
	}
	name := ".quota-" + at.UTC().Format("2006-01-02")
	count := 0
	newEntries := 1
	if _, err := s.root.Lstat(name); err == nil {
		newEntries = 0
		count, err = s.quota(name)
		if err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errStore
	}
	if count >= dailyRequests {
		return errBudget
	}
	line := []byte(strconv.Itoa(count+1) + "\n")
	used, err := s.scan(false, newEntries)
	if err != nil {
		return err
	}
	if used+int64(len(line)) > s.maxBytes {
		return errBudget
	}
	f, err := s.root.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return errStore
	}
	defer f.Close()
	if !privateFile(f) {
		return errStore
	}
	if _, err = f.Write(line); err != nil {
		return errStore
	}
	if f.Sync() != nil || f.Close() != nil || s.syncDir() != nil {
		return errStore
	}
	return nil
}
