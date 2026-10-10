//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const testPrivateCredentials = `{"api_key":"test-key","secret_key":"test-secret","passphrase":"test-passphrase","expected_uid":"123456"}`

func credentialsPipe(t *testing.T, text string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	done := make(chan struct{})
	go func() { defer close(done); _, _ = w.WriteString(text); _ = w.Close() }()
	t.Cleanup(func() { _ = r.Close(); <-done })
	return r
}
func testCredentials(t *testing.T) *PrivateCredentials {
	t.Helper()
	creds, err := ReadPrivateCredentials(context.Background(), credentialsPipe(t, testPrivateCredentials))
	if err != nil {
		t.Fatal(err)
	}
	return creds
}
func TestCredentialsValidationAndPrivacy(t *testing.T) {
	creds := testCredentials(t)
	for _, v := range []any{creds, *creds} {
		if _, err := json.Marshal(v); err == nil {
			t.Fatal("credentials serialized")
		}
		for _, f := range []string{"%v", "%+v", "%#v", "%q"} {
			if out := fmt.Sprintf(f, v); strings.Contains(out, "test-") || strings.Contains(out, "123456") {
				t.Fatal("credentials formatted")
			}
		}
	}
	for _, bad := range []string{"", `{}`, `null`, testPrivateCredentials + `{}`, strings.Replace(testPrivateCredentials, `"api_key":"test-key"`, `"api_key":"test-key","api_key":"duplicate"`, 1), strings.Replace(testPrivateCredentials, `"expected_uid":"123456"`, `"expected_uid":"invalid"`, 1), strings.Replace(testPrivateCredentials, `"test-key"`, `"bad\r\nheader"`, 1), strings.Replace(testPrivateCredentials, `"test-key"`, `""`, 1), strings.Replace(testPrivateCredentials, `"test-key"`, `"`+strings.Repeat("x", 4097)+`"`, 1), strings.Replace(testPrivateCredentials, `"api_key"`, `"unknown"`, 1)} {
		creds, err := ReadPrivateCredentials(context.Background(), credentialsPipe(t, bad))
		if err == nil || creds != nil || strings.Contains(err.Error(), "test-") {
			t.Fatal("unsafe credential decode")
		}
	}
	if _, err := ReadPrivateCredentials(context.Background(), nil); err == nil {
		t.Fatal("nil pipe accepted")
	}
	file, err := os.CreateTemp(t.TempDir(), "not-a-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := ReadPrivateCredentials(context.Background(), file); err == nil {
		t.Fatal("non-pipe credentials accepted")
	}
}
func TestCredentialsCancellation(t *testing.T) {
	for _, preCancelled := range []bool{false, true} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		defer w.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		if preCancelled {
			cancel()
		}
		start := time.Now()
		_, err = ReadPrivateCredentials(ctx, r)
		cancel()
		if err == nil || time.Since(start) > time.Second {
			t.Fatal("credential read did not cancel promptly")
		}
	}
}

func TestCredentialsRejectCaseAliases(t *testing.T) {
	for _, key := range []string{"api_key", "secret_key", "passphrase", "expected_uid"} {
		for _, duplicate := range []bool{false, true} {
			input := strings.Replace(testPrivateCredentials, `"`+key+`"`, `"`+strings.ToUpper(key)+`"`, 1)
			if duplicate {
				input = strings.TrimSuffix(testPrivateCredentials, "}") + `,"` + strings.ToUpper(key) + `":"123456"}`
			}
			if _, err := ReadPrivateCredentials(context.Background(), credentialsPipe(t, input)); err == nil {
				t.Fatalf("accepted case alias for %s, duplicate=%v", key, duplicate)
			}
		}
	}
}
