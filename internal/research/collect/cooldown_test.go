//go:build linux || darwin

package collect

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveDurableCooldown(t *testing.T) {
	for _, delay := range []string{"", "120"} {
		t.Run("retry_"+delay, func(t *testing.T) {
			calls := 0
			c, dir := archiveFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				r := archiveResponse(200, `{"code":"0","data":[]}`)
				if delay != "" {
					r.StatusCode = 429
					r.Header.Set("Retry-After", delay)
				}
				return r, nil
			})
			_, _ = c.Get(context.Background(), candlesPath, archiveQuery())
			at := c.now()
			_ = c.store.Close()
			c.store = openTestStore(t, dir, 1<<20)
			var waited time.Duration
			c.wait = func(_ context.Context, d time.Duration) error {
				if d > 0 {
					waited += d
				}
				return nil
			}
			_, err := c.Get(context.Background(), candlesPath, archiveQuery())
			if delay != "" {
				if calls != 1 || err == nil || err.Error() != "archive_retry_later" {
					t.Fatalf("restart bypassed server cooldown: calls=%d err=%v", calls, err)
				}
			} else if waited < time.Second || calls != 2 || err != nil {
				t.Fatalf("restart bypassed minimum spacing at %v: wait=%v calls=%d err=%v", at, waited, calls, err)
			}
		})
	}
}

func TestCooldownIncompleteOrCorruptFailsClosed(t *testing.T) {
	for _, kind := range []string{"begin", "empty", "partial", "modified", "finish_without_begin"} {
		t.Run(kind, func(t *testing.T) {
			dir := storeDir(t)
			s := openTestStore(t, dir, 1<<20)
			at := time.Date(2026, 10, 9, 7, 0, 0, 0, time.UTC)
			if err := s.beginAttempt(at); err != nil {
				t.Fatal(err)
			}
			if kind != "begin" {
				if err := s.finishAttempt(at.Add(120 * time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			_ = s.Close()
			path := filepath.Join(dir, ".requests")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "empty":
				data = nil
			case "partial":
				data = data[:len(data)-1]
			case "modified":
				data = []byte(strings.Replace(string(data), "2026", "2027", 1))
			case "finish_without_begin":
				data = []byte(strings.SplitAfter(string(data), "\n")[1])
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if s, err := OpenStore(dir, 1<<20); err == nil {
				_ = s.Close()
				t.Fatal("interrupted/corrupt journal accepted")
			}
		})
	}
}

func TestCooldownMonotoneAndBudget(t *testing.T) {
	at := time.Date(2026, 10, 9, 7, 0, 0, 0, time.UTC)
	dir := storeDir(t)
	s := openTestStore(t, dir, 1<<20)
	if err := s.beginAttempt(at); err != nil {
		t.Fatal(err)
	}
	if err := s.finishAttempt(at.Add(120 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.beginAttempt(at.Add(121 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.finishAttempt(at); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s = openTestStore(t, dir, 1<<20)
	if !s.nextRequest.Equal(at.Add(122 * time.Second)) {
		t.Fatal("deadline decreased", s.nextRequest)
	}
	for _, limit := range []int64{1, 120} {
		dir := storeDir(t)
		s := openTestStore(t, dir, limit)
		if err := s.beginAttempt(at); err == nil {
			if err := s.finishAttempt(at); err == nil {
				t.Fatal("disk budget ignored")
			}
		}
		if err := s.beginAttempt(at.Add(time.Hour)); err == nil {
			t.Fatal("failed store resumed requests")
		}
	}
	small, err := openStore(storeDir(t), 1<<20, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	if err := small.beginAttempt(at); err == nil {
		t.Fatal("file budget ignored")
	}
}

func TestArchiveDurableFailedFinishBlocksNetwork(t *testing.T) {
	calls := 0
	var c *ArchiveClient
	c, _ = archiveFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		// Simulate another operation exhausting capacity after the request began.
		c.store.mu.Lock()
		c.store.maxBytes = 1
		c.store.mu.Unlock()
		return archiveResponse(200, `{"code":"0","data":[]}`), nil
	})
	if _, err := c.Get(context.Background(), candlesPath, archiveQuery()); err == nil {
		t.Fatal("failed durable finish reported success")
	}
	c.store.mu.Lock()
	c.store.maxBytes = 1 << 20
	c.store.mu.Unlock()
	_, err := c.Get(context.Background(), candlesPath, archiveQuery())
	if err == nil || calls != 1 {
		t.Fatal("failed journal allowed another request", calls)
	}
}
