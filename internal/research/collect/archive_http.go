//go:build linux || darwin

package collect

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ArchiveClient only reads public archives. It accepts no credentials, headers,
// method, base URL or proxy. Private archive access needs a separate adapter.
type ArchiveClient struct {
	store *Store
	http  *http.Client
	now   func() time.Time
	wait  func(context.Context, time.Duration) error
}

func NewArchiveClient(store *Store) *ArchiveClient {
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		MaxResponseHeaderBytes: 32 << 10, MaxConnsPerHost: 1, MaxIdleConns: 1, IdleConnTimeout: 30 * time.Second,
		DisableCompression: true,
		// Reused-connection GET retries inside net/http otherwise bypass our
		// persistent reservation. Every attempt must go through Get's loop.
		DisableKeepAlives: true,
	}
	return &ArchiveClient{store: store, http: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now, wait: waitArchive}
}

func waitArchive(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func archiveURL(path string, q url.Values) (string, error) {
	if path == "/api/v5/public/instruments" {
		if len(q) != 2 || len(q["instType"]) != 1 || q.Get("instType") != "SWAP" || len(q["instId"]) != 1 || !usdtSwap.MatchString(q.Get("instId")) {
			return "", errors.New("archive_query_invalid")
		}
		return (&url.URL{Scheme: "https", Host: "www.okx.com", Path: path, RawQuery: q.Encode()}).String(), nil
	}
	candles := path == "/api/v5/market/history-candles" || path == "/api/v5/market/history-mark-price-candles"
	trades := path == "/api/v5/market/history-trades"
	if !candles && !trades && path != "/api/v5/public/funding-rate-history" {
		return "", errors.New("archive_request_not_allowed")
	}
	if !usdtSwap.MatchString(q.Get("instId")) {
		return "", errors.New("archive_query_invalid")
	}
	if len(q) > 6 {
		return "", errors.New("archive_query_invalid")
	}
	for key, values := range q {
		if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 64 {
			return "", errors.New("archive_query_invalid")
		}
		v := values[0]
		switch key {
		case "instId":
		case "bar":
			if !candles || v != "1m" {
				return "", errors.New("archive_query_invalid")
			}
		case "type":
			if !trades || (v != "1" && v != "2") {
				return "", errors.New("archive_query_invalid")
			}
		case "limit":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 100 {
				return "", errors.New("archive_query_invalid")
			}
		case "after", "before":
			for _, r := range v {
				if r < '0' || r > '9' {
					return "", errors.New("archive_query_invalid")
				}
			}
		default:
			return "", errors.New("archive_query_invalid")
		}
	}
	return (&url.URL{Scheme: "https", Host: "www.okx.com", Path: path, RawQuery: q.Encode()}).String(), nil
}

// Get returns a bounded public response, not proof of complete pagination or
// validated market evidence. The caller must normalize it before persistence.
func (c *ArchiveClient) Get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	target, err := archiveURL(path, query)
	if err != nil {
		return nil, err
	}
	return c.get(ctx, target, nil)
}

// get is package-private: only the public validated URL builder and the fixed
// private endpoint adapter may supply a target. Signing happens at each attempt.
func (c *ArchiveClient) get(ctx context.Context, target string, authorize func(*http.Request, time.Time)) ([]byte, error) {
	if c.store == nil || c.store.requestGate == nil {
		return nil, errors.New("archive_budget_unavailable")
	}
	select {
	case c.store.requestGate <- struct{}{}:
		defer func() { <-c.store.requestGate }()
	case <-ctx.Done():
		return nil, errors.New("archive_cancelled")
	}
	for attempt := 0; attempt < 4; attempt++ {
		if ctx.Err() != nil {
			return nil, errors.New("archive_cancelled")
		}
		wait := c.store.nextRequest.Sub(c.now())
		if wait > 60*time.Second {
			return nil, errors.New("archive_retry_later")
		}
		if c.wait(ctx, wait) != nil || ctx.Err() != nil {
			return nil, errors.New("archive_cancelled")
		}
		if err := c.store.ReserveRequest(c.now()); err != nil {
			return nil, err
		}
		if err := c.store.beginAttempt(c.now()); err != nil {
			return nil, err
		}
		c.store.nextRequest = c.now().Add(time.Second)
		body, retry, after, err := c.attempt(ctx, target, authorize)
		// Preserve the server deadline across calls/clients, including terminal
		// retries and cancellation during backoff, before releasing the gate.
		if until := c.now().Add(after); retry && until.After(c.store.nextRequest) {
			c.store.nextRequest = until
		}
		if storeErr := c.store.finishAttempt(c.store.nextRequest); storeErr != nil {
			return nil, storeErr
		}
		if err == nil {
			return body, nil
		}
		if ctx.Err() != nil {
			return nil, errors.New("archive_cancelled")
		}
		if !retry || attempt == 3 {
			return nil, err
		}
		delay := time.Duration(2<<attempt) * time.Second
		if after > delay {
			delay = after
		}
		if delay > 60*time.Second {
			return nil, errors.New("archive_retry_later")
		}
		if c.wait(ctx, delay) != nil {
			return nil, errors.New("archive_cancelled")
		}
	}
	return nil, errors.New("archive_unavailable")
}

func (c *ArchiveClient) attempt(ctx context.Context, target string, authorize func(*http.Request, time.Time)) ([]byte, bool, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, false, 0, errors.New("archive_request_invalid")
	}
	req.Header.Set("Accept", "application/json")
	if authorize != nil {
		authorize(req, c.now())
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, 0, errors.New("archive_network_error")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 || resp.StatusCode == 500 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504 {
		delay := retryAfter(resp.Header.Get("Retry-After"), c.now())
		return nil, true, delay, errors.New("archive_transient_error")
	}
	if resp.StatusCode != 200 {
		return nil, false, 0, errors.New("archive_http_error")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxRecord+1))
	if err != nil {
		return nil, true, 0, errors.New("archive_response_io_error")
	}
	if len(b) > maxRecord {
		return nil, false, 0, errors.New("archive_response_too_large")
	}
	dup := json.NewDecoder(bytes.NewReader(b))
	if uniqueJSON(dup, 0) != nil {
		return nil, false, 0, errors.New("archive_response_invalid")
	}
	var envelope struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &envelope) != nil || len(envelope.Data) == 0 || envelope.Data[0] != '[' {
		return nil, false, 0, errors.New("archive_response_invalid")
	}
	if envelope.Code != "0" {
		return nil, false, 0, errors.New("archive_api_error")
	}
	return b, false, 0, nil
}

func retryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n < 0 {
			return 61 * time.Second
		}
		if n > int64((1<<63-1)/time.Second) {
			return time.Duration(1<<63 - 1)
		}
		return time.Duration(n) * time.Second
	} else if errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(raw, "-") {
		return time.Duration(1<<63 - 1)
	}
	if at, err := http.ParseTime(raw); err == nil {
		delay := at.Sub(now)
		if delay < 0 {
			return 0
		}
		return delay
	}
	// An invalid/overflowing instruction cannot justify retrying earlier.
	return 61 * time.Second
}
