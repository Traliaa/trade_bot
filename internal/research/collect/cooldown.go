//go:build linux || darwin

package collect

import (
	"errors"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const maxRequestJournal = 64 << 20

// loadCooldown runs under the directory's exclusive writer lock at construction.
// A response may contain a long Retry-After. If its receipt was interrupted,
// guessing a retry time is unsafe, so an unmatched begin requires operator review.
func (s *Store) loadCooldown() error {
	if _, err := s.root.Lstat(".requests"); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return errStore
	}
	b, err := s.read(".requests", maxRequestJournal)
	if err != nil || len(b) == 0 || b[len(b)-1] != '\n' {
		return errStore
	}
	active := false
	var deadline time.Time
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		parts := strings.Split(line, " ")
		if len(parts) != 3 || parts[2] != digest([]byte(parts[0]+" "+parts[1])) {
			return errStore
		}
		if (!active && parts[0] != "B") || (active && parts[0] != "F") {
			return errStore
		}
		at, err := time.Parse(time.RFC3339Nano, parts[1])
		if err != nil || !validRequestTime(at) || parts[1] != at.UTC().Format(time.RFC3339Nano) || at.Before(deadline) {
			return errStore
		}
		deadline = at
		active = !active
	}
	if active {
		return errStore
	}
	s.requestBytes = int64(len(b))
	s.requestDeadline, s.nextRequest = deadline, deadline
	return nil
}

func validRequestTime(at time.Time) bool {
	return !at.IsZero() && at.Year() >= 1970 && at.Year() <= 9999
}

func (s *Store) appendRequest(kind string, until time.Time) error {
	if s.root == nil || s.requestBroken || !validRequestTime(until) {
		s.requestBroken = true
		return errStore
	}
	if until.Before(s.requestDeadline) {
		until = s.requestDeadline
	}
	line := kind + " " + until.UTC().Format(time.RFC3339Nano)
	b := []byte(line + " " + digest([]byte(line)) + "\n")
	newEntries := 0
	if s.requestBytes == 0 {
		newEntries = 1
	}
	used, err := s.scan(false, newEntries)
	if err != nil {
		s.requestBroken = true
		return err
	}
	if used+int64(len(b)) > s.maxBytes || s.requestBytes+int64(len(b)) > maxRequestJournal {
		s.requestBroken = true
		return errBudget
	}
	flags := os.O_WRONLY | os.O_APPEND | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if s.requestBytes == 0 {
		flags |= os.O_CREATE | os.O_EXCL
	}
	f, err := s.root.OpenFile(".requests", flags, 0600)
	if err != nil {
		s.requestBroken = true
		return errStore
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !privateFile(f) || info.Size() != s.requestBytes {
		s.requestBroken = true
		return errStore
	}
	n, err := f.Write(b)
	if err != nil || n != len(b) {
		s.requestBroken = true
		return errStore
	}
	if f.Sync() != nil || f.Close() != nil || s.syncDir() != nil {
		s.requestBroken = true
		return errStore
	}
	s.requestBytes += int64(len(b))
	s.requestDeadline = until
	return nil
}

func (s *Store) beginAttempt(at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requestActive || !validRequestTime(at) {
		s.requestBroken = true
		return errStore
	}
	if err := s.appendRequest("B", at.Add(time.Second)); err != nil {
		return err
	}
	s.requestActive = true
	return nil
}

func (s *Store) finishAttempt(until time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.requestActive {
		s.requestBroken = true
		return errStore
	}
	if err := s.appendRequest("F", until); err != nil {
		return err
	}
	s.requestActive = false
	return nil
}
