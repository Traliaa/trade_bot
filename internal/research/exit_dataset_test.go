package research

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strings"
	"testing"
	"time"
)

func TestExitDatasetStrictJSON(t *testing.T) {
	d := ExampleExitDataset()
	raw, _ := json.Marshal(d)
	if _, err := DecodeExitDataset(bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{string(raw) + ` {}`, strings.Replace(string(raw), `"schema":1`, `"schema":99`, 1), strings.Replace(string(raw), `"schema":1`, `"schema":1,"secret":"x"`, 1), strings.Replace(string(raw), `"entry_fee":-0.1,`, "", 1), strings.Replace(string(raw), `"provenance":"synthetic fixture; not market performance"`, `"provenance":""`, 1)} {
		if _, err := DecodeExitDataset(strings.NewReader(v)); err == nil {
			t.Fatal("accepted malformed/missing data")
		}
	}
	if _, err := DecodeExitDataset(io.LimitReader(zeroReader{}, (128<<20)+1)); err == nil {
		t.Fatal("size limit")
	}
	d.Samples = append(d.Samples, d.Samples[0])
	if _, err := ValidateExitDataset(d); err == nil {
		t.Fatal("duplicate ID")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}
func TestExitDatasetNumericAndGeometry(t *testing.T) {
	cases := []func(*ExitSample){func(s *ExitSample) { s.Entry = math.NaN() }, func(s *ExitSample) { s.Contracts = math.Inf(1) }, func(s *ExitSample) { s.InitialStop = 101 }, func(s *ExitSample) { s.InitialTarget = 99 }, func(s *ExitSample) { s.Contracts = 2.2 }, func(s *ExitSample) { s.TickSize = 0 }, func(s *ExitSample) { s.ContractValue = math.MaxFloat64 }, func(s *ExitSample) { s.Bars[0].Low = 105 }}
	for i, f := range cases {
		d := ExampleExitDataset()
		f(&d.Samples[0])
		if _, err := ValidateExitDataset(d); err == nil {
			t.Fatalf("accepted invalid case %d", i)
		}
	}
}
func TestExitDatasetCoverage(t *testing.T) {
	d := ExampleExitDataset()
	s := &d.Samples[0]
	s.Bars = append(s.Bars, s.Bars[0])
	if _, e := ValidateExitDataset(d); e == nil {
		t.Fatal("duplicate")
	}
	d = ExampleExitDataset()
	d.Samples[0].Bars = append(d.Samples[0].Bars[:1], d.Samples[0].Bars[2:]...)
	if _, e := ValidateExitDataset(d); e == nil {
		t.Fatal("gap")
	}
	d = ExampleExitDataset()
	d.Samples[0].Bars = d.Samples[0].Bars[:1]
	v, e := ValidateExitDataset(d)
	if e != nil || len(v.Excluded) != 1 || v.Excluded[0].Reason != "insufficient_coverage" {
		t.Fatalf("%+v %v", v, e)
	}
}
func TestExitDatasetIntraminuteEntry(t *testing.T) {
	d := ExampleExitDataset()
	s := &d.Samples[0]
	s.EntryAt = s.EntryAt.Add(2 * time.Second)
	v, e := ValidateExitDataset(d)
	if e != nil || len(v.Excluded) != 1 || v.Excluded[0].Reason != "intraminute_entry_coverage_missing" {
		t.Fatalf("%+v %v", v, e)
	}
	b := s.Bars[0]
	b.Start = s.EntryAt
	b.Source = "aggregated after-entry ticks"
	s.EntryTail = &b
	s.Bars = s.Bars[1:]
	v, e = ValidateExitDataset(d)
	if e != nil || len(v.Accepted) != 1 {
		t.Fatalf("%+v %v", v, e)
	}
	s.EntryTail.Start = s.EntryTail.Start.Add(-time.Second)
	if _, e = ValidateExitDataset(d); e == nil {
		t.Fatal("pre-entry tail")
	}
}
func TestExitDatasetEligibility(t *testing.T) {
	for _, tt := range []struct {
		change func(*ExitSample)
		reason string
	}{{func(s *ExitSample) { s.ReconciliationStatus = "mismatch" }, "reconciliation_not_verified"}, {func(s *ExitSample) { s.FeeCurrency = "USDC" }, "unsupported_fee_currency"}, {func(s *ExitSample) { s.Config.BETriggerR = 2 }, "config_snapshot_mismatch"}} {
		d := ExampleExitDataset()
		tt.change(&d.Samples[0])
		v, e := ValidateExitDataset(d)
		if e != nil || len(v.Excluded) != 1 || v.Excluded[0].Reason != tt.reason {
			t.Fatalf("%+v %v", v, e)
		}
	}
	d := ExampleExitDataset()
	d.Samples[0].FundingComplete = false
	d.Samples[0].SpreadBPS = nil
	v, e := ValidateExitDataset(d)
	if e != nil || len(v.Accepted) != 1 || len(v.Warnings) < 2 {
		t.Fatalf("%+v %v", v, e)
	}
}
func TestExitDefaultManifest(t *testing.T) {
	d := ExampleExitDataset()
	d.Samples[0].EntryFee = .01
	if _, e := ValidateExitDataset(d); e != nil {
		t.Fatal(e)
	}
	d.Manifest.Config.PartialCloseFrac = 1
	if _, e := ValidateExitDataset(d); e == nil {
		t.Fatal("invalid fraction")
	}
}
