# Research collector core implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Build a deterministic offline core for trustworthy prospective research inputs, without changing live accounting.
**Architecture:** Pure functions in `internal/research/collect` consume the existing entry snapshot and privately attributed fills. Outputs whitelist non-identifying evidence and independent statuses. Network adapters and the collector process will consume this core in the next integration stage, not the trading runner.
**Tech Stack:** Go 1.27, encoding/json, math/big, existing models; no dependencies.
**Spec:** `docs/superpowers/specs/2026-10-05-prospective-exit-data-design.md`, sections 2–5; capture producer already exists.

## Global Constraints

- No trading changes, live API requests, production writes, capture activation, push or deployment.
- Private IDs used only in memory; output has no account/order/fill IDs or raw errors.
- Decimal arithmetic before any float conversion; unknown funding never becomes zero.
- Independent capture, reconciliation, costs and market coverage; no overall verified boolean.
- Exact horizon 72h, half-open intervals; no automatic v1 replay conversion.
- Capture stays off. Full collector (read-only DB/HTTP, quota, private volume, archive pagination) remains a separate integration stage; this core alone is not a launched collector.

## Review Focus

1. JSONB key reordering must not break producer checksums; unknown/missing/duplicate keys cannot silently lose evidence (Task 1).
2. A duplicated private fill with altered order or amount must not become two executions (Task 2).
3. AVAX-style extra volume and DB-open/exchange-flat must remain mismatch, not guessed closure (Task 2).
4. Fees/rebates in an unknown currency must not produce a USDT total; missing funding must leave full net null (Task 2).
5. Intraminute entry and right-boundary ticks must not shift the 72h horizon (Task 3).

### Task 1: Snapshot verification

**Files:** `internal/research/collect/snapshot.go`, `snapshot_test.go`.
**Interfaces:** `DecodeSnapshot(raw []byte, protocol string) (models.ResearchCaptureInput, string, error)`; returns producer status. Errors contain fixed codes only.

- [x] Write `TestSnapshotJSONB`, `TestSnapshotRejectsAlteration`, `TestSnapshotIncomplete`: producer fixture roundtrip/key reorder accepted; changed amount/checksum/protocol, duplicate/unknown/missing fields and >32KiB rejected; incomplete stays incomplete.
- [x] Run `go test ./internal/research/collect -run TestSnapshot -count=1`, expect RED for missing function.
- [x] Implement bounded strict decode, duplicate-key rejection, rebuild through canonical producer, checksum and complete field-set comparison; no reinterpretation as archived evidence.
- [x] Same command GREEN; no network.

### Task 2: Exact execution reconciliation

**Files:** `internal/research/collect/ledger.go`, `ledger_test.go`.
**Interfaces:** `Fill` (private IDs/order, role inferred from expected orders, symbol, side, posSide, At, Size/Price/Fee/PnL/FeeCurrency); `LedgerInput` (symbol/side, entry window, cutoff, entry size, entry/exit order sets, fills, archiveComplete, DB state, optional exchange remaining, optional recorded net excluding funding, optional verified funding evidence). Input types refuse JSON serialization. `Reconcile(LedgerInput) LedgerReport`; report only fixed reasons, counts, canonical decimal sums, whitelisted anonymous executions and independent reconciliation/cost statuses.

- [x] Write tests for ARB exact 0.4+0.9+0.4+0.1=1.8; dedup; altered duplicates; foreign order/symbol/side; wrong time; AVAX 1 vs1.5; exit before entry; multi-time entry; signed rebates; currency mismatch; incomplete archive; DB-open/exchange-flat; recorded net mismatch; missing funding vs independently verified zero. Report contains no sentinel private IDs.
- [x] Run `go test ./internal/research/collect -run TestLedger -count=1`, expect RED.
- [x] Implement bounded plain decimal parser (32 integer/32 fraction digits), exact sums, semantic duplicate checks, role/chronological conservation checks. Reject ambiguous multi-order entry attribution; incomplete/pending results cannot claim reconciled net. No DB repair, no live runner imports.
- [x] Same command GREEN, plus Task 1 tests.

### Task 3: Exact horizon segmentation and handoff

**Files:** `internal/research/collect/segments.go`, `segments_test.go`; `docs/research-entry-capture.md` append core usage/limits.
**Interfaces:** `PlanSegments(entry time.Time) ([]Segment,error)` where Segment has Kind, Start, End; `BoundaryOHLC(segment Segment, ticks []Tick, sourceComplete bool) (BoundaryReport,error)`. Only safe numeric/time output; no synthetic prices for empty segments.

- [x] Write `TestSegmentsExact72Hours`, `TestBoundaryRightExclusive`, `TestBoundaryEmptyIncomplete`: aligned and intraminute entries have contiguous exact72h coverage; 4319 full bars for intraminute entry; ticks before/after excluded, duplicate/conflicting public IDs tested, unproven empty remains incomplete with nil OHLC.
- [x] Run `go test ./internal/research/collect -run 'Test(Segments|Boundary)' -count=1`, expect RED.
- [x] Implement half-open intervals without horizon rounding, bounded tick validation and exact decimal min/max; absence of ticks never invents OHLC.
- [x] Run full package/race and whole-project tests, build and changed-package vet; strict self-review plus fresh reviewer.
- [x] Record results and remaining integration work. Keep changes local with previous Telegram/UI batch as requested; no intermediate push/production launch.

## Execution rulings

- User asked to proceed without intermediate questions: inline implementation after plan self-review, no additional approval pause.
- Preserve previous uncommitted Telegram/UI work. User previously authorized master; no new worktree or unrelated reset.
- Commits/push deferred for the combined batch. This plan implements the independently testable core, not all of the network/storage specification. Cost: actual prospective collection remains unavailable until integration/access/launch gates are completed.
