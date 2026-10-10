# Research boundary trades implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collect public trades for partial first/last minutes without pre-entry or post-horizon prices, skipped timestamp ties, or invented OHLC.

**Architecture:** Add a typed boundary adapter on the existing read-only ArchiveClient. Seek once by timestamp, then traverse by numeric public trade ID; retain exact decimal strings and use BoundaryOHLC for conservative ordering/coverage. This implements another slice of the approved collector, not its CLI or production launch.

**Tech Stack:** Go 1.27, standard library, existing private Store and HTTP quota gate.

**Spec:** `docs/superpowers/specs/2026-10-05-prospective-exit-data-design.md`

## Global Constraints

- No changes to live trading, production data, notifications, schedules or credentials; no push/deploy.
- Exact half-open boundary intervals, less than one minute and not crossing a minute boundary; millisecond timestamps.
- Shared one-request/second, 15s timeout, three transient retries, 4 MiB response, 200 pages/component/pass, 10000 requests/day.
- No private identifiers/raw responses in output. Public market IDs are allowed.
- Missing or ambiguous evidence stays incomplete; an empty traversed window has no synthetic OHLC.
- Existing in-place master workflow and accumulated dirty changes remain intact. Commit/release together later per user preference.

## Review Focus

- More than 100 trades sharing one timestamp: switch to ID pagination, never advance past the tie by time.
- Overlapping, repeated, contradictory or time-regressing pages: deduplicate identical rows, fail closed otherwise.
- Cancellation during a terminal response, quota or page exhaustion: do not certify completeness.
- Exact entry/end millisecond and empty windows: include start, exclude end, never borrow outside prices.
- Malformed/aliased fields, oversized IDs or unexpected sources: validate atomically before exposing any row from a page.

### Task 1: Typed boundary archive

**Files:** Create `internal/research/collect/market_trades.go`, `internal/research/collect/market_trades_test.go`; update `docs/research-entry-capture.md`.

**Interfaces:**
- Consumes `MarketWindow`, `ArchiveCoverage`, `ArchiveClient.Get`, `BoundaryOHLC`, decimal and exact-object helpers.
- Produces `CollectBoundaryTrades(context.Context, MarketWindow) (BoundaryArchive, error)`; archive contains symbol/source/segment, coverage, boundary report and whitelisted `PublicTrade` records (ID, At, Price, Size, Side, Source).
- First GET history-trades has `type=2&after=To.UnixMilli()`; later requests use `type=1&after=<oldest public ID>`, always `limit=100`. Documented earlier-than semantics ensure wanted timestamps are below To. Exact boundary rows returned by inclusive overlap are filtered locally.
- Validate canonical positive IDs up to 64 digits, positive decimal price/size, buy/sell, source 0/1, matching symbol and canonical timestamp no later than pass start. Sort page by numeric ID descending and require nonincreasing time across new IDs. A repeated seen ID may overlap but must be numerically equal in all decimal fields and identical otherwise. Reject unknown IDs newer than the cursor; require strictly older fresh IDs. End traversal at an empty page or a validated trade before From, never at a short page or timestamp equal to From.
- `WindowTraversed` describes the observed API traversal, not a certified immutable exchange snapshot. Same-millisecond different prices remain `tick_order_ambiguous`, with raw trades retained and no OHLC.

- [x] Write tests for inclusive start/exclusive end, short/overlapping pages and raw-decimal whitelist export; a 150-trade timestamp tie split across pages; ambiguity, no prices, initial empty result; malformed rows, duplicate conflicts and cursor/time regressions; invalid scope, cancellation, quota and page exhaustion. All use the real Store and a fake HTTP transport only.
- [x] Run `GOSUMDB=sum.golang.org GOTOOLCHAIN=go1.27.0 go test ./internal/research/collect -run TestBoundaryArchive -count=1`; expected RED for missing adapter.
- [x] Implement the interface and bounded ID traversal; build boundary report without changing existing segment mathematics.
- [x] Run focused tests then `go test -race ./...`, `go vet ./internal/research/collect`, `go build ./...` and Linux collector compile with the pinned toolchain; expect tests/build pass, report existing global-vet limitations separately.
- [x] Update operational docs; perform self-review and one fresh independent final review. Preserve ledger and local changes until the combined release; no commit/push/deploy in this slice.
