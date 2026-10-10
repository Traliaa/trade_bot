//go:build linux || darwin

package collect

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"trade_bot/internal/models"
)

type Component struct {
	Kind   string    `json:"kind"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Status string    `json:"status"`
	Reason string    `json:"reason"`
	Refs   []string  `json:"refs"`
}
type Sample struct {
	CaptureID               string      `json:"capture_id"`
	SnapshotRef             string      `json:"snapshot_ref"`
	Symbol                  string      `json:"symbol"`
	EntryAt                 time.Time   `json:"entry_at"`
	EndAt                   time.Time   `json:"end_at"`
	Capture                 string      `json:"capture"`
	Market                  string      `json:"market"`
	Reconciliation          string      `json:"reconciliation"`
	Costs                   string      `json:"costs"`
	MetadataChanged         *bool       `json:"metadata_changed_since_previous_observation"`
	MetadataHistoryVerified bool        `json:"metadata_history_verified"`
	Components              []Component `json:"components"`
}
type Bundle struct {
	Format           string        `json:"format"`
	ProtocolRef      string        `json:"protocol_ref"`
	CreatedAt        time.Time     `json:"created_at"`
	Previous         string        `json:"previous_bundle_ref"`
	LastSuccessfulAt *time.Time    `json:"last_successful_pass_at"`
	Status           string        `json:"collection_status"`
	ReplayReady      bool          `json:"replay_ready"`
	Entries          EntryCoverage `json:"entries"`
	Samples          []Sample      `json:"samples"`
}
type PassReport struct {
	Status           string        `json:"status"`
	BundleRef        string        `json:"bundle_ref"`
	Samples          int           `json:"samples"`
	Entries          EntryCoverage `json:"entries"`
	LastSuccessfulAt *time.Time    `json:"last_successful_pass_at"`
}
type evidenceRecord struct {
	Format  string          `json:"format"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}
type boundaryPiece struct {
	Part    int             `json:"part"`
	Parts   int             `json:"parts"`
	Archive BoundaryArchive `json:"archive"`
}

var errBundle = errors.New("research_bundle_invalid")

func saveEvidence(s *Store, kind string, value any) (string, error) {
	// Keep this boundary closed: never accept FillArchive/EntryBatch/credentials.
	switch value.(type) {
	case CandleArchive, FundingArchive, MetadataObservation, LedgerReport, boundaryPiece:
	default:
		return "", errBundle
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", errBundle
	}
	data, err := json.Marshal(evidenceRecord{"prospective-exit-evidence-v1", kind, payload})
	if err != nil {
		return "", errBundle
	}
	return s.Put(data)
}
func saveBoundary(s *Store, a BoundaryArchive) ([]string, error) {
	refs := []string{}
	parts := (len(a.Trades) + 499) / 500
	if parts == 0 {
		parts = 1
	}
	for i := 0; i < parts; i++ {
		chunk := a
		start := i * 500
		end := start + 500
		if end > len(a.Trades) {
			end = len(a.Trades)
		}
		chunk.Trades = a.Trades[start:end]
		ref, err := saveEvidence(s, "boundary", boundaryPiece{i, parts, chunk})
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}
func readEvidence(s *Store, ref, kind string, out any) error {
	b, err := s.Get(ref)
	if err != nil {
		return err
	}
	var record evidenceRecord
	if strictRecord(b, &record) != nil || record.Format != "prospective-exit-evidence-v1" || record.Kind != kind || strictRecord(record.Payload, out) != nil {
		return errBundle
	}
	return nil
}
func validStatus(v string) bool { return v == "complete" || v == "incomplete" || v == "pending" }

// ReadBundle verifies the bounded content-addressed graph, not the truth of the
// provider's history. All records are private-volume artifacts, not replay v1.
func ReadBundle(s *Store, ref string) (Bundle, error) {
	history, err := readBundleHistory(s, ref)
	if err != nil {
		return Bundle{}, err
	}
	return history[0], nil
}

func validateBundle(s *Store, b Bundle) (Bundle, error) {
	if b.Previous != "" {
		history, err := readBundleHistory(s, b.Previous)
		if err != nil {
			return Bundle{}, err
		}
		if history[0].ProtocolRef != b.ProtocolRef || !history[0].CreatedAt.Before(b.CreatedAt) {
			return Bundle{}, errBundle
		}
	}
	return validateBundleNode(s, b, newBundleChecks())
}

type checkedSnapshot struct {
	input  models.ResearchCaptureInput
	status string
}
type bundleChecks struct {
	snapshots  map[string]checkedSnapshot
	components map[[32]byte]bool
}

func newBundleChecks() *bundleChecks {
	return &bundleChecks{snapshots: map[string]checkedSnapshot{}, components: map[[32]byte]bool{}}
}

// Iterative, bounded by the store's file budget. Shared snapshots/evidence are
// checked once per traversal; a missing ancestor node fails the entire graph.
func readBundleHistory(s *Store, ref string) ([]Bundle, error) {
	if s == nil || ref == "" {
		return nil, errBundle
	}
	history := []Bundle{}
	seen := map[string]bool{}
	checks := newBundleChecks()
	for ref != "" {
		if seen[ref] || len(history) >= s.maxFiles {
			return nil, errBundle
		}
		seen[ref] = true
		raw, err := s.Get(ref)
		if err != nil {
			return nil, err
		}
		var b Bundle
		if strictRecord(raw, &b) != nil {
			return nil, errBundle
		}
		if len(history) > 0 {
			parent := history[len(history)-1]
			if parent.ProtocolRef != b.ProtocolRef || !b.CreatedAt.Before(parent.CreatedAt) {
				return nil, errBundle
			}
		}
		if _, err := validateBundleNode(s, b, checks); err != nil {
			return nil, err
		}
		history = append(history, b)
		ref = b.Previous
	}
	return history, nil
}

func validateBundleNode(s *Store, b Bundle, checks *bundleChecks) (Bundle, error) {
	if b.Format != "prospective-exit-bundle-v1" || b.ReplayReady || b.CreatedAt.IsZero() || !validStatus(b.Status) || len(b.Samples) > 1000 || b.Entries.Rows < 0 || b.Entries.Rows > 1000 || b.Entries.Rows < len(b.Samples) {
		return Bundle{}, errBundle
	}
	p, err := LoadProtocol(s, b.ProtocolRef)
	if err != nil {
		return Bundle{}, err
	}
	if b.LastSuccessfulAt != nil && b.LastSuccessfulAt.After(b.CreatedAt) {
		return Bundle{}, errBundle
	}
	seen := map[string]bool{}
	if b.Status == "complete" && (b.Entries.Truncated || b.Entries.Missing != 0 || b.Entries.Invalid != 0 || b.Entries.Incomplete != 0 || len(b.Samples) != b.Entries.Rows) {
		return Bundle{}, errBundle
	}
	for _, sample := range b.Samples {
		checked, exists := checks.snapshots[sample.SnapshotRef]
		if !exists {
			raw, err := s.Get(sample.SnapshotRef)
			if err != nil {
				return Bundle{}, err
			}
			in, status, err := DecodeSnapshot(raw, p.ID)
			if err != nil {
				return Bundle{}, err
			}
			checked = checkedSnapshot{in, status}
			checks.snapshots[sample.SnapshotRef] = checked
		}
		in, status := checked.input, checked.status
		if seen[in.CaptureID] || sample.CaptureID != in.CaptureID || sample.Symbol != in.Symbol || sample.Capture != status || !sample.EntryAt.Equal(in.EntryAt) || !sample.EndAt.Equal(in.EntryAt.Add(72*time.Hour)) || in.EntryAt.Before(p.Start) || !in.EntryAt.Before(p.End) || sample.MetadataHistoryVerified || !validStatus(sample.Market) || len(sample.Components) > 8 {
			return Bundle{}, errBundle
		}
		seen[in.CaptureID] = true
		if err := validateScopes(in, sample); err != nil {
			return Bundle{}, err
		}
		if b.Status == "complete" && (sample.Capture != "complete" || sample.Market != "complete" || sample.Reconciliation != "matched") {
			return Bundle{}, errBundle
		}
		for _, component := range sample.Components {
			keyBytes, _ := json.Marshal(struct {
				Symbol, Reconciliation, Costs string
				Component                     Component
			}{sample.Symbol, sample.Reconciliation, sample.Costs, component})
			key := sha256.Sum256(keyBytes)
			if !checks.components[key] {
				if err := validateComponent(s, sample, component); err != nil {
					return Bundle{}, err
				}
				checks.components[key] = true
			}
			if b.Status == "complete" && component.Kind != "ledger" && component.Status != "complete" {
				return Bundle{}, errBundle
			}
		}
	}
	return b, nil
}
func validateComponent(s *Store, sample Sample, c Component) error {
	switch c.Kind {
	case "trade", "mark", "funding", "metadata", "boundary":
		if !validStatus(c.Status) {
			return errBundle
		}
	case "ledger":
		if c.Status != "pending" && c.Status != "matched" && c.Status != "mismatch" {
			return errBundle
		}
	default:
		return errBundle
	}
	if len(c.Refs) > 40 {
		return errBundle
	}
	if len(c.Refs) == 0 {
		if c.Status == "complete" || c.Status == "matched" {
			return errBundle
		}
		return nil
	}
	if c.Kind != "boundary" && len(c.Refs) != 1 {
		return errBundle
	}
	switch c.Kind {
	case "trade", "mark":
		var a CandleArchive
		if readEvidence(s, c.Refs[0], c.Kind, &a) != nil || a.Symbol != sample.Symbol || a.Source != c.Kind || a.From.Before(c.From) || a.To.After(c.To) || len(a.Candles) > 4320 {
			return errBundle
		}
		if c.Status == "complete" && (a.Coverage.Status != "complete" || !a.From.Equal(c.From) || !a.To.Equal(c.To) || a.MissingMinutes != 0 || a.ExpectedMinutes != int(c.To.Sub(c.From)/time.Minute) || len(a.Candles) != a.ExpectedMinutes) {
			return errBundle
		}
		for i, row := range a.Candles {
			if row.At.Before(a.From) || !row.At.Before(a.To) || !row.At.Equal(row.At.Truncate(time.Minute)) || i > 0 && !row.At.After(a.Candles[i-1].At) {
				return errBundle
			}
		}
	case "funding":
		var a FundingArchive
		if readEvidence(s, c.Refs[0], c.Kind, &a) != nil || a.Symbol != sample.Symbol || a.ScheduleVerified || a.Costs == "verified" || len(a.Rates) > 20000 {
			return errBundle
		}
		if c.Status == "complete" && (a.Coverage.Status != "complete" || !a.From.Equal(c.From) || !a.To.Equal(c.To)) {
			return errBundle
		}
	case "metadata":
		var a MetadataObservation
		if readEvidence(s, c.Refs[0], c.Kind, &a) != nil || a.Symbol != sample.Symbol || a.HistoryVerified || a.Status != c.Status {
			return errBundle
		}
	case "ledger":
		var a LedgerReport
		if readEvidence(s, c.Refs[0], c.Kind, &a) != nil || a.Net != nil || a.Costs == "verified" || len(a.Executions) > 20000 || a.Reconciliation != c.Status || a.Reconciliation != sample.Reconciliation || a.Costs != sample.Costs {
			return errBundle
		}
	case "boundary":
		total := 0
		ticks := []Tick{}
		var first *BoundaryArchive
		for i, ref := range c.Refs {
			var piece boundaryPiece
			if readEvidence(s, ref, c.Kind, &piece) != nil || piece.Part != i || piece.Parts != len(c.Refs) || len(piece.Archive.Trades) > 500 || piece.Archive.Symbol != sample.Symbol || !piece.Archive.Segment.Start.Equal(c.From) || !piece.Archive.Segment.End.Equal(c.To) {
				return errBundle
			}
			if first == nil {
				v := piece.Archive
				first = &v
			} else if piece.Archive.Coverage != first.Coverage {
				return errBundle
			}
			total += len(piece.Archive.Trades)
			for _, row := range piece.Archive.Trades {
				if row.At.Before(c.From) || !row.At.Before(c.To) {
					return errBundle
				}
				ticks = append(ticks, Tick{ID: row.ID, At: row.At, Price: row.Price})
			}
		}
		if first == nil || total != first.Coverage.Records {
			return errBundle
		}
		report, err := BoundaryOHLC(first.Segment, ticks, first.Coverage.WindowTraversed)
		if err != nil || report.Trades != total || first.Coverage.Status != c.Status {
			return errBundle
		}
		left, _ := json.Marshal(report)
		right, _ := json.Marshal(first.Report)
		if string(left) != string(right) {
			return errBundle
		}
	default:
		return errBundle
	}
	return nil
}

func validateScopes(in models.ResearchCaptureInput, s Sample) error {
	type scope struct {
		kind     string
		from, to time.Time
	}
	key := func(k string, f, t time.Time) scope { return scope{k, f.UTC(), t.UTC()} }
	expected := map[scope]bool{key("metadata", time.Time{}, time.Time{}): true, key("ledger", time.Time{}, time.Time{}): true}
	exact := in.Evidence.TimeSource == "exchange_fills" && in.EntryAt.Nanosecond()%int(time.Millisecond) == 0
	if exact {
		segments, err := PlanSegments(in.EntryAt)
		if err != nil {
			return errBundle
		}
		var from, to time.Time
		for _, seg := range segments {
			if seg.Kind == "boundary" {
				expected[key("boundary", seg.Start, seg.End)] = true
			} else {
				if from.IsZero() {
					from = seg.Start
				}
				to = seg.End
			}
		}
		expected[key("trade", from, to)] = true
		expected[key("mark", from, to)] = true
		expected[key("funding", in.EntryAt, s.EndAt)] = true
	} else if s.Market == "complete" {
		return errBundle
	}
	if len(expected) != len(s.Components) {
		return errBundle
	}
	for _, c := range s.Components {
		k := key(c.Kind, c.From, c.To)
		if !expected[k] {
			return errBundle
		}
		delete(expected, k)
		if s.Market == "complete" && (c.Kind == "trade" || c.Kind == "mark" || c.Kind == "boundary") && c.Status != "complete" {
			return errBundle
		}
	}
	return nil
}
func latestBundle(s *Store, protocol string, before time.Time) (string, *Bundle, error) {
	refs, err := s.Refs()
	if err != nil {
		return "", nil, err
	}
	var latest *Bundle
	var key string
	for _, ref := range refs {
		raw, err := s.Get(ref)
		if err != nil {
			return "", nil, err
		}
		var tag struct {
			Format string `json:"format"`
		}
		if json.Unmarshal(raw, &tag) != nil {
			return "", nil, errBundle
		}
		if tag.Format != "prospective-exit-bundle-v1" {
			continue
		}
		var candidate Bundle
		if strictRecord(raw, &candidate) != nil {
			return "", nil, errBundle
		}
		if candidate.ProtocolRef != protocol {
			continue
		}
		if !candidate.CreatedAt.Before(before) {
			return "", nil, errors.New("research_pass_time_not_increasing")
		}
		if latest == nil || candidate.CreatedAt.After(latest.CreatedAt) || (candidate.CreatedAt.Equal(latest.CreatedAt) && ref > key) {
			v := candidate
			latest = &v
			key = ref
		}
	}
	if latest != nil {
		checked, err := ReadBundle(s, key)
		if err != nil {
			return "", nil, err
		}
		latest = &checked
	}
	return key, latest, nil
}
func saveSnapshot(s *Store, in models.ResearchCaptureInput) (string, error) {
	b, code := models.BuildResearchEntrySnapshot(in)
	if code != "" {
		return "", errBundle
	}
	return s.Put(b)
}
func sortSamples(samples []Sample) {
	sort.Slice(samples, func(i, j int) bool { return samples[i].CaptureID < samples[j].CaptureID })
}
func saveBundle(s *Store, b Bundle) (string, error) {
	if _, err := validateBundle(s, b); err != nil {
		return "", err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return "", errBundle
	}
	return s.Put(raw)
}
