//go:build linux || darwin

package collect

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func privateFixture(t *testing.T, transport archiveTransport) (*PrivateClient, string) {
	t.Helper()
	engine, dir := archiveFixture(t, transport)
	p, err := NewPrivateClient(engine.store, testCredentials(t))
	if err != nil {
		t.Fatal(err)
	}
	p.engine = engine
	return p, dir
}

func TestPrivateAndPublicClientsShareConcurrentGate(t *testing.T) {
	var calls int
	p, dir := privateFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++ // Protected by the Store gate, not a test-only mutex.
		if r.URL.Path == accountConfigPath {
			if r.Header.Get("OK-ACCESS-SIGN") == "" {
				t.Error("private signature missing")
			}
			return archiveResponse(200, `{"code":"0","data":[{"perm":"read_only","uid":"123456"}]}`), nil
		}
		if r.Header.Get("OK-ACCESS-KEY") != "" || r.Header.Get("OK-ACCESS-SIGN") != "" {
			t.Error("public request inherited auth")
		}
		return archiveResponse(200, `{"code":"0","data":[]}`), nil
	})
	public := NewArchiveClient(p.engine.store)
	public.http.Transport = p.engine.http.Transport
	public.now, public.wait = p.engine.now, p.engine.wait
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := p.verifyAccount(context.Background()); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := public.Get(context.Background(), candlesPath, archiveQuery()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	b, err := os.ReadFile(filepath.Join(dir, ".quota-2026-10-08"))
	if err != nil || calls != 8 || string(b) != "1\n2\n3\n4\n5\n6\n7\n8\n" {
		t.Fatal("separate clients bypassed shared quota/gate")
	}
}
func TestPrivateSignsEveryAttemptAndPublicStaysUnsigned(t *testing.T) {
	var stamps []string
	p, dir := privateFixture(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.Body != nil || r.URL.Host != "www.okx.com" || r.URL.Scheme != "https" {
			t.Fatal("unsafe request")
		}
		if r.URL.Path == candlesPath {
			if r.Header.Get("OK-ACCESS-KEY") != "" || r.Header.Get("OK-ACCESS-SIGN") != "" {
				t.Fatal("signed public request")
			}
			return archiveResponse(200, `{"code":"0","data":[]}`), nil
		}
		stamp := r.Header.Get("OK-ACCESS-TIMESTAMP")
		stamps = append(stamps, stamp)
		if len(stamps) == 1 && (stamp != "2026-10-08T07:00:00.000Z" || r.Header.Get("OK-ACCESS-SIGN") != "wFKC5cWv6cPevr7gNgeMkkBbLt3yQFI5ET4/1kiaa6o=") {
			t.Fatal("known HMAC test vector mismatch")
		}
		// Independently derive the signature from the actual wire URI, not the
		// implementation's request builder or signing helper.
		mac := hmac.New(sha256.New, []byte("test-secret"))
		_, _ = mac.Write([]byte(stamp + "GET" + r.URL.RequestURI()))
		if r.Header.Get("OK-ACCESS-SIGN") != base64.StdEncoding.EncodeToString(mac.Sum(nil)) || r.Header.Get("OK-ACCESS-KEY") != "test-key" || r.Header.Get("OK-ACCESS-PASSPHRASE") != "test-passphrase" {
			t.Fatal("incorrect auth headers")
		}
		if len(stamps) == 1 {
			return nil, errors.New("PRIVATE upstream secret")
		}
		return archiveResponse(200, `{"code":"0","data":[{"perm":"read_only","uid":"123456","label":"PRIVATE"}]}`), nil
	})
	if err := p.verifyAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(stamps) != 2 || stamps[0] == stamps[1] {
		t.Fatal("retry reused timestamp/signature")
	}
	if _, err := p.engine.Get(context.Background(), candlesPath, archiveQuery()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".quota-2026-10-08"))
	if err != nil || string(b) != "1\n2\n3\n" {
		t.Fatal("private/public quota not shared")
	}
	for _, v := range []any{p, *p} {
		if _, err := json.Marshal(v); err == nil {
			t.Fatal("private client serialized")
		}
		if strings.Contains(fmt.Sprintf("%+v", v), "test-") {
			t.Fatal("client leaked secrets")
		}
	}
}
func TestPrivateAccountDenialAndSafeErrors(t *testing.T) {
	for _, body := range []string{`{"code":"0","data":[]}`, `{"code":"0","data":[{"perm":"read_only,trade","uid":"123456"}]}`, `{"code":"0","data":[{"perm":"read_only,withdraw","uid":"123456"}]}`, `{"code":"0","data":[{"perm":"read_only","uid":"999999"}]}`, `{"code":"0","data":[{"perm":"read_only"}]}`, `{"code":"0","data":[{"perm":"read_only","perm":"trade","uid":"123456"}]}`, `{"code":"50101","msg":"PRIVATE secret","data":[]}`} {
		p, _ := privateFixture(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/api/v5/account/config" {
				t.Fatal("unexpected endpoint")
			}
			return archiveResponse(200, body), nil
		})
		if err := p.verifyAccount(context.Background()); err == nil || strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), "123456") {
			t.Fatal("account denial/redaction failed")
		}
	}
	if _, err := NewPrivateClient(nil, testCredentials(t)); err == nil {
		t.Fatal("nil store")
	}
	s := openTestStore(t, storeDir(t), 1<<20)
	for _, creds := range []*PrivateCredentials{nil, {}} {
		if _, err := NewPrivateClient(s, creds); err == nil {
			t.Fatal("missing credentials accepted")
		}
	}
}

func TestPrivateAccountRejectsCaseAliases(t *testing.T) {
	for _, body := range []string{
		`{"code":"0","data":[{"perm":"trade","PERM":"read_only","uid":"123456"}]}`,
		`{"code":"0","data":[{"PERM":"read_only","uid":"123456"}]}`,
		`{"code":"0","data":[{"perm":"read_only","UID":"123456"}]}`,
		`{"code":"0","data":[{"perm":"read_only","uid":"999999","UID":"123456"}]}`,
		`{"code":"0","DATA":[{"perm":"read_only","uid":"123456"}]}`,
		`{"CODE":"0","data":[{"perm":"read_only","uid":"123456"}]}`,
	} {
		p, _ := privateFixture(t, func(*http.Request) (*http.Response, error) { return archiveResponse(200, body), nil })
		if err := p.verifyAccount(context.Background()); err == nil {
			t.Fatal("ambiguous/case-aliased permission response accepted")
		}
	}
}
func TestPrivateFillPageQueryAndRedirect(t *testing.T) {
	calls := 0
	p, _ := privateFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/api/v5/trade/fills-history" || r.URL.Query().Get("instType") != "SWAP" || r.URL.Query().Get("after") != "987" {
			t.Fatal("wrong fills request")
		}
		mac := hmac.New(sha256.New, []byte("test-secret"))
		_, _ = mac.Write([]byte(r.Header.Get("OK-ACCESS-TIMESTAMP") + "GET/api/v5/trade/fills-history?after=987&begin=1791446400000&end=1791446460000&instId=ETH-USDT-SWAP&instType=SWAP&limit=100"))
		if r.Header.Get("OK-ACCESS-SIGN") != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
			t.Fatal("query missing from signature")
		}
		rsp := archiveResponse(302, `PRIVATE`)
		rsp.Header.Set("Location", "https://other.invalid/steal")
		return rsp, nil
	})
	q := url.Values{"instType": {"SWAP"}, "instId": {"ETH-USDT-SWAP"}, "begin": {"1791446400000"}, "end": {"1791446460000"}, "after": {"987"}, "limit": {"100"}}
	_, err := p.getFillPage(context.Background(), q)
	if err == nil || strings.Contains(err.Error(), "PRIVATE") || calls != 1 {
		t.Fatal("redirect not refused")
	}
	for _, key := range []string{"private", "ordId", "before"} {
		bad, _ := url.ParseQuery(q.Encode())
		bad.Set(key, "secret")
		if _, err := p.getFillPage(context.Background(), bad); err == nil {
			t.Fatal("unsupported query accepted")
		}
	}
	if calls != 1 {
		t.Fatal("invalid query sent")
	}
}
