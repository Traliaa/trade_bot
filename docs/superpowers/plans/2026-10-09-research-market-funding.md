# Market candles and funding archive Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline, with TDD and one fresh final reviewer.

**Goal:** Fetch bounded, normalized closed 1m trade/mark candles and realized funding rates for prospective exit research, exposing missing data without changing trading.
**Architecture:** Reuse unsigned ArchiveClient and its Store gate/quota. A shared internal timestamp walker validates complete pages, deduplicates equal rows, rejects conflicts and stalled cursors, and filters half-open windows. Candle coverage additionally proves the minute grid; funding traversal never certifies actual account costs or the settlement schedule.
**Tech Stack:** Go1.27 standard library, existing collect package; Linux/macOS.
**Spec:** docs/superpowers/specs/2026-10-05-prospective-exit-data-design.md sections2–5.

**Execution status:** Tasks1–2 complete locally. Full Go race, collect vet, cmd
build, Linux compile and whitespace checks PASS after boundary correction.
Independent review: no remaining functional blockers; old cursor wording was
updated. No deferred minors. Global vet retains three unrelated runner_old
warnings. DB/external opt-in tests and Linux runtime not run. No push/deploy.

## Global Constraints

- Preserve existing master changes; user authorized inline continuation without per-stage questions and later combined push. No commits, push, deploy, production HTTP, DB or trading changes.
- Fixed public HTTPS GET through existing allowlist, unsigned, 1 GET/s,15s,3 retries,4MiB,10000/day,1..200 pages/pass,100 rows/page. No persistence of raw HTTP responses.
- `MarketWindow{Symbol string; From,To time.Time; MaxPages int}`: USDT swap, half-open UTC instants, millisecond-aligned, positive times, width<=72h, From>=now-80days, To<=now. Candles require minute-aligned bounds; no rounding/substitution for boundary segments. Shared pass-local now, no request beyond invalid scope. Retention limit conservative; no claimed complete remote archive guarantee.
- `ArchiveCoverage{Status,Reason string; Pages,Records int; WindowTraversed bool}`. Error or cancellation never complete. Short pages do not prove exhaustion. Initial after=To-1ms for minute-grid candles (exclude next live candle), To+1ms for arbitrary-ms funding, then oldest ts; identical overlaps allowed, unknown rows newer than cursor rejected; empty page or oldest<From proves only API-window traversal. Conflicting timestamp rows or no progress fail closed. Sort rows deterministically on success/failure. Whole page validation before acceptance; <=20000 observed rows.
- Candle source exactly `trade` or `mark`; arrays exactly9/6 strings; timestamp minute-aligned, closed confirm=1, end<=pass-now, positive valid decimal OHLC and low<=open/close<=high, nonnegative trade volumes. Preserve decimal strings and zero volume evidence; don't synthesize missing candles. Candle archive includes symbol/source/window and normalized rows, expected/missing minute counts. Complete only after traversal AND every requested minute exists. Mark OHLC is an approximation, not settlement mark.
- Funding rows exact known names (reject case aliases), SWAP/exact symbol, canonical fundingTime, valid signed `realizedRate`; do not substitute `fundingRate`. Extra public fields ignored. Preserve actual timestamps, never assume8h. Funding archive includes source/window/rates; API traversal separate from `ScheduleVerified=false`; Costs unknown if no rates, otherwise sensitivity-only, never verified or net amount.
- This slice is library-only. Boundary public trades, metadata, order attribution, bundle/CLI remain next stages; no replay-v1 conversion.

## Review Focus

1. Empty/short/stale pages must not turn candle gaps or missing funding schedules into verified data or zero costs.
2. Timestamp ties, conflicts and overlapping pages must not skip unseen data or loop indefinitely; page limits apply to malformed/duplicate responses too.
3. Boundary inclusion, unclosed candles and minute rounding must not contaminate a 72h sample with pre-entry/post-horizon prices.
4. Decimal strings, realized versus predicted rates, mark versus settlement prices and unknown account costs must remain distinguishable.
5. Cancellation, quota failures and response field aliases must fail closed without partially accepting an invalid page or leaking upstream errors.

### Task 1: Closed candle archives and bounded timestamp traversal

**Files:** Create market_archive.go, market_candles.go, market_candles_test.go in internal/research/collect.
**Interfaces:** `(*ArchiveClient).CollectCandles(ctx context.Context,w MarketWindow,source string)(CandleArchive,error)`. `Candle{At time.Time; Prices OHLC; Volume,VolumeCurrency,VolumeQuote *string}`; mark volume pointers nil. `CandleArchive{Symbol,Source string; From,To time.Time; Coverage ArchiveCoverage; ExpectedMinutes,MissingMinutes int; Candles []Candle}`. Internal generic timestamp walker and strict response decoder reused in Task2; reuse exactPrivateObject/Response validation and privateTime existing helpers without public/private API changes.

- [x] Write RED tests: exact grid/half-open bounds, trade and mark formats, confirmed/valid decimals, 72h4320 rows via<=45 pages, semantic duplicate/conflict, empty archive/gaps, short page continuation, repeated/newer cursor, malformed/oversized/aliases, invalid/future/old/subminute windows, cancellation including empty terminal response, shared quota exhaustion, page budget; deterministic normalization and zero-volume preservation. Run `go test ./internal/research/collect -run TestMarketCandles -count=1`; expect missing APIs FAIL.
- [x] Implement validation, bounded timestamp walk, normalized candle arrays and explicit coverage. No external requests, no files except existing quota/cooldown fixtures.
- [x] Run focused tests and collect race, expect PASS. Record task completion, retain changes locally.

### Task 2: Realized funding archive with honest cost status

**Files:** Create market_funding.go, market_funding_test.go; update docs/research-entry-capture.md.
**Interfaces:** `(*ArchiveClient).CollectFunding(ctx context.Context,w MarketWindow)(FundingArchive,error)`. `FundingRate{At time.Time; RealizedRate string}`; `FundingArchive{Symbol,Source string; From,To time.Time; Coverage ArchiveCoverage; ScheduleVerified bool; Costs string; Rates []FundingRate}`. Consumes Task1 timestamp walk with request path /api/v5/public/funding-rate-history, no bar query.

- [x] Write RED tests: irregular settlement times, after cursor/fenceposts, realized not forecast, zero/negative/rebate rates, missing/malformed/aliased fields, unknown currency not invented, conflicting/equal duplicate timestamps, exhaustion and budgets, cancellation, empty archive not zero verified costs, public-only/no extra fields serialized. Run `go test ./internal/research/collect -run TestMarketFunding -count=1`; expect missing API FAIL.
- [x] Implement strict funding normalizer on Task1 walker; document API traversal vs schedule/cost proof, boundaries/retention, remaining work. No account funding amount inference.
- [x] Run focused and full Go race suite, collect vet, cmd build, Linux cross-compile, diff check; strict self-review plus one fresh reviewer. Report existing unrelated global vet warnings and skipped opt-in checks. No production readiness claim.

## Sources / interpretation

OKX official documentation checked2026-10-09: https://app.okx.com/docs-v5/en (history-candles array/after direction/confirm); https://app.okx.com/docs-v5/zh/ (history-mark-price-candles and funding-rate-history). Funding schedule can change; public realized rate is not a private account bill. API docs do not promise immutable snapshot-consistent pagination. Observed traversal is not cryptographic proof that provider has no missing records.
