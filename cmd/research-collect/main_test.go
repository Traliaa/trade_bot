//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"trade_bot/internal/research/collect"
)

func TestCLIFlagsDoNotEchoSecrets(t *testing.T) {
	for _, args := range [][]string{{"run", "--dsn", "PRIVATE"}, {"PRIVATE-command"}, {"run", "--db-fd", "PRIVATE"}, {"run", "--store", "/PRIVATE/missing"}, {"inspect", "--ref", "PRIVATE"}} {
		var out bytes.Buffer
		if execute(context.Background(), args, &out, time.Now()) == 0 || strings.Contains(out.String(), "PRIVATE") {
			t.Fatal("unsafe CLI error", out.String())
		}
	}
	var out bytes.Buffer
	if execute(context.Background(), []string{"--help"}, &out, time.Now()) != 0 || !strings.Contains(out.String(), "one-shot") {
		t.Fatal("missing safe help")
	}
}

func TestCLIInheritedPipe(t *testing.T) {
	if os.Getenv("RESEARCH_PIPE_TEST_CHILD") == "1" {
		f := credentialPipe(3)
		if f == nil {
			t.Fatal("inherited pipe unavailable")
		}
		defer f.Close()
		if _, err := collect.ReadDBCredentials(context.Background(), f); err != nil {
			t.Fatal("inherited pipe rejected", err)
		}
		return
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, err = w.WriteString(`{"dsn":"postgres://reader:synthetic@127.0.0.1:55001/test?sslmode=disable"}`)
	_ = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIInheritedPipe$")
	cmd.Env = append(os.Environ(), "RESEARCH_PIPE_TEST_CHILD=1")
	cmd.ExtraFiles = []*os.File{r}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("inherited pipe: %v %s", err, out)
	}
}
func TestCLIInitAndInspect(t *testing.T) {
	dir, err := os.MkdirTemp("/private/tmp", "research-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(dir, 0700)
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	args := []string{"init", "--store", dir, "--outside-web-root", "--protocol", "study", "--start", now.Add(time.Hour).Format(time.RFC3339)}
	var out bytes.Buffer
	if execute(context.Background(), args, &out, now) != 0 {
		t.Fatal(out.String())
	}
	var result struct {
		ProtocolRef string `json:"protocol_ref"`
	}
	if json.Unmarshal(out.Bytes(), &result) != nil || len(result.ProtocolRef) != 64 {
		t.Fatal("no manifest")
	}
	out.Reset()
	if execute(context.Background(), []string{"inspect", "--store", dir, "--ref", result.ProtocolRef}, &out, now) != 0 || !strings.Contains(out.String(), "prospective-exit-protocol-v1") {
		t.Fatal("offline inspect failed", out.String())
	}
	out.Reset()
	if execute(context.Background(), []string{"run", "--store", dir, "--outside-web-root", "--ref", result.ProtocolRef}, &out, now) != 2 {
		t.Fatal("run without private DB pipe")
	}
}
