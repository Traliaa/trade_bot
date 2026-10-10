//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestDBCredentialPipeAndConfig(t *testing.T) {
	for _, dsn := range []string{"postgres://reader:SECRET@db.example:5432/trades?sslmode=verify-full", "postgres://reader:SECRET@127.0.0.1:5432/test?sslmode=disable"} {
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]string{"dsn": dsn})
		_, _ = write.Write(raw)
		_ = write.Close()
		creds, err := ReadDBCredentials(context.Background(), read)
		_ = read.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fmt.Sprintf("%+v", creds), "SECRET") {
			t.Fatal("formatted DB secret")
		}
		if _, err := json.Marshal(creds); err == nil {
			t.Fatal("serialized DB secret")
		}
		cfg, err := creds.config()
		if err != nil || cfg.MaxConns != 1 || cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] != "on" || len(cfg.ConnConfig.Fallbacks) != 0 {
			t.Fatal("unsafe DB config", err)
		}
		if strings.Contains(dsn, "example") && (cfg.ConnConfig.TLSConfig == nil || cfg.ConnConfig.TLSConfig.InsecureSkipVerify) {
			t.Fatal("unverified remote TLS")
		}
	}
}
func TestDBCredentialRejectsDiscovery(t *testing.T) {
	for _, dsn := range []string{"", "postgres://reader:SECRET@db.example:5432/db?sslmode=disable", "postgres://reader@db.example:5432/db?sslmode=verify-full", "postgres://reader:SECRET@db.example:5432/db?service=trade", "postgres://reader:SECRET@db.example:5432/db?sslmode=prefer"} {
		c := &DBCredentials{dsn: dsn}
		if _, err := c.config(); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("unsafe DSN", err)
		}
	}
	t.Setenv("PGSERVICE", "SECRET-service")
	c := &DBCredentials{dsn: "postgres://reader:SECRET@db.example:5432/db?sslmode=verify-full"}
	if _, err := c.config(); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("env discovery allowed", err)
	}
}

func TestDBCredentialPipeBounds(t *testing.T) {
	for _, raw := range []string{strings.Repeat("X", 4097), `{"dsn":"a","DSN":"b"}`, `{"dsn":"a"} {}`, `{"dsn":"a","extra":"b"}`} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.WriteString(raw)
		_ = w.Close()
		_, err = ReadDBCredentials(context.Background(), r)
		_ = r.Close()
		if err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
	f, err := os.CreateTemp(t.TempDir(), "not-a-pipe")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = ReadDBCredentials(context.Background(), f); err == nil {
		t.Fatal("file credentials allowed")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = ReadDBCredentials(ctx, r); err == nil {
		t.Fatal("cancel ignored")
	}
}
