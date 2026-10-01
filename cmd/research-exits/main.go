// research-exits is strictly offline: no exchange clients, credentials or mutations.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"trade_bot/internal/research"
)

func main() {
	if e := run(os.Args[1:], os.Stdout, os.Stderr); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("research-exits", flag.ContinueOnError)
	fs.SetOutput(stderr)
	input := fs.String("input", "", "local exit dataset JSON")
	example := fs.Bool("example", false, "print synthetic schema example, not market results")
	stress := fs.Bool("stress", true, "also compare doubled negative fees and slippage")
	if e := fs.Parse(args); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	if fs.NArg() != 0 || (*example && *input != "") {
		return fmt.Errorf("choose -example or -input; no positional arguments")
	}
	encode := func(v any) error {
		raw, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		_, e = stdout.Write(append(raw, '\n'))
		return e
	}
	if *example {
		return encode(research.ExampleExitDataset())
	}
	if *input == "" || strings.Contains(*input, "://") {
		return fmt.Errorf("-input must name a local regular file")
	}
	// Opening a FIFO can block before f.Stat; reject special files first.
	st, e := os.Stat(*input)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Size() > 128<<20 {
		return fmt.Errorf("input must be a regular file <=128 MiB")
	}
	f, e := os.Open(*input)
	if e != nil {
		return e
	}
	defer f.Close()
	st, e = f.Stat()
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Size() > 128<<20 {
		return fmt.Errorf("input must be a regular file <=128 MiB")
	}
	raw, e := io.ReadAll(io.LimitReader(f, (128<<20)+1))
	if e != nil {
		return e
	}
	d, e := research.DecodeExitDataset(bytes.NewReader(raw))
	if e != nil {
		return e
	}
	report, e := research.CompareExits(d, *stress)
	if e != nil {
		return e
	}
	hash := sha256.Sum256(raw)
	report.DatasetSHA256 = hex.EncodeToString(hash[:])
	report.CodeRevision = "unknown"
	dirty := false
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				report.CodeRevision = s.Value
			}
			if s.Key == "vcs.modified" && s.Value == "true" {
				dirty = true
			}
		}
	}
	if dirty {
		report.CodeRevision += "-dirty"
	}
	return encode(report)
}
