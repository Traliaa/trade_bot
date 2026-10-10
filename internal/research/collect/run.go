//go:build linux || darwin

package collect

import (
	"context"
	"errors"
	"time"
)

// RunPass performs a bounded one-shot collection from an already closed DB
// snapshot. It never opens the trading service, discovers credentials, or places
// orders. Independent source failures are saved as partial evidence.
func RunPass(ctx context.Context, store *Store, protocolRef string, batch EntryBatch, public *ArchiveClient, private *PrivateClient, now time.Time) (PassReport, error) {
	report := PassReport{Status: "incomplete", Entries: batch.Coverage}
	p, err := LoadProtocol(store, protocolRef)
	if err != nil {
		return report, err
	}
	now = now.UTC().Truncate(time.Millisecond)
	if public == nil || public.store != store || private != nil && (private.engine == nil || private.engine.store != store) || now.IsZero() || now.Before(p.Start) || len(batch.entries) > 1000 || batch.Coverage.Rows != len(batch.entries) {
		return report, errors.New("research_pass_scope_invalid")
	}
	store.mu.Lock()
	if store.maxBytes > p.MaxBytes {
		store.maxBytes = p.MaxBytes
	}
	_, err = store.scan(false, 0)
	store.mu.Unlock()
	if err != nil {
		return report, err
	}
	previous, old, err := latestBundle(store, protocolRef, now)
	if err != nil {
		return report, err
	}
	var history []Bundle
	if previous != "" {
		history, err = readBundleHistory(store, previous)
		if err != nil {
			return report, err
		}
	}
	b := Bundle{Format: "prospective-exit-bundle-v1", ProtocolRef: protocolRef, CreatedAt: now, Previous: previous, Status: "complete", Entries: batch.Coverage, Samples: []Sample{}}
	if old != nil {
		b.LastSuccessfulAt = old.LastSuccessfulAt
	}
	if batch.Coverage.Truncated || batch.Coverage.Missing > 0 || batch.Coverage.Invalid > 0 || batch.Coverage.Incomplete > 0 {
		b.Status = "incomplete"
	}
	observedMetadata := map[string]MetadataObservation{}
	for _, entry := range batch.entries {
		if ctx.Err() != nil {
			return report, errors.New("archive_cancelled")
		}
		if entry.capture == nil {
			continue
		}
		in := entry.capture
		if in.ProtocolID != p.ID || in.EntryAt.Before(p.Start) || !in.EntryAt.Before(p.End) || in.EntryAt.After(now) {
			return report, errors.New("research_pass_entry_invalid")
		}
		snapshot, err := saveSnapshot(store, *in)
		if err != nil {
			return report, err
		}
		sample := Sample{CaptureID: in.CaptureID, SnapshotRef: snapshot, Symbol: in.Symbol, EntryAt: in.EntryAt, EndAt: in.EntryAt.Add(72 * time.Hour), Capture: entry.captureStatus, Market: "complete", Reconciliation: "pending", Costs: "unknown", Components: []Component{}}
		// Completed immutable market observations remain valid evidence even
		// when the remote archive later expires. Refresh metadata and private
		// attribution, but reuse only checksum-validated exact-scope components.
		reuse := func(kind string, from, to time.Time) (Component, bool) {
			for _, previousPass := range history {
				for _, prior := range previousPass.Samples {
					if prior.SnapshotRef != snapshot {
						continue
					}
					for _, component := range prior.Components {
						if component.Kind == kind && component.Status == "complete" && component.From.Equal(from) && component.To.Equal(to) {
							return component, true
						}
					}
				}
			}
			return Component{}, false
		}
		add := func(c Component, value any) error {
			if value != nil {
				ref, err := saveEvidence(store, c.Kind, value)
				if err != nil {
					return err
				}
				c.Refs = []string{ref}
			}
			if c.Refs == nil {
				c.Refs = []string{}
			}
			sample.Components = append(sample.Components, c)
			return nil
		}
		metadata, exists := observedMetadata[in.Symbol]
		if !exists {
			metadata, _ = public.CollectMetadata(ctx, in.Symbol)
			observedMetadata[in.Symbol] = metadata
		}
		var metadataValue any
		if metadata.Symbol == in.Symbol {
			metadataValue = metadata
		}
		if err := add(Component{Kind: "metadata", Status: metadata.Status, Reason: metadata.Reason}, metadataValue); err != nil {
			return report, err
		}
		if old != nil && metadata.Status == "complete" {
			for _, prior := range old.Samples {
				if prior.CaptureID != sample.CaptureID {
					continue
				}
				for _, component := range prior.Components {
					if component.Kind != "metadata" || len(component.Refs) != 1 {
						continue
					}
					var m MetadataObservation
					if readEvidence(store, component.Refs[0], "metadata", &m) == nil && m.Status == "complete" {
						m.ObservedAt = metadata.ObservedAt
						changed := m != metadata
						sample.MetadataChanged = &changed
					}
				}
			}
		}
		exactTime := in.Evidence.TimeSource == "exchange_fills" && in.EntryAt.Nanosecond()%int(time.Millisecond) == 0
		if exactTime {
			segments, err := PlanSegments(in.EntryAt)
			if err != nil {
				return report, err
			}
			var minuteFrom, minuteTo time.Time
			for _, seg := range segments {
				if seg.Kind == "minute" {
					if minuteFrom.IsZero() {
						minuteFrom = seg.Start
					}
					minuteTo = seg.End
					continue
				}
				component := Component{Kind: "boundary", From: seg.Start, To: seg.End, Status: "pending", Reason: "horizon_pending", Refs: []string{}}
				if prior, ok := reuse("boundary", seg.Start, seg.End); ok {
					sample.Components = append(sample.Components, prior)
					continue
				}
				if !seg.End.After(now) {
					a, _ := public.CollectBoundaryTrades(ctx, MarketWindow{in.Symbol, seg.Start, seg.End, p.MaxPages})
					component.Status = a.Coverage.Status
					component.Reason = a.Coverage.Reason
					if a.Symbol == in.Symbol {
						refs, err := saveBoundary(store, a)
						if err != nil {
							return report, err
						}
						component.Refs = refs
					}
				}
				if component.Status != "complete" {
					sample.Market = "incomplete"
				}
				sample.Components = append(sample.Components, component)
			}
			for _, source := range []string{"trade", "mark"} {
				if prior, ok := reuse(source, minuteFrom, minuteTo); ok {
					sample.Components = append(sample.Components, prior)
					continue
				}
				c := Component{Kind: source, From: minuteFrom, To: minuteTo, Status: "pending", Reason: "horizon_pending"}
				until := minuteTo
				if until.After(now.Truncate(time.Minute)) {
					until = now.Truncate(time.Minute)
				}
				var value any
				if until.After(minuteFrom) {
					a, _ := public.CollectCandles(ctx, MarketWindow{in.Symbol, minuteFrom, until, p.MaxPages}, source)
					c.Status = a.Coverage.Status
					c.Reason = a.Coverage.Reason
					if a.Symbol == in.Symbol {
						value = a
					}
					if until.Before(minuteTo) && c.Status == "complete" {
						c.Status = "pending"
						c.Reason = "horizon_pending"
					}
				}
				if c.Status != "complete" {
					sample.Market = "incomplete"
				}
				if err := add(c, value); err != nil {
					return report, err
				}
			}
			until := sample.EndAt
			if until.After(now) {
				until = now
			}
			c := Component{Kind: "funding", From: in.EntryAt, To: sample.EndAt, Status: "pending", Reason: "horizon_pending"}
			var value any
			priorFunding, reusedFunding := reuse("funding", in.EntryAt, sample.EndAt)
			if reusedFunding {
				c = priorFunding
			} else if until.After(in.EntryAt) {
				a, _ := public.CollectFunding(ctx, MarketWindow{in.Symbol, in.EntryAt, until, p.MaxPages})
				c.Status = a.Coverage.Status
				c.Reason = a.Coverage.Reason
				if a.Symbol == in.Symbol {
					value = a
				}
				if until.Before(sample.EndAt) && c.Status == "complete" {
					c.Status = "pending"
					c.Reason = "horizon_pending"
				}
			}
			if c.Status != "complete" {
				b.Status = "incomplete"
			}
			if err := add(c, value); err != nil {
				return report, err
			}
		} else {
			sample.Market = "incomplete"
		}
		ledger := LedgerReport{Reconciliation: "pending", Costs: "unknown", Reasons: []string{"private_access_pending"}, Executions: []Execution{}}
		if private != nil && exactTime {
			from := in.EntryAt.Add(-time.Millisecond)
			if in.Evidence.FirstFillAt != nil {
				from = in.Evidence.FirstFillAt.Add(-time.Millisecond)
			}
			w := FillWindow{Symbol: in.Symbol, From: from, To: now, MaxPages: p.MaxPages}
			a, fillErr := private.CollectFills(ctx, w)
			ledger = attributeEntry(entry, a, w)
			if fillErr != nil {
				ledger.Reasons = append(ledger.Reasons, "private_archive_incomplete")
			}
		}
		sample.Reconciliation = ledger.Reconciliation
		sample.Costs = ledger.Costs
		if err := add(Component{Kind: "ledger", Status: ledger.Reconciliation}, ledger); err != nil {
			return report, err
		}
		if sample.Capture != "complete" || sample.Market != "complete" || sample.Reconciliation != "matched" || metadata.Status != "complete" {
			b.Status = "incomplete"
		}
		b.Samples = append(b.Samples, sample)
	}
	if ctx.Err() != nil {
		return report, errors.New("archive_cancelled")
	}
	sortSamples(b.Samples)
	if b.Status == "complete" {
		at := now
		b.LastSuccessfulAt = &at
	}
	ref, err := saveBundle(store, b)
	if err != nil {
		return report, err
	}
	report = PassReport{Status: b.Status, BundleRef: ref, Samples: len(b.Samples), Entries: b.Entries, LastSuccessfulAt: b.LastSuccessfulAt}
	return report, nil
}
