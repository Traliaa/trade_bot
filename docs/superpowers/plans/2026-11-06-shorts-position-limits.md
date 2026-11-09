# SHORT Trading and Position Limits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable V3 SHORT entries with at most ten total, five LONG, and five SHORT positions in shipped configurations.

**Architecture:** Keep the existing runner guards unchanged and express the approved risk envelope through V3 and user-default configuration. Pin the intended values with a table-driven regression test over both shipped YAML files.

**Tech Stack:** Go 1.25, YAML v2, Go testing

**Spec:** `docs/superpowers/specs/2026-11-06-shorts-position-limits-design.md`

## Global Constraints

- The total configured position limit is exactly 10.
- The configured per-side limits are exactly 5 LONG and 5 SHORT.
- V3 SHORT signal generation is enabled.
- Existing persisted user settings are not silently overwritten.
- The one-instrument/one-position guard remains unchanged.

## Review Focus

- A shipped environment retaining `allow_shorts: false` must fail the regression test.
- A mismatch between the V3 global cap and the user global default must fail the regression test.
- Either directional default exceeding five must fail the regression test.
- Existing stored user settings must not be mutated during config load.
- Position guards must remain unchanged by this configuration-only change.

---

### Task 1: Pin and apply the approved risk envelope

**Files:**
- Create: `internal/modules/config/trading_limits_config_test.go`
- Modify: `configs/values_local.yaml`
- Modify: `configs/values_ci.yaml`

**Interfaces:**
- Consumes: `Config`, YAML tags for `strategy.v3` and `user_defaults`
- Produces: shipped configuration values `allow_shorts=true`, global limit `10`, LONG limit `5`, SHORT limit `5`

- [ ] **Step 1: Write the failing configuration test**

Add `TestShippedConfigsEnableShortsWithApprovedPositionLimits`, parsing both YAML files and asserting the exact approved values.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `GOCACHE=/tmp/trade-bot-go-cache go test ./internal/modules/config -run TestShippedConfigsEnableShortsWithApprovedPositionLimits -count=1`

Expected: FAIL because SHORT is disabled and limits are 3/3/3.

- [ ] **Step 3: Update both shipped YAML files**

Set SHORT to enabled and limits to 10/5/5 exactly as specified.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run: `GOCACHE=/tmp/trade-bot-go-cache go test ./internal/modules/config -run TestShippedConfigsEnableShortsWithApprovedPositionLimits -count=1`

Expected: PASS.
- [ ] **Step 5: Run package and full-suite verification**

Run: `GOCACHE=/tmp/trade-bot-go-cache go test ./internal/modules/config/... ./internal/models/... ./internal/modules/runner_old/service/...`

Run: `GOCACHE=/tmp/trade-bot-go-cache go test ./...`

Expected: PASS.
