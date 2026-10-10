//go:build linux || darwin

package collect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func storeDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
func openTestStore(t *testing.T, dir string, budget int64) *Store {
	t.Helper()
	s, err := OpenStore(dir, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStoreImmutableRoundtrip(t *testing.T) {
	dir := storeDir(t)
	s := openTestStore(t, dir, 1<<20)
	ref, err := s.Put([]byte(`{"safe":1}`))
	if err != nil {
		t.Fatal(err)
	}
	ref2, err := s.Put([]byte(`{"safe":1}`))
	if err != nil || ref != ref2 {
		t.Fatal("idempotency lost")
	}
	ref3, err := s.Put([]byte(`{"safe":2}`))
	if err != nil || ref3 == ref {
		t.Fatal("version overwritten")
	}
	if _, err := OpenStore(dir, 1<<20); err == nil {
		t.Fatal("second writer accepted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, dir, 1<<20)
	b, err := s.Get(ref)
	if err != nil || string(b) != `{"safe":1}` {
		t.Fatalf("lost original: %s %v", b, err)
	}
	for _, ref := range []string{"../secret", "/tmp/x", "", strings.Repeat("f", 63)} {
		if _, err := s.Get(ref); err == nil {
			t.Fatal("unsafe ref accepted")
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		info, _ := entry.Info()
		if info.Mode().Perm() != 0600 {
			t.Fatal("non-private file")
		}
	}
}

func TestStoreRejectsUnsafeFilesystem(t *testing.T) {
	for _, kind := range []string{"permissions", "symlink", "hardlink", "git", "lock_symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := storeDir(t)
			switch kind {
			case "permissions":
				_ = os.Chmod(dir, 0755)
			case "symlink":
				p := filepath.Join(storeDir(t), "link")
				if err := os.Symlink(dir, p); err != nil {
					t.Fatal(err)
				}
				dir = p
			case "hardlink":
				p := filepath.Join(storeDir(t), "outside")
				_ = os.WriteFile(p, []byte("secret"), 0600)
				if err := os.Link(p, filepath.Join(dir, ".lock")); err != nil {
					t.Fatal(err)
				}
			case "git":
				_ = os.Mkdir(filepath.Join(dir, ".git"), 0700)
			case "lock_symlink":
				_ = os.Symlink(filepath.Join(storeDir(t), "absent"), filepath.Join(dir, ".lock"))
			}
			if s, err := OpenStore(dir, 1<<20); err == nil {
				_ = s.Close()
				t.Fatal("unsafe filesystem accepted")
			}
		})
	}
}

func TestStoreCorruptAndInterrupted(t *testing.T) {
	dir := storeDir(t)
	s := openTestStore(t, dir, 4096)
	ref, err := s.Put([]byte(`{"safe":1}`))
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, ref+".json")
	if err := os.WriteFile(name, []byte(`{"schema":1,"payload":{"safe":99},"checksum":"wrong"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put([]byte(`{"safe":1}`)); err == nil {
		t.Fatal("corrupt object overwritten")
	}
	_ = s.Close()
	if reopened, err := OpenStore(dir, 4096); err == nil {
		_ = reopened.Close()
		t.Fatal("corrupt reopen accepted")
	}
	dir = storeDir(t)
	if err := os.WriteFile(filepath.Join(dir, ".pending-11111111-1111-4111-8111-111111111111"), []byte(strings.Repeat("x", 4000)), 0600); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, dir, 4096)
	if _, err := s.Put([]byte(`{"safe":1}`)); err == nil {
		t.Fatal("interrupted bytes omitted from budget")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatal("leftover deleted or partial published")
	}
}

func TestStoreQuotaSurvivesReopen(t *testing.T) {
	dir := storeDir(t)
	s := openTestStore(t, dir, 1<<20)
	at := time.Date(2026, 10, 8, 7, 0, 0, 0, time.UTC)
	if err := s.ReserveRequest(at); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s = openTestStore(t, dir, 1<<20)
	if err := s.ReserveRequest(at); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".quota-2026-10-08"))
	if err != nil || string(b) != "1\n2\n" {
		t.Fatal("quota reset on reopen")
	}
	_ = s.Close()
	var full strings.Builder
	for i := 1; i <= 10000; i++ {
		fmt.Fprintf(&full, "%d\n", i)
	}
	_ = os.WriteFile(filepath.Join(dir, ".quota-2026-10-08"), []byte(full.String()), 0600)
	s = openTestStore(t, dir, 1<<20)
	if err := s.ReserveRequest(at); err == nil {
		t.Fatal("quota exceeded")
	}
	if err := s.ReserveRequest(at.Add(24 * time.Hour)); err != nil {
		t.Fatal("next UTC day blocked")
	}
	_ = s.Close()
	_ = os.WriteFile(filepath.Join(dir, ".quota-2026-10-09"), []byte("1\n2"), 0600)
	if reopened, err := OpenStore(dir, 1<<20); err == nil {
		_ = reopened.Close()
		t.Fatal("partial journal reset")
	}
}

func TestStoreConcurrentAndClosed(t *testing.T) {
	s := openTestStore(t, storeDir(t), 1<<20)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Put([]byte(`{"safe":1}`)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	_ = s.Close()
	if _, err := s.Put([]byte(`{}`)); err == nil {
		t.Fatal("closed store accepted write")
	}
	if err := s.ReserveRequest(time.Now()); err == nil {
		t.Fatal("closed store accepted quota")
	}
}

func TestStoreJSONTextPreserved(t *testing.T) {
	s := openTestStore(t, storeDir(t), 1<<20)
	payload := []byte(`{"text":"<test>&"}`)
	ref, err := s.Put(payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ref)
	if err != nil || string(got) != string(payload) {
		t.Fatal("JSON escaping broke content checksum", err)
	}
}

func TestStoreFileLimitCannotPoisonReopen(t *testing.T) {
	for _, action := range []string{"object", "new_quota"} {
		t.Run(action, func(t *testing.T) {
			dir := storeDir(t)
			// Exercise the production constructor/scan/publication path with a small
			// resource limit rather than creating 100000 files per regression run.
			s, err := openStore(dir, 1<<20, 4)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			payload := []byte(`{"safe":1}`)
			if _, err := s.Put(payload); err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 10, 8, 7, 0, 0, 0, time.UTC)
			for _, d := range []time.Duration{0, 24 * time.Hour} {
				if err := s.ReserveRequest(at.Add(d)); err != nil {
					t.Fatal(err)
				}
			}
			// lock, old object, two daily journals: exactly at capacity.
			if action == "object" {
				_, err = s.Put([]byte(`{"safe":2}`))
			} else {
				err = s.ReserveRequest(at.Add(48 * time.Hour))
			}
			if err == nil {
				t.Fatal("accepted new entry beyond file limit")
			}
			if _, err := s.Put(payload); err != nil {
				t.Fatal("idempotent write blocked")
			}
			if err := s.ReserveRequest(at); err != nil {
				t.Fatal("append at capacity blocked")
			}
			_ = s.Close()
			reopened, err := openStore(dir, 1<<20, 4)
			if err != nil {
				t.Fatal("valid write poisoned reopen", err)
			}
			_ = reopened.Close()
		})
	}
}

func TestStoreLockCreationHonorsFileCapacity(t *testing.T) {
	dir := storeDir(t)
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf(".pending-%08d-1111-4111-8111-111111111111", i)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if s, err := openStore(dir, 1<<20, 4); err == nil {
		_ = s.Close()
		t.Fatal("full directory accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 4 {
		t.Fatal("failed constructor exceeded capacity")
	}
}
