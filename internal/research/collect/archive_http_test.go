//go:build linux || darwin

package collect

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type archiveTransport func(*http.Request) (*http.Response, error)

func (f archiveTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func archiveResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func archiveQuery() url.Values { return url.Values{"instId": {"ETH-USDT-SWAP"}, "bar": {"1m"}} }

const candlesPath = "/api/v5/market/history-candles"

func archiveFixture(t *testing.T, transport archiveTransport) (*ArchiveClient, string) {
	t.Helper()
	dir := storeDir(t)
	s := openTestStore(t, dir, 1<<20)
	c := NewArchiveClient(s)
	c.http.Transport = transport
	now := time.Date(2026, 10, 8, 7, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	c.wait = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if d > 0 {
			now = now.Add(d)
		}
		return nil
	}
	return c, dir
}

func TestArchiveTargetAndBlockedPaths(t *testing.T) {
	calls := 0
	c, _ := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Scheme != "https" || r.URL.Host != "www.okx.com" || r.URL.Path != candlesPath || r.URL.RawQuery != "bar=1m&instId=ETH-USDT-SWAP" || r.Body != nil || r.Header.Get("OK-ACCESS-KEY") != "" {
			t.Fatal("unsafe outgoing request")
		}
		return archiveResponse(200, `{"code":"0","msg":"","data":[]}`), nil
	})
	if _, err := c.Get(context.Background(), candlesPath, archiveQuery()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"https://other.invalid/a", "/api/v5/trade/order", candlesPath + "?x=1", "/api/v5/market/../trade/order"} {
		if _, err := c.Get(context.Background(), path, archiveQuery()); err == nil {
			t.Fatal("blocked path allowed")
		}
	}
	query := archiveQuery()
	query.Set("secret", "PRIVATE")
	if _, err := c.Get(context.Background(), candlesPath, query); err == nil {
		t.Fatal("unknown query allowed")
	}
	if calls != 1 {
		t.Fatalf("blocked requests reached network: %d", calls)
	}
}

func TestArchiveRetriesChargedAndSpaced(t *testing.T) {
	calls := 0
	var starts []time.Time
	var c *ArchiveClient
	c, dir := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		starts = append(starts, c.now())
		return nil, errors.New("PRIVATE URL key")
	})
	_, err := c.Get(context.Background(), candlesPath, archiveQuery())
	if err == nil || strings.Contains(err.Error(), "PRIVATE") || calls != 4 {
		t.Fatalf("retry/redaction: calls=%d err=%v", calls, err)
	}
	for i := 1; i < len(starts); i++ {
		if starts[i].Sub(starts[i-1]) < time.Second {
			t.Fatal("rate limit bypassed")
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, ".quota-2026-10-08"))
	if err != nil || string(b) != "1\n2\n3\n4\n" {
		t.Fatal("retries not charged")
	}
}

func TestArchiveFailuresAndRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body, after string
		calls       int
	}{
		{"redirect", 302, "PRIVATE", "", 1},
		{"denied", 403, "PRIVATE", "", 1},
		{"malformed", 200, "PRIVATE", "", 1},
		{"oversize", 200, strings.Repeat("x", (4<<20)+1), "", 1},
		{"api_error", 200, `{"code":"50000","msg":"PRIVATE","data":[]}`, "", 1},
		{"missing_data", 200, `{"code":"0"}`, "", 1},
		{"defer", 429, "PRIVATE", "61", 1},
		{"retry", 429, "PRIVATE", "3", 4},
		{"server", 503, "PRIVATE", "", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c, _ := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
				calls++
				resp := archiveResponse(tc.status, tc.body)
				resp.Header.Set("Location", "https://other.invalid/PRIVATE")
				resp.Header.Set("Retry-After", tc.after)
				return resp, nil
			})
			_, err := c.Get(context.Background(), candlesPath, archiveQuery())
			if err == nil || strings.Contains(err.Error(), "PRIVATE") || calls != tc.calls {
				t.Fatalf("unsafe error/retry: %d %v", calls, err)
			}
		})
	}
}

func TestArchiveCancellationAndSingleFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	c, dir := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-release:
			return archiveResponse(200, `{"code":"0","data":[]}`), nil
		}
	})
	first := make(chan error, 1)
	go func() { _, err := c.Get(context.Background(), candlesPath, archiveQuery()); first <- err }()
	select {
	case <-started:
	case err := <-first:
		t.Fatalf("request stopped before transport: %v", err)
	case <-time.After(time.Second):
		t.Fatal("transport did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	second := NewArchiveClient(c.store)
	second.http, second.now, second.wait = c.http, c.now, c.wait
	if _, err := second.Get(ctx, candlesPath, archiveQuery()); err == nil {
		t.Fatal("cancelled queued request ran")
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".quota-2026-10-08"))
	if string(b) != "1\n" {
		t.Fatal("queued cancellation charged")
	}
}

func TestArchiveCancelBackoff(t *testing.T) {
	calls := 0
	c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) { calls++; return archiveResponse(503, "unavailable"), nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.wait = func(ctx context.Context, d time.Duration) error {
		if d > 0 {
			cancel()
		}
		return ctx.Err()
	}
	if _, err := c.Get(ctx, candlesPath, archiveQuery()); err == nil || calls != 1 {
		t.Fatal("backoff cancellation allowed retry")
	}
}

func TestArchiveRetryAfterDates(t *testing.T) {
	now := time.Date(2026, 10, 8, 7, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		raw  string
		want time.Duration
	}{
		{"Thu, 08 Oct 2026 07:00:30 GMT", 30 * time.Second},
		{"Thu, 08 Oct 2026 06:59:30 GMT", 0},
		{"99999999999999999999999999", time.Duration(1<<63 - 1)},
		{"garbage", 61 * time.Second},
	} {
		if got := retryAfter(tc.raw, now); got != tc.want {
			t.Fatalf("%q: %s", tc.raw, got)
		}
	}
}

func TestArchiveUnopenedStoreFailsClosed(t *testing.T) {
	c := NewArchiveClient(&Store{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := c.Get(ctx, candlesPath, archiveQuery())
	if err == nil || err.Error() != "archive_budget_unavailable" {
		t.Fatalf("unopened store queued forever: %v", err)
	}
}

func TestArchiveCooldownSharedAcrossCalls(t *testing.T) {
	for _, after := range []string{"120", "Thu, 08 Oct 2026 07:02:00 GMT"} {
		t.Run(after, func(t *testing.T) {
			calls := 0
			c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					return archiveResponse(200, `{"code":"0","data":[]}`), nil
				}
				r := archiveResponse(429, "")
				r.Header.Set("Retry-After", after)
				return r, nil
			})
			if _, err := c.Get(context.Background(), candlesPath, archiveQuery()); err == nil {
				t.Fatal("429 accepted")
			}
			second := NewArchiveClient(c.store)
			second.http, second.now, second.wait = c.http, c.now, c.wait
			if _, err := second.Get(context.Background(), candlesPath, archiveQuery()); err == nil || calls != 1 {
				t.Fatalf("cooldown forgotten, requests=%d", calls)
			}
			_ = c.wait(context.Background(), 90*time.Second)
			if _, err := second.Get(context.Background(), candlesPath, archiveQuery()); err != nil || calls != 2 {
				t.Fatal("cooldown never expires", err)
			}
			if got := c.now(); !got.Equal(time.Date(2026, 10, 8, 7, 2, 0, 0, time.UTC)) {
				t.Fatal("Retry-After was shortened", got)
			}
		})
	}
}

func TestArchiveBudgetAndTimeout(t *testing.T) {
	calls := 0
	c, _ := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 15*time.Second {
			t.Fatal("unbounded request")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx, candlesPath, archiveQuery()); err == nil || calls != 1 {
		t.Fatal("timeout not respected")
	}
	_ = c.store.Close()
	if _, err := c.Get(context.Background(), candlesPath, archiveQuery()); err == nil || calls != 1 {
		t.Fatal("failed reservation reached network")
	}
}

func TestArchiveTransportRetriesCannotBypassQuota(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 2 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = io.WriteString(w, `{"code":"0","data":[]}`)
	}))
	defer server.Close()
	dir := storeDir(t)
	s := openTestStore(t, dir, 1<<20)
	c := NewArchiveClient(s)
	tr := c.http.Transport.(*http.Transport)
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // local self-signed test server only
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	defer tr.CloseIdleConnections()
	now := time.Date(2026, 10, 8, 7, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	c.wait = func(ctx context.Context, d time.Duration) error {
		if d > 0 {
			now = now.Add(d)
		}
		return ctx.Err()
	}
	for i := 0; i < 2; i++ {
		if _, err := c.Get(context.Background(), candlesPath, archiveQuery()); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, ".quota-2026-10-08"))
	if err != nil || calls.Load() != 3 || string(b) != "1\n2\n3\n" {
		t.Fatalf("unaccounted retry: requests=%d journal=%q err=%v", calls.Load(), b, err)
	}
}
