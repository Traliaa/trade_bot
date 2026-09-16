# Close Report Integrity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Stop inventing close reasons and make new trade R metrics and win rates internally consistent before comparing trailing profiles.

**Architecture:** This is the first independently releasable part of the approved September 14 strategy plan. Keep the existing trade-history JSON contract backward compatible, add classification provenance to new records, use immutable entry risk for new price metrics, and classify wins by recorded monetary P&L. Do not change signal generation, trailing decisions, orders, saved settings or historical rows.

**Tech Stack:** Go 1.27, existing models/session/repository packages, standard testing, PostgreSQL JSONB payload (no schema migration for additive fields).

**Spec:** `docs/strategy-review-2026-09-14.md`, proposed plan stages 1–2.

## Global Constraints

- No production writes, deployments, bot restarts, real orders or account-setting changes.
- Preserve the untracked September 14 audit report and unrelated frontend changes.
- Historical reasons remain historical: do not run a backfill based on guesses.
- No funding completeness claims: funding ingestion and reconciliation are a separate deliverable.
- No profitability claims from synthetic tests or the single closed post-deployment trade.
- First establish workspace preference; current checkout is `master`, not an isolated worktree.
- Use Go 1.27 explicitly; default local Go 1.26 cannot load this module.
- Run regression tests before and after each implementation task. Do not commit/push without resolving workspace/integration preference.

## Delivery boundaries

This plan covers close-reason integrity, fixed-risk reporting, and net-P&L win counting only. Remaining stage-1 work is persistent signal/rejection journaling, fills/funding reconciliation, versioned effective settings and historical completeness reporting. Stage 2 is shared trailing decision logic and a replay executor for partials/BE/time exits, followed by profile comparison on real historical data. Those are separate deliverables, not implied complete by this patch.

## Baseline

- [x] Inspect affected packages and audit evidence.
- [x] Confirm Go 1.27 is installed.
- [x] Run baseline tests: models, runner sessions, repository stats and research packages all pass.
- [x] User explicitly authorized editing `master` directly: «можно мастер менять».

Test command prefix used below:

```sh
GOCACHE=/private/tmp/trade_bot_gocache_127 /Users/akorastelev/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.darwin-arm64/bin/go
```

## Task 1: Remove invented close reasons and retain their provenance

**Files:**
- Modify: `internal/models/trade.go` (additive JSON fields).
- Modify: `internal/modules/runner_old/sessions/resolve_closed_trade.go` (classification/finalization).
- Create: `internal/modules/runner_old/sessions/close_reason_test.go`.

**Interfaces:**
- Consume `TradePayload.PendingCloseReason`, stop/target/BE levels and existing trail state.
- Keep `classifyCloseReason(tr, payload, state, exitPrice) models.CloseReason` as a compatibility wrapper if needed by callers.
- Introduce `classifyCloseEvidence(payload models.TradePayload, state *models.PositionTrailState, exitPrice float64) (models.CloseReason, string)`.
- Add payload strings `CloseIntentReason` (`close_intent_reason,omitempty`) and `CloseReasonSource` (`close_reason_source,omitempty`). Allowed source values: `bot_intent`, `price_level_inferred`, `legacy_time_flag`, `unknown`. A price match is never marked exchange-confirmed.

- [x] Write failing tests for unsupported BE/TP guesses. Example:

```go
func TestSmallLossDoesNotProveBreakEven(t *testing.T) {
    p := models.TradePayload{EntryPrice: 100, StopLoss: 90, TakeProfit: 120, RiskDist: 10, PosSide: "long"}
    got := classifyCloseReason(models.TradeRecord{}, p, nil, 99)
    if got != models.CloseReasonUnknown { t.Fatalf("got %s, want unknown", got) }
}
```

Add independent cases: exit 108 (0.8R, far from target 120) remains unknown; `TookPartial=true` alone does not prove the final remainder closed by partial exit; `MovedToBE=true` with exit far from BE does not prove BE; long/short final exits are symmetric.

- [x] Run `go test ./internal/modules/runner_old/sessions -run 'TestSmallLoss|TestCloseEvidence' -count=1`; confirm the old fallback fails the new expectations.
- [x] Implement the evidence function: explicit normalized pending reason first; invalid non-empty pending value produces unknown; use existing level matching only as `price_level_inferred`; require BE/lock flags and their actual relevant level for BE/lock inference. Do not infer a final reason from historical partial/BE/stale state alone. `TimeStopTriggered` without a specific saved reason is generic time-stop, not proof of stale exit. No terminal classification from signed R alone.

```go
reason, source := classifyCloseEvidence(p, state, execution.FinalFillPrice)
payload.CloseIntentReason = p.PendingCloseReason
payload.CloseReasonSource = source
// Preserve the existing finalization's pending-field clearing afterwards.
```

- [x] Add tests that manual/time-stop intent survives an exit near TP; `TimeStopTriggered` is generic time-stop; plain nearby stop/target remains explicitly inferred; unknown stays unknown; new fields survive JSON round trip and old payloads remain readable.
- [x] Run session/model tests and inspect the diff for changes to trading decisions (none allowed).

## Task 2: Compute report price R, MFE and MAE from entry risk

**Files:**
- Modify: `internal/models/trade_metrics.go`.
- Create: `internal/models/trade_metrics_test.go`.
- Modify: `internal/modules/runner_old/sessions/resolve_closed_trade.go`.
- Modify: `internal/modules/runner_old/sessions/sync_closed_trades.go`.

**Interfaces:**
- Add `CalcPriceR(entry, price, initialRiskDist float64, posSide string) float64` for reporting only.
- Add `TradePayload.InitialRiskDist() float64`: prefer finite positive saved `RiskDist`; otherwise derive from finite positive `PlannedRiskUSDT / (EntrySize * CtVal)` when available; finally accept a valid legacy entry/SL distance only if neither denominator is present and no moved/locked-stop flag is set. Return 0 if original risk cannot be established.
- Existing live-trailing helpers and their semantics remain untouched.

- [x] Write failing numeric tests with literal expected values:

```go
func TestPriceRUsesInitialRisk(t *testing.T) {
    p := TradePayload{EntryPrice: 100, StopLoss: 101, RiskDist: 10, PosSide: "long", MovedToBE: true}
    if got := CalcPriceR(p.EntryPrice, 105, p.InitialRiskDist(), p.PosSide); got != 0.5 {
        t.Fatalf("got %v, want 0.5R despite moved stop", got)
    }
}
```

Also cover short 100→95 with original risk 10 = +0.5R; MAE long 100→97 = −0.3R; zero/negative/NaN/Inf risk never generates non-finite output; missing original risk after a moved stop is not reconstructed from the new stop.

- [x] Run model tests to observe missing behavior.
- [x] Implement finite checks and a single signed-price-distance formula:

```go
switch posSide {
case "long": return (price-entry)/initialRiskDist
case "short": return (entry-price)/initialRiskDist
default: return 0
}
```

Apply only after validating entry, price and risk. Update open/closed report calculations of `ExitPriceR`, `MFER`, `MAER` to pass the same immutable risk. Continue assigning closed `EffectiveR = RealizedPnL / PlannedRiskUSDT` when planned risk is valid; do not replace fee-aware closed R with price R.

- [x] Test a payload with moved stop, nonzero fees and unequal price R/net R; assert both meanings remain distinct. Run model and session tests.

## Task 3: Count wins consistently using monetary P&L

**Files:**
- Modify: `internal/modules/repository/pg/trade_history.go` (`buildTradeStats`, `buildStatsBreakdown`, `performanceWindow`).
- Modify: `internal/modules/repository/pg/trade_stats_test.go`.

**Interfaces:**
- Existing API structs and field names unchanged. Wins/losses use the sign of `RealizedPnL` in totals, every grouped breakdown and rolling windows. R remains a separate metric.

- [x] Add a failing fixture where positive price R accompanies negative net P&L:

```go
trades := []models.TradeRecord{
    {Payload: models.TradePayload{PosSide: "long", RMultiple: 0.1, RealizedPnL: -0.01}},
    {Payload: models.TradePayload{PosSide: "long", RMultiple: -0.1, RealizedPnL: 0.02}},
    {Payload: models.TradePayload{PosSide: "long", RMultiple: 1, RealizedPnL: 0}},
}
stats := buildTradeStats(trades)
if stats.Wins != 1 || stats.Losses != 1 || stats.BreakevenTrades != 1 {
    t.Fatalf("net outcome counts disagree: %+v", stats)
}
```

Assert the same 1 win/1 loss and 33⅓% win rate in direction breakdown and recent window, using a numeric tolerance. Assert total R remains 1 and P&L remains 0.01 independently.

- [x] Run repository tests and observe the wrong counts on old code.
- [x] Replace outcome branching on `p.RMultiple` with `p.RealizedPnL` in all three aggregation functions; preserve R summation and all unrelated metrics.
- [x] Run repository tests, then the full test suite and targeted race tests.

## Review and handoff

- [x] Run `go test ./... -count=1` with the installed Go 1.27 toolchain (exit 0).
- [x] Run `go test -race ./internal/models ./internal/modules/runner_old/sessions ./internal/modules/repository/pg -count=1` (exit 0).
- [x] Run `git diff --check` and review for unintended settings/order mutations.
- [x] Run `go build ./cmd/...` (exit 0; binaries not executed).
- [x] Obtain code review of the completed slice and address findings.
- [x] Handoff states exactly what changed and what remains in stages 1–2; no claim of completed funding reconciliation or production-equivalent replay.
- [ ] User decision on commit/push remains pending; no push or deployment is included in this plan.

## Implementation notes (September 14)

- Implemented directly on `master` with explicit user approval. No commit, push, deployment, production DB mutation or settings change.
- Regression failures observed before fixes: guessed TP/BE and generic stale reasons, monetary outcome counts, missing original-risk helpers, invalid saved risk with valid planned risk, and ambiguous protective/target matches.
- Extracted pure close finalization and open reporting payload construction to test the production calculations. Open reporting keeps the fallback denominator local: it must not overwrite `RiskDist` consumed by live recovery. The regression failed when that mutation was temporarily reintroduced and passed after its removal.
- Independent review identified the open-risk mutation and planned-risk fallback gap; both corrected and reviewed again, with no remaining actionable findings.
- New close evidence is additive JSON only. Existing closed rows are not rewritten. Price proximity remains an inference, not exchange execution confirmation. Missing original risk yields numeric zero under the existing API contract, not proof of a breakeven outcome.
- Monetary outcomes use the recorded `RealizedPnL` sign; this does not certify funding completeness. The fee-aware net-R path is retained separately from price R.
