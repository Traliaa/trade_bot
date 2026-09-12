// research reads a local historical dataset; it has no network or trading client.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
	"trade_bot/internal/research"
	"trade_bot/internal/universe"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	example := flag.Bool("example", false, "print a synthetic dataset illustrating the input schema")
	file := flag.String("input", "", "historical JSON dataset (schema 1)")
	startArg := flag.String("start", "", "OOS start RFC3339, earlier bars are warmup only")
	endArg := flag.String("end", "", "OOS end RFC3339")
	caps := flag.String("limits", "20,40,80", "total universe caps, not core + dynamic")
	windows := flag.Int("windows", 1, "sequential evaluation windows, no automatic parameter optimization")
	stress := flag.Bool("stress", true, "also report doubled execution costs")
	flag.Parse()
	if *example {
		d, _ := research.ExampleDataset()
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(d)
	}
	start, err := time.Parse(time.RFC3339, *startArg)
	if err != nil {
		return fmt.Errorf("valid -start required")
	}
	end, err := time.Parse(time.RFC3339, *endArg)
	if err != nil || !end.After(start) {
		return fmt.Errorf("valid -end after -start required")
	}
	if *windows < 1 || *windows > 24 || end.Sub(start)/time.Duration(*windows) < time.Hour {
		return fmt.Errorf("windows must be 1..24, each at least one hour")
	}
	f, err := os.Open(*file)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 128<<20+1))
	if err != nil {
		return err
	}
	if len(raw) > 128<<20 {
		return fmt.Errorf("dataset exceeds 128 MiB; split evaluation windows")
	}
	var d research.Dataset
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	version := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, v := range bi.Settings {
			if v.Key == "vcs.revision" {
				version = v.Value
			}
		}
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, v := range bi.Settings {
			if v.Key == "vcs.modified" && v.Value == "true" {
				version += "-dirty"
			}
		}
	}
	out := struct {
		DatasetSHA256 string            `json:"dataset_sha256"`
		CodeRevision  string            `json:"code_revision"`
		Provenance    string            `json:"provenance"`
		Reports       []research.Report `json:"reports"`
	}{DatasetSHA256: hex.EncodeToString(hash[:]), CodeRevision: version, Provenance: d.Provenance}
	// Same risk, fees, leverage and time exit for every candidate. These are
	// research assumptions, not deployment settings or a recommendation to trade.
	for _, capArg := range strings.Split(*caps, ",") {
		cap, err := strconv.Atoi(strings.TrimSpace(capArg))
		if err != nil {
			return err
		}
		for _, name := range []string{"v3", "smc"} {
			for w := 0; w < *windows; w++ {
				for cost := 1; cost <= 2; cost++ {
					if cost == 2 && !*stress {
						continue
					}
					a := start.Add(end.Sub(start) * time.Duration(w) / time.Duration(*windows))
					b := start.Add(end.Sub(start) * time.Duration(w+1) / time.Duration(*windows))
					o := research.Options{Strategy: name, Start: a, End: b, Equity: 100, RiskPct: .2, Leverage: 3, FeeBPS: 5 * float64(cost), SlippageBPS: 3 * float64(cost), MaxPositions: 3, MaxHolding: 2 * time.Hour, RR: 1.5, Shorts: false, SnapshotMaxAge: time.Hour, Policy: universe.Policy{Limit: cap, MinQuoteVolume: 10_000_000, MaxSpreadBPS: 20, MaxRangePct: .2, MaxMovePct: .12, MinAge: 30 * 24 * time.Hour, MaxAge: 2 * time.Minute, MinResidence: 6 * time.Hour, RetainBonus: .15}}
					report, err := research.Run(d, o)
					if err != nil {
						return fmt.Errorf("%s/%d/window%d: %w", name, cap, w, err)
					}
					out.Reports = append(out.Reports, report)
				}
			}
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}
