//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func studyHTTP(t *testing.T) (*ArchiveClient, string, EntryBatch, time.Time) {
	t.Helper()
	e, _, _ := linkedFixture(t)
	e.orderID = "PRIVATE-ENTRY-ONLY"
	now := e.entryAt.Add(73 * time.Hour)
	c, dir := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v5/public/instruments":
			return marketResponse([]map[string]string{metadataWire()}), nil
		case "/api/v5/public/funding-rate-history", "/api/v5/market/history-trades":
			return marketResponse([]map[string]string{}), nil
		case "/api/v5/market/history-candles", "/api/v5/market/history-mark-price-candles":
			source := "trade"
			if strings.Contains(r.URL.Path, "mark-price") {
				source = "mark"
			}
			cursor, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
			at := time.UnixMilli(cursor).Add(-time.Millisecond).Truncate(time.Minute)
			rows := [][]string{}
			for i := 0; i < 100; i++ {
				rows = append(rows, wireCandle(at.Add(-time.Duration(i)*time.Minute), source))
			}
			return marketResponse(rows), nil
		default:
			t.Fatal("unexpected external path", r.URL.Path)
			return nil, nil
		}
	})
	c.store.maxBytes = 1 << 30
	clock := now
	c.now = func() time.Time { return clock }
	c.wait = func(ctx context.Context, d time.Duration) error {
		if d > 0 {
			clock = clock.Add(d)
		}
		return ctx.Err()
	}
	return c, dir, EntryBatch{entries: []entryRecord{e}, Coverage: EntryCoverage{Rows: 1, Complete: 1}}, now
}
func TestRunBundleHorizonAndPrivacy(t *testing.T) {
	c, dir, b, now := studyHTTP(t)
	p := studyProtocol()
	ref, err := RegisterProtocol(c.store, p, p.Start)
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunPass(context.Background(), c.store, ref, b, c, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := ReadBundle(c.store, report.BundleRef)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Format != "prospective-exit-bundle-v1" || bundle.ReplayReady || len(bundle.Samples) != 1 || bundle.Samples[0].Market != "complete" || bundle.Samples[0].Reconciliation != "pending" || bundle.Samples[0].Costs != "unknown" {
		t.Fatalf("wrong independent coverage %+v", bundle)
	}
	s := bundle.Samples[0]
	if !s.EndAt.Equal(s.EntryAt.Add(72*time.Hour)) || len(s.Components) != 7 {
		t.Fatal("wrong full horizon components", s)
	}
	refs, err := c.store.Refs()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		v, _ := c.store.Get(r)
		if strings.Contains(string(v), "PRIVATE") || strings.Contains(string(v), "private_order") || strings.Contains(string(v), `"row_id"`) {
			t.Fatal("private data exported")
		}
	}
	first, _ := json.Marshal(bundle)
	again, _ := ReadBundle(c.store, report.BundleRef)
	second, _ := json.Marshal(again)
	if string(first) != string(second) {
		t.Fatal("nondeterministic export")
	}
	rerun, err := RunPass(context.Background(), c.store, ref, b, c, nil, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	next, err := ReadBundle(c.store, rerun.BundleRef)
	if err != nil || next.Previous != report.BundleRef {
		t.Fatal("lost prior pass", err)
	}
	if err := c.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := ReadBundle(reopened, rerun.BundleRef); err != nil {
		t.Fatal("restart lost graph", err)
	}
}
func TestRunPartialAndCorruptGraph(t *testing.T) {
	c, dir, b, now := studyHTTP(t)
	p := studyProtocol()
	ref, _ := RegisterProtocol(c.store, p, p.Start)
	report, err := RunPass(context.Background(), c.store, ref, b, c, nil, b.entries[0].entryAt.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := ReadBundle(c.store, report.BundleRef)
	if err != nil || bundle.Samples[0].Market == "complete" || report.Status == "complete" {
		t.Fatal("unfinished horizon certified", err)
	}
	b.Coverage.Truncated = true
	b.entries[0].capture = nil
	b.entries[0].captureStatus = "missing"
	b.Coverage.Complete = 0
	b.Coverage.Missing = 1
	report, err = RunPass(context.Background(), c.store, ref, b, c, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = ReadBundle(c.store, report.BundleRef)
	if err != nil || !bundle.Entries.Truncated || len(bundle.Samples) != 0 || report.Status == "complete" {
		t.Fatal("missing capture invented", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ref+".json"), []byte(`{"bad":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBundle(c.store, report.BundleRef); err == nil {
		t.Fatal("corrupt reference accepted")
	}
}
func TestRunSourceFailureAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		c, _, b, now := studyHTTP(t)
		p := studyProtocol()
		ref, _ := RegisterProtocol(c.store, p, p.Start)
		c.http.Transport = archiveTransport(func(*http.Request) (*http.Response, error) { return archiveResponse(403, `PRIVATE upstream`), nil })
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		report, err := RunPass(ctx, c.store, ref, b, c, nil, now)
		cancel()
		if report.Status == "complete" {
			t.Fatal("failed source certified")
		}
		if cancelled {
			if err == nil {
				t.Fatal("lost cancellation")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		v, err := ReadBundle(c.store, report.BundleRef)
		if err != nil || v.Samples[0].Market == "complete" {
			t.Fatal("lost partial graph", err)
		}
	}
}

func TestRunExpiredArchiveRemainsInspectable(t *testing.T) {
	c, _, b, now := studyHTTP(t)
	p := studyProtocol()
	ref, _ := RegisterProtocol(c.store, p, p.Start)
	now = now.Add(90 * 24 * time.Hour)
	c.now = func() time.Time { return now }
	c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	report, err := RunPass(context.Background(), c.store, ref, b, c, nil, now)
	if err != nil {
		t.Fatal("expired source lost whole coverage report", err)
	}
	bundle, err := ReadBundle(c.store, report.BundleRef)
	if err != nil || bundle.Samples[0].Market == "complete" {
		t.Fatal("expired archive certified", err)
	}
}
func TestBundleRejectsInconsistentGraph(t *testing.T) {
	c, _, b, now := studyHTTP(t)
	p := studyProtocol()
	ref, _ := RegisterProtocol(c.store, p, p.Start)
	report, err := RunPass(context.Background(), c.store, ref, b, c, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"unknown_kind", "missing_market", "ledger_status", "pass_status", "market_status"} {
		t.Run(kind, func(t *testing.T) {
			graph, err := ReadBundle(c.store, report.BundleRef)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "unknown_kind":
				graph.Samples[0].Components = append(graph.Samples[0].Components, Component{Kind: "not_supported", Status: "pending", Refs: []string{}})
			case "missing_market":
				graph.Samples[0].Components = graph.Samples[0].Components[:1]
			case "ledger_status":
				graph.Samples[0].Reconciliation = "matched"
			case "pass_status":
				graph.Status = "complete"
			case "market_status":
				for i := range graph.Samples[0].Components {
					if graph.Samples[0].Components[i].Kind == "trade" {
						graph.Samples[0].Components[i].Status = "pending"
					}
				}
			}
			raw, _ := json.Marshal(graph)
			bad, err := c.store.Put(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ReadBundle(c.store, bad); err == nil {
				t.Fatal("inconsistent graph certified", kind)
			}
		})
	}
}

func TestRunReusesValidatedMarket(t *testing.T) {
	c, _, b, now := studyHTTP(t)
	p := studyProtocol()
	ref, _ := RegisterProtocol(c.store, p, p.Start)
	first, err := RunPass(context.Background(), c.store, ref, b, c, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	prior, _ := ReadBundle(c.store, first.BundleRef)
	calls := 0
	c.http.Transport = archiveTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return archiveResponse(403, "unavailable"), nil
	})
	next, err := RunPass(context.Background(), c.store, ref, b, c, nil, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := ReadBundle(c.store, next.BundleRef)
	if err != nil || graph.Samples[0].Market != "complete" || calls != 1 {
		t.Fatal("lost verified market or refetched archive", err, calls)
	}
	for _, old := range prior.Samples[0].Components {
		if old.Kind == "metadata" || old.Kind == "ledger" {
			continue
		}
		found := false
		for _, new := range graph.Samples[0].Components {
			if new.Kind == old.Kind && new.From.Equal(old.From) && len(new.Refs) > 0 && new.Refs[0] == old.Refs[0] {
				found = true
			}
		}
		if !found {
			t.Fatal("lost archived reference")
		}
	}
}

func TestSaveBundleRejectsBeforePublication(t *testing.T) {
	c, _, _, now := studyHTTP(t)
	p := studyProtocol()
	ref, _ := RegisterProtocol(c.store, p, p.Start)
	before, _ := c.store.Refs()
	_, err := saveBundle(c.store, Bundle{Format: "prospective-exit-bundle-v1", ProtocolRef: ref, CreatedAt: now, Status: "complete", ReplayReady: true})
	after, _ := c.store.Refs()
	if err == nil || len(before) != len(after) {
		t.Fatal("invalid bundle poisoned resume")
	}
}

func TestRunRejectsZeroPrivateClient(t *testing.T) {
	c, _, b, now := studyHTTP(t)
	p := studyProtocol()
	ref, _ := RegisterProtocol(c.store, p, p.Start)
	if _, err := RunPass(context.Background(), c.store, ref, b, c, &PrivateClient{}, now); err == nil {
		t.Fatal("invalid client accepted")
	}
}

func TestBoundaryBundleChunkRoundTrip(t *testing.T) {
	c, _, batch, _ := studyHTTP(t)
	from := batch.entries[0].entryAt
	to := from.Truncate(time.Minute).Add(time.Minute)
	a := BoundaryArchive{Symbol: batch.entries[0].symbol, Source: "okx_history_trades", Segment: Segment{Kind: "boundary", Start: from, End: to}, Coverage: ArchiveCoverage{Status: "complete", WindowTraversed: true, Records: 501}, Trades: []PublicTrade{}}
	ticks := []Tick{}
	for i := 0; i < 501; i++ {
		id := strconv.Itoa(i + 1)
		at := from.Add(time.Duration(i) * time.Millisecond)
		a.Trades = append(a.Trades, PublicTrade{ID: id, At: at, Price: "100", Size: "1", Side: "buy", Source: "0"})
		ticks = append(ticks, Tick{ID: id, At: at, Price: "100"})
	}
	var err error
	a.Report, err = BoundaryOHLC(a.Segment, ticks, true)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := saveBoundary(c.store, a)
	if err != nil || len(refs) != 2 {
		t.Fatal("chunk failure", err)
	}
	component := Component{Kind: "boundary", From: from, To: to, Status: "complete", Refs: refs}
	if err := validateComponent(c.store, Sample{Symbol: a.Symbol}, component); err != nil {
		t.Fatal(err)
	}
	component.Refs = []string{refs[1], refs[0]}
	if err := validateComponent(c.store, Sample{Symbol: a.Symbol}, component); err == nil {
		t.Fatal("reordered chunks accepted")
	}
}

func TestRunResumeAcrossMissingCapture(t *testing.T) {
	c, _, batch, now := studyHTTP(t)
	p := studyProtocol()
	ref, _ := RegisterProtocol(c.store, p, p.Start)
	if _, err := RunPass(context.Background(), c.store, ref, batch, c, nil, now); err != nil {
		t.Fatal(err)
	}
	missing := EntryBatch{entries: []entryRecord{{captureStatus: "missing"}}, Coverage: EntryCoverage{Rows: 1, Missing: 1}}
	if _, err := RunPass(context.Background(), c.store, ref, missing, c, nil, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c.http.Transport = archiveTransport(func(*http.Request) (*http.Response, error) { calls++; return archiveResponse(403, "unavailable"), nil })
	r, err := RunPass(context.Background(), c.store, ref, batch, c, nil, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ReadBundle(c.store, r.BundleRef)
	if err != nil || b.Samples[0].Market != "complete" || calls != 1 {
		t.Fatal("earlier coverage stranded", err, calls)
	}
}

func TestBundleRejectsBrokenAncestorEvidence(t *testing.T) {
	c, dir, batch, now := studyHTTP(t)
	p := studyProtocol()
	ref, _ := RegisterProtocol(c.store, p, p.Start)
	first, err := RunPass(context.Background(), c.store, ref, batch, c, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := ReadBundle(c.store, first.BundleRef)
	next, err := RunPass(context.Background(), c.store, ref, EntryBatch{entries: []entryRecord{}, Coverage: EntryCoverage{}}, c, nil, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var target string
	for _, c := range old.Samples[0].Components {
		if c.Kind == "metadata" {
			target = c.Refs[0]
		}
	}
	if target == "" {
		t.Fatal("no ancestor evidence")
	}
	if err := os.Remove(filepath.Join(dir, target+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBundle(c.store, next.BundleRef); err == nil {
		t.Fatal("broken ancestor accepted")
	}
}
