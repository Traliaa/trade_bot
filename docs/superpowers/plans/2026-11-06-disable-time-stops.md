# Disable Time Stops Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Disable early and regular time-stop exits in every shipped configuration while retaining reversible runner support.

**Architecture:** Use the existing zero-disables semantics in `decideTrail15m`; change configuration only. Extend the shipped-configuration regression test so local and CI configurations cannot accidentally reactivate either timer.

**Tech Stack:** Go 1.25, YAML v2, Go testing

**Spec:** `docs/superpowers/specs/2026-11-06-disable-time-stops-design.md`

## Global Constraints

- Both time-stop bar counts are exactly zero in both shipped YAML files.
- Existing time-stop implementation and decision tests remain unchanged.
- SL, TP, partial, BE/lock, stale, runner, entry risk, and leverage remain unchanged.
- Persisted user settings are not silently overwritten.

## Review Focus

- Either shipped configuration retaining a non-zero timer must fail the regression test.
- Zero bar counts must remain valid under `validateConfig`.
- Existing explicit non-zero decision tests must continue to pass.
- Threshold values remaining non-zero must not activate a timer when its bar count is zero.
- No unrelated trailing parameters may change.

---

### Task 1: Disable both configured time stops

**Files:**
- Modify: `internal/modules/config/trading_limits_config_test.go`
- Modify: `configs/values_local.yaml`
- Modify: `configs/values_ci.yaml`

**Interfaces:**
- Consumes: `Config.DefaultTrailing.EarlyTimeStopBars`, `Config.DefaultTrailing.TimeStopBars`
- Produces: shipped configurations with both bar counts set to zero

- [ ] **Step 1: Extend the configuration test**

Rename the test to cover the complete approved trading experiment and assert both time-stop bar counts equal zero for each shipped configuration.

- [ ] **Step 2: Verify RED**

Run: `GOCACHE=/tmp/trade-bot-go-cache go test ./internal/modules/config -run TestShippedConfigsUseApprovedTradingExperiment -count=1`

Expected: FAIL because current bar counts are 3 and 8.

- [ ] **Step 3: Disable both timers in both YAML files**

Set `early_time_stop_bars` and `time_stop_bars` to `0`; do not change their thresholds or any other trailing parameter.

- [ ] **Step 4: Verify GREEN and reversibility**

Run: `GOCACHE=/tmp/trade-bot-go-cache go test ./internal/modules/config -run TestShippedConfigsUseApprovedTradingExperiment -count=1`

Run: `GOCACHE=/tmp/trade-bot-go-cache go test ./internal/modules/runner_old/sessions -run 'TestDecideTrail15m(UsesRegularTimeStop|KeepsTradeAboveRegularTimeStopThreshold)' -count=1`

Expected: PASS.

- [ ] **Step 5: Verify the repository**

Run: `GOCACHE=/tmp/trade-bot-go-cache go test ./...`

Run: `git diff --check`

Expected: PASS with no whitespace errors.
