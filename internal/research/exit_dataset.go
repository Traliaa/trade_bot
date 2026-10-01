package research

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"
)

const exitDatasetLimit = 128 << 20

func exitFinite(v float64) bool   { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func exitPositive(v float64) bool { return exitFinite(v) && v > 0 }
func exitNear(a, b float64) bool {
	return math.Abs(a-b) <= 1e-8+1e-9*math.Max(math.Abs(a), math.Abs(b))
}
func exitSign(side string) float64 {
	if side == "short" {
		return -1
	}
	return 1
}

// Decode rejects missing values rather than silently turning unknown fees into zero.
func DecodeExitDataset(r io.Reader) (ExitDataset, error) {
	var d ExitDataset
	raw, e := io.ReadAll(io.LimitReader(r, exitDatasetLimit+1))
	if e != nil {
		return d, e
	}
	if len(raw) > exitDatasetLimit {
		return d, fmt.Errorf("exit dataset exceeds 128 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&d); e != nil {
		return d, e
	}
	var extra any
	if e = dec.Decode(&extra); e != io.EOF {
		return d, fmt.Errorf("exactly one JSON document required")
	}
	if e = requireExitFields(raw, reflect.TypeOf(d), "dataset"); e != nil {
		return d, e
	}
	_, e = ValidateExitDataset(d)
	return d, e
}
func requireExitFields(raw json.RawMessage, t reflect.Type, path string) error {
	if t.Kind() == reflect.Pointer {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil
		}
		return requireExitFields(raw, t.Elem(), path)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%s cannot be null", path)
	}
	if t == reflect.TypeOf(time.Time{}) {
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		var m map[string]json.RawMessage
		if e := json.Unmarshal(raw, &m); e != nil {
			return e
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "" {
				name = f.Name
			}
			v, ok := m[name]
			if !ok {
				return fmt.Errorf("%s.%s required", path, name)
			}
			if e := requireExitFields(v, f.Type, path+"."+name); e != nil {
				return e
			}
		}
	case reflect.Slice:
		var a []json.RawMessage
		if e := json.Unmarshal(raw, &a); e != nil {
			return e
		}
		for i, v := range a {
			if e := requireExitFields(v, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); e != nil {
				return e
			}
		}
	}
	return nil
}
func validateExitConfig(c ExitPolicyConfig) error {
	v := reflect.ValueOf(c)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Float64:
			if !exitFinite(f.Float()) {
				return fmt.Errorf("nonfinite config")
			}
		case reflect.Int:
			if f.Int() < 0 || f.Int() > 100000 {
				return fmt.Errorf("invalid bar count")
			}
		}
	}
	if c.FixedTimeStopBars <= 0 || c.BETriggerR < 0 || c.BEOffsetR < 0 || c.LockTriggerR < 0 || c.LockOffsetR < 0 || c.PartialTriggerR < 0 || c.PartialCloseFrac < 0 || c.PartialCloseFrac > 1 || c.PartialEnabled && (c.PartialCloseFrac <= 0 || c.PartialCloseFrac >= 1) || c.StaleAfterBars <= 0 || c.StaleGraceBars <= 0 || c.StaleWorseByR < 0 {
		return fmt.Errorf("invalid policy configuration")
	}
	return nil
}
func ValidateExitDataset(d ExitDataset) (ExitValidation, error) {
	v := ExitValidation{Accepted: []ExitSample{}, Excluded: []ExitExclusion{}, Warnings: []string{}}
	if d.Schema != 1 || strings.TrimSpace(d.Provenance) == "" || d.Manifest.Version != "exit-policies-v1" {
		return v, fmt.Errorf("schema=1, provenance and exit-policies-v1 required")
	}
	if e := validateExitConfig(d.Manifest.Config); e != nil {
		return v, e
	}
	if !exitFinite(d.Manifest.Costs.ExitFeeBPS) || math.Abs(d.Manifest.Costs.ExitFeeBPS) > 100 || !exitFinite(d.Manifest.Costs.SlippageBPS) || d.Manifest.Costs.SlippageBPS < 0 || d.Manifest.Costs.SlippageBPS > 100 {
		return v, fmt.Errorf("invalid costs")
	}
	seen := map[string]bool{}
	for _, s := range d.Samples {
		if s.ID == "" || seen[s.ID] {
			return v, fmt.Errorf("empty/duplicate sample ID")
		}
		seen[s.ID] = true
		reason, e := validateExitSample(s, d.Manifest.Config)
		if e != nil {
			return v, fmt.Errorf("%s: %w", s.ID, e)
		}
		if reason != "" {
			v.Excluded = append(v.Excluded, ExitExclusion{s.ID, reason})
			continue
		}
		bars := make([]ExitBar, 0, len(s.Bars)+1)
		if s.EntryTail != nil {
			bars = append(bars, *s.EntryTail)
		}
		for _, b := range s.Bars {
			if !b.Start.Before(s.EntryAt) && b.Start.Before(s.EndAt) {
				bars = append(bars, b)
			}
		}
		sort.Slice(bars, func(i, j int) bool { return bars[i].Start.Before(bars[j].Start) })
		at := s.EntryAt
		for i, b := range bars {
			if e := validateExitBar(b); e != nil {
				return v, fmt.Errorf("%s: %w", s.ID, e)
			}
			tail := i == 0 && s.EntryTail != nil
			if !b.Start.Equal(at) || b.End.After(s.EndAt) || !b.End.Equal(b.End.Truncate(time.Minute)) || (!tail && (b.End.Sub(b.Start) != time.Minute || !b.Start.Equal(b.Start.Truncate(time.Minute)))) {
				return v, fmt.Errorf("%s: duplicate, gap, overlap or invalid bar boundary", s.ID)
			}
			at = b.End
		}
		if !at.Equal(s.EndAt) {
			v.Excluded = append(v.Excluded, ExitExclusion{s.ID, "insufficient_coverage"})
			continue
		}
		fs := []Funding{}
		for _, f := range s.Funding {
			if f.At.Before(s.EntryAt) || f.At.After(s.EndAt) {
				continue
			}
			if f.Symbol != s.Symbol || f.At.IsZero() || !exitPositive(f.Mark) || !exitFinite(f.Rate) {
				return v, fmt.Errorf("%s: invalid funding", s.ID)
			}
			fs = append(fs, f)
		}
		sort.Slice(fs, func(i, j int) bool { return fs[i].At.Before(fs[j].At) })
		for i := 1; i < len(fs); i++ {
			if fs[i].At.Equal(fs[i-1].At) {
				return v, fmt.Errorf("%s: duplicate funding", s.ID)
			}
		}
		if !s.FundingComplete {
			v.Warnings = append(v.Warnings, s.ID+": funding_incomplete (sensitivity only)")
		}
		if s.SpreadBPS == nil {
			v.Warnings = append(v.Warnings, s.ID+": assumed_spread (sensitivity only)")
		}
		// Canonical form retains the tail separately for repeat validation.
		s.Bars = bars
		if s.EntryTail != nil {
			tail := bars[0]
			s.EntryTail = &tail
			s.Bars = bars[1:]
		}
		s.Funding = fs
		v.Accepted = append(v.Accepted, s)
	}
	sort.Slice(v.Accepted, func(i, j int) bool { return v.Accepted[i].ID < v.Accepted[j].ID })
	sort.Slice(v.Excluded, func(i, j int) bool { return v.Excluded[i].SampleID < v.Excluded[j].SampleID })
	sort.Strings(v.Warnings)
	return v, nil
}
func validateExitSample(s ExitSample, c ExitPolicyConfig) (string, error) {
	if s.Symbol == "" || s.Provenance == "" || (s.Side != "long" && s.Side != "short") || s.EntryAt.IsZero() || !s.EndAt.After(s.EntryAt) || !s.EndAt.Equal(s.EndAt.Truncate(time.Minute)) {
		return "", fmt.Errorf("invalid sample identity/time")
	}
	for _, x := range []float64{s.Entry, s.InitialStop, s.InitialTarget, s.Contracts, s.ContractValue, s.TickSize, s.LotSize, s.MinSize} {
		if !exitPositive(x) {
			return "", fmt.Errorf("invalid price/metadata")
		}
	}
	risk := exitSign(s.Side) * (s.Entry - s.InitialStop)
	if risk <= 0 || exitSign(s.Side)*(s.InitialTarget-s.Entry) <= 0 || !exitPositive(risk*s.Contracts*s.ContractValue) || !exitFinite(s.EntryFee) || s.Contracts < s.MinSize || !exitNear(s.Contracts/s.LotSize, math.Round(s.Contracts/s.LotSize)) {
		return "", fmt.Errorf("invalid risk, size or fee")
	}
	if e := validateExitConfig(s.Config); e != nil {
		return "", e
	}
	spread := s.AssumedSpreadBPS
	if s.SpreadBPS != nil {
		spread = *s.SpreadBPS
	}
	if !exitFinite(spread) || spread < 0 || spread > 100 {
		return "", fmt.Errorf("invalid spread")
	}
	switch s.ReconciliationStatus {
	case "verified":
	case "incomplete", "mismatch":
		return "reconciliation_not_verified", nil
	default:
		return "", fmt.Errorf("unknown reconciliation status")
	}
	if s.FeeCurrency != "USDT" || !strings.HasSuffix(s.Symbol, "-USDT-SWAP") {
		return "unsupported_fee_currency", nil
	}
	if s.Config != c {
		return "config_snapshot_mismatch", nil
	}
	aligned := s.EntryAt.Equal(s.EntryAt.Truncate(time.Minute))
	if !aligned && s.EntryTail == nil {
		return "intraminute_entry_coverage_missing", nil
	}
	if s.EntryTail != nil {
		b := s.EntryTail
		if aligned || !b.Start.Equal(s.EntryAt) || !b.End.Equal(s.EntryAt.Truncate(time.Minute).Add(time.Minute)) || b.Source == "" {
			return "", fmt.Errorf("invalid after-entry tail")
		}
	}
	return "", nil
}
func validateExitBar(b ExitBar) error {
	for _, x := range []float64{b.Open, b.High, b.Low, b.Close} {
		if !exitPositive(x) {
			return fmt.Errorf("invalid OHLC")
		}
	}
	if !b.End.After(b.Start) || b.Low > math.Min(b.Open, b.Close) || b.High < math.Max(b.Open, b.Close) || b.High < b.Low {
		return fmt.Errorf("invalid OHLC geometry")
	}
	return nil
}
