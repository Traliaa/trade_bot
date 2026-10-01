package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"trade_bot/internal/research"
)

func TestExitCLIExampleRoundTrip(t *testing.T) {
	var out, errout bytes.Buffer
	if e := run([]string{"-example"}, &out, &errout); e != nil {
		t.Fatal(e)
	}
	raw := append([]byte{}, out.Bytes()...)
	if _, e := research.DecodeExitDataset(bytes.NewReader(raw)); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "input.json")
	if e := os.WriteFile(p, raw, 0600); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e := run([]string{"-input", p}, &out, &errout); e != nil {
		t.Fatal(e)
	}
	var r research.ExitComparison
	if e := json.Unmarshal(out.Bytes(), &r); e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(raw)
	if r.DatasetSHA256 != hex.EncodeToString(hash[:]) || r.CodeRevision == "" || r.ModelVersion == "" || len(r.Outcomes) != 6 {
		t.Fatal(r)
	}
}
func TestExitCLIRejectsInvalidInput(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	if e := os.WriteFile(p, []byte(`{} {}`), 0600); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{nil, {"-input", p}, {"-input", "https://example.com/x"}, {"-input", p + "missing"}, {"-example", "-input", p}, {"-example", "extra"}} {
		var out, errout bytes.Buffer
		if e := run(args, &out, &errout); e == nil || out.Len() != 0 {
			t.Fatalf("%v %v %s", args, e, out.String())
		}
	}
}
func TestExitCLIOfflineBoundary(t *testing.T) {
	f, e := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, im := range f.Imports {
		s, _ := strconv.Unquote(im.Path.Value)
		for _, bad := range []string{"net/", "database/", "okx_client", "repository/", "os/exec"} {
			if strings.Contains(s, bad) {
				t.Fatal("forbidden dependency", s)
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok && id.Name == "os" && (s.Sel.Name == "Getenv" || s.Sel.Name == "LookupEnv") {
				t.Fatal("credential/env read")
			}
		}
		return true
	})
}
