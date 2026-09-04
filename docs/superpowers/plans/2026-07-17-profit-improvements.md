# Profit Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring trade_bot V3 Donchian strategy to PF > 1.0 and positive total R through exit improvements, weighted scoring, volume confirmation, and config centralization.

**Architecture:** Changes span 4 layers — config (YAML + config struct), strategy scoring (service_v3.go), trailing execution (runner_old + trailing_helpers), and position management (on_candle_close). Each layer is modified independently with compilation and test gates between them.

**Tech Stack:** Go 1.25, pgx/v5, go.uber.org/fx, chi router.

## Global Constraints

- All V3 score changes must keep `minEdge = 2` and `minConfirmScore = 5`
- HTF bias thresholds (0.4-0.6) and all retest tolerances must not change
- V3 autotune path must not be modified
- Existing trailing presets (safe/mid/aggr) must continue to work after config refactor
- Every task must compile before moving to the next
- `go test ./...` must pass before final commit

---

### Task 1: Add config sections — strategy.v3 + stale YAML defaults

**Files:**
- Modify: `configs/values_local.yaml`
- Modify: `internal/modules/config/config.go`
- Modify: `internal/models/user.go`
- Test: `go build ./cmd/bot`

**Interfaces:**
- Consumes: existing `StrategyConfig` struct, `TrailingDefaultsConfig` struct, `TrailingConfig` struct, `GetStaleConfig()`
- Produces: new `V3Config` struct in config.go, `StaleConfig` in config.go, new `default_trailing.stale` YAML section

- [ ] **Step 1: Add V3Config struct + stale config to config.go**

Read `internal/modules/config/config.go`. Add `V3Config` nested struct inside `StrategyConfig`. Add `StaleConfig` struct. Add `Stale` field to `TrailingDefaultsConfig`. Fix ATR tag from `mapstructure` to `yaml`.

```go
// Inside StrategyConfig struct, add:
V3 V3Config `yaml:"v3"`

// New structs (add after StrategyConfig):
type V3Config struct {
    MinConfirmScore         int     `yaml:"min_confirm_score"`
    RetestTolerancePct      float64 `yaml:"retest_tolerance_pct"`
    ImpulseBodyMinPct       float64 `yaml:"impulse_body_min_pct"`
    CompressionThresholdPct float64 `yaml:"compression_threshold_pct"`
    StrongCloseMin          float64 `yaml:"strong_close_min"`
    StrongCloseMax          float64 `yaml:"strong_close_max"`
    VolumeMinRatio          float64 `yaml:"volume_min_ratio"`
    MinRR                   float64 `yaml:"min_rr"`
    SLBufferPct             float64 `yaml:"sl_buffer_pct"`
    SwingLookbackBars       int     `yaml:"swing_lookback_bars"`
    TargetLookbackBars      int     `yaml:"target_lookback_bars"`
    UsePercentFallback      bool    `yaml:"use_percent_fallback"`
    FallbackStopPct         float64 `yaml:"fallback_stop_pct"`
    ATRPeriod               int     `yaml:"atr_period"`
    ATRStopMult             float64 `yaml:"atr_stop_mult"`
    UseATRGuard             bool    `yaml:"use_atr_guard"`
}

type StaleConfig struct {
    AfterBars     int     `yaml:"after_bars"`
    MinMFER       float64 `yaml:"min_mfe_r"`
    ExitProfitR   float64 `yaml:"exit_profit_r"`
    NearBER       float64 `yaml:"near_be_r"`
    MaxAdverseR   float64 `yaml:"max_adverse_r"`
    GraceBars     int     `yaml:"grace_bars"`
    WorseByR      float64 `yaml:"worse_by_r"`
    TightenToBER  float64 `yaml:"tighten_to_be_r"`
}
```

Update `TrailingDefaultsConfig` to include stale:
```go
type TrailingDefaultsConfig struct {
    // ... existing fields unchanged ...
    Stale StaleConfig `yaml:"stale"`
}
```

Fix the ATR fields in existing `StrategyConfig` — change `mapstructure` to `yaml`:
```go
ATRPeriod   int     `yaml:"atr_period"`
ATRStopMult float64 `yaml:"atr_stop_mult"`
UseATRGuard bool    `yaml:"use_atr_guard"`
```

- [ ] **Step 2: Add V3 defaults in ApplyV3Defaults for V3Config**

Read `config.go:293-360`. Move V3 defaults to work with `V3Config`:

```go
func (c *StrategyConfig) ApplyV3Defaults() {
    v := &c.V3
    if v.MinConfirmScore <= 0 { v.MinConfirmScore = 5 }
    if v.RetestTolerancePct <= 0 { v.RetestTolerancePct = 0.0015 }
    if v.ImpulseBodyMinPct <= 0 { v.ImpulseBodyMinPct = 0.003 }
    if v.CompressionThresholdPct <= 0 { v.CompressionThresholdPct = 0.012 }
    if v.StrongCloseMin <= 0 { v.StrongCloseMin = 0.70 }
    if v.StrongCloseMax <= 0 { v.StrongCloseMax = 0.30 }
    if v.VolumeMinRatio <= 0 { v.VolumeMinRatio = 0.7 }
    if v.MinRR <= 0 { v.MinRR = 1.5 }
    // remove the old individual field defaults that are now in V3Config
    // keep fields not moved: AddonMinScore, MaxAdds, AddonCooldownBars, etc.
}
```

- [ ] **Step 3: Update `configs/values_local.yaml`**

```yaml
strategy:
  ltf: "15m"
  htf: "1h"
  donchian_period: 20
  # ... existing fields unchanged ...
  v3:
    min_confirm_score: 5
    retest_tolerance_pct: 0.0015
    impulse_body_min_pct: 0.003
    compression_threshold_pct: 0.012
    strong_close_min: 0.70
    strong_close_max: 0.30
    volume_min_ratio: 0.7
    min_rr: 1.5
    sl_buffer_pct: 0.001
    swing_lookback_bars: 5
    target_lookback_bars: 20
    atr_period: 14
    atr_stop_mult: 0.8

default_trailing:
  be_trigger_r: 0.45
  be_offset_r: 0.05
  lock_trigger_r: 0.8
  lock_offset_r: 0.25
  early_time_stop_bars: 3
  early_time_stop_min_mfe_r: 0.15
  time_stop_bars: 8
  time_stop_min_current_r: 0.1
  partial_enabled: true
  partial_trigger_r: 0.7
  partial_close_frac: 0.5
  stale:
    after_bars: 16
    min_mfe_r: 0.35
    exit_profit_r: 0.25
    near_be_r: -0.03
    max_adverse_r: -0.65
    grace_bars: 6
    worse_by_r: 0.30
    tighten_to_be_r: 0.05

user_defaults:
  default_leverage: 15
  default_max_open_positions: 6
  default_max_long_positions: 3
  default_max_short_positions: 3
  default_position_pct: 1.0
  default_risk_pct: 0.4
  default_stop_pct: 2.0
  default_take_profit_rr: 1.5
```

- [ ] **Step 4: Copy stale fields in `NewTradingSettingsFromDefaults`**

Read `internal/models/user.go`, find `NewTradingSettingsFromDefaults`. After the existing TrailingConfig field assignments, add:

```go
func NewTradingSettingsFromDefaults(d *config.TrailingDefaultsConfig) TrailingConfig {
    // ... existing code ...
    // After the existing field assignments, add:
    tc.StaleAfterBars = d.Stale.AfterBars
    tc.StaleMinMFER = d.Stale.MinMFER
    tc.StaleExitProfitR = d.Stale.ExitProfitR
    tc.StaleNearBER = d.Stale.NearBER
    tc.StaleMaxAdverseR = d.Stale.MaxAdverseR
    tc.StaleGraceBars = d.Stale.GraceBars
    tc.StaleWorseByR = d.Stale.WorseByR
    tc.StaleTightenToBER = d.Stale.TightenToBER
    // ... rest of function unchanged ...
}
```

- [ ] **Step 5: Build and verify**

```bash
go build ./cmd/bot
```

Expected: success, no compilation errors.

- [ ] **Step 6: Commit**

```bash
git add configs/values_local.yaml internal/modules/config/config.go internal/models/user.go
git commit -m "config: add V3Config, stale section in YAML, trailing defaults update

- Add V3Config struct and StaleConfig struct to config.go
- Copy stale fields in NewTradingSettingsFromDefaults
- Fix ATR mapstructure→yaml tags
- Update YAML: new strategy.v3, default_trailing.stale, new trailing defaults
  (BE 0.45, lock 0.8, partial 0.7, time_stop 8, risk 0.4, stop 2%, TP 1.5R)"
```

---

### Task 2: Update effectiveV3Params to use V3Config, remove effectiveV3TuningLocked

**Files:**
- Modify: `internal/modules/strategy/service/service_v3.go`

**Interfaces:**
- Consumes: `V3Config` from config.go, `effectiveV3Params()` callers (`onCandleV3ReadyLocked`, `AutoTuneV3Now`, `buildMarketContext`)
- Produces: simplified `effectiveV3Params()` that reads from `cfg.Strategy.V3` + tune override, `AutoTuneV3Now` uses before/after from `effectiveV3Params()`

- [ ] **Step 1: Simplify `effectiveV3Params()`**

Replace the current implementation (lines 683-724) which reads both cfg fields and tune fields:

```go
func (e *Service) effectiveV3Params() (
    minConfirm int,
    retestTol float64,
    impulseMin float64,
    compression float64,
    closeUpMin float64,
    closeDnMax float64,
) {
    e.tuneMu.RLock()
    defer e.tuneMu.RUnlock()

    cfg := e.cfg.Strategy.V3
    e.cfg.Strategy.ApplyV3Defaults() // ensure V3 defaults are set

    minConfirm = cfg.MinConfirmScore
    retestTol = cfg.RetestTolerancePct
    impulseMin = cfg.ImpulseBodyMinPct
    compression = cfg.CompressionThresholdPct
    closeUpMin = cfg.StrongCloseMin
    closeDnMax = cfg.StrongCloseMax

    if e.tune.V3MinConfirmScore > 0 {
        minConfirm = e.tune.V3MinConfirmScore
    }
    if e.tune.V3RetestTolerancePct > 0 {
        retestTol = e.tune.V3RetestTolerancePct
    }
    if e.tune.V3ImpulseBodyMinPct > 0 {
        impulseMin = e.tune.V3ImpulseBodyMinPct
    }
    if e.tune.V3CompressionThresholdPct > 0 {
        compression = e.tune.V3CompressionThresholdPct
    }
    if e.tune.V3StrongCloseMin > 0 {
        closeUpMin = e.tune.V3StrongCloseMin
    }
    if e.tune.V3StrongCloseMax > 0 {
        closeDnMax = e.tune.V3StrongCloseMax
    }

    return
}
```

- [ ] **Step 2: Remove `effectiveV3TuningLocked()`**

Delete the entire method (currently lines 656-682). Update `AutoTuneV3Now` which was the only caller — it currently calls `effectiveV3TuningLocked()` for `before`/`after`. Replace with:
```go
// In AutoTuneV3Now, instead of:
// before := e.effectiveV3TuningLocked()
// Use:
before := e.effectiveV3Params() // returns 6 values, not RuntimeTuning struct
```

But `AutoTuneV3Now` needs `before`/`after` as `RuntimeTuning` struct for logging. Either:
(a) Convert `AutoTuneV3Now` to use the 6 return values directly, or
(b) Create a small helper to build `RuntimeTuning` from params.

Option (a) is cleaner. The log.Printf at the end already prints individual fields by name, not the struct. Replace `before.V3MinConfirmScore` etc with `beforeMinConfirm` etc.

- [ ] **Step 3: Update `buildMarketContext` to read compression from V3Config**

Currently `buildMarketContext` calls `e.effectiveV3Params()` just for compression. Keep it unchanged.

- [ ] **Step 4: Build**

```bash
go build ./cmd/bot
```

- [ ] **Step 5: Commit**

```bash
git add internal/modules/strategy/service/service_v3.go
git commit -m "refactor: remove effectiveV3TuningLocked, use V3Config in effectiveV3Params"
```

---

### Task 3: Add weighted scoring + volume confirmation + RejectLowVolume

**Files:**
- Modify: `internal/modules/strategy/service/service_v3.go` — `buildLongScore`, `buildShortScore`
- Modify: `internal/modules/strategy/service/service_v3_helpers.go` — add weighted helpers
- Modify: `internal/models/reject_reason.go` — add `RejectLowVolume`
- Test: `go build ./cmd/bot`

**Interfaces:**
- Consumes: `effectiveV3Params()`, `candleBody()`, `closePosInRange()`, `distancePct()`
- Produces: weighted score (0-2 per check), volume penalty (-1), `RejectLowVolume` rejection

- [ ] **Step 1: Add `RejectLowVolume` to reject_reason.go**

```go
// In the v3 section (around line 50), add:
RejectLowVolume RejectReason = "low_volume"
```

- [ ] **Step 2: Add weighted helper functions + update isV3RejectReason to include RejectLowVolume in service_v3_helpers.go**

```go
func isV3RejectReason(r models.RejectReason) bool {
    switch r {
    case models.RejectConfirmScoreLow,
        models.RejectRetestNotConfirmed,
        models.RejectImpulseWeak,
        models.RejectCompressedRange,
        models.RejectVolatilityTooLow,
        models.RejectWeakCloseUp,
        models.RejectWeakCloseDown,
        models.RejectHTFConflict,
        models.RejectReclaimFailed,
        models.RejectStructureNotConfirmed,
        models.RejectOverextendedUp,
        models.RejectOverextendedDown,
        models.RejectLowVolume:  // ADD THIS
        return true
    default:
        return false
    }
}

// weight helpers:

```go
// weightedRetest returns 0, 1, or 2 based on retest quality.
// 0 = no retest; 1 = wick-touch only; 2 = close/body overlap or close near.
func weightedRetest(retestLevel float64, last models.CandleTick, retestTol float64) int {
    if retestLevel <= 0 {
        return 0
    }
    wickTouch := distancePct(last.Low, retestLevel) <= retestTol
    closeNear := distancePct(last.Close, retestLevel) <= retestTol*0.6
    bodyCross := last.Open <= retestLevel && last.Close >= retestLevel*(1-retestTol)
    if closeNear || bodyCross {
        return 2
    }
    if wickTouch {
        return 1
    }
    return 0
}

// weightedClose returns 0, 1, or 2 based on close position in range.
func weightedClose(closePos, strongThreshold float64) int {
    if closePos >= strongThreshold+0.15 {
        return 2
    }
    if closePos >= strongThreshold {
        return 1
    }
    return 0
}

// weightedImpulse returns 0, 1, or 2 based on body size relative to price.
func weightedImpulse(bodyPct, impulseMin float64) int {
    if bodyPct >= impulseMin*2 {
        return 2
    }
    if bodyPct >= impulseMin {
        return 1
    }
    return 0
}

// weightedCloseShort returns 0, 1, or 2 based on close position for short signals.
func weightedCloseShort(closePos, closeDnMax float64) int {
    if closePos <= closeDnMax-0.15 {
        return 2
    }
    if closePos <= closeDnMax {
        return 1
    }
    return 0
}

// computeSMAVolume computes simple moving average of Volume over last N candles.
func computeSMAVolume(candles []models.CandleTick, n int) float64 {
    if len(candles) == 0 || n <= 0 {
        return 0
    }
    if n > len(candles) {
        n = len(candles)
    }
    start := len(candles) - n
    var sum float64
    for i := start; i < len(candles); i++ {
        sum += candles[i].Volume
    }
    return sum / float64(n)
}
```

- [ ] **Step 3: Update `buildLongScore` with weighted scoring + volume**

Replace `buildLongScore` (currently `service_v3.go:102-193`):

```go
func (e *Service) buildLongScore(
    ltf []models.CandleTick,
    mctx models.MarketContext,
    retestLevel float64,
) models.SignalScore {
    _, retestTol, impulseMin, _, closeUpMin, _ := e.effectiveV3Params()

    s := models.SignalScore{
        Reasons: make([]models.RejectReason, 0, 8),
    }

    if len(ltf) < 3 {
        s.Reasons = append(s.Reasons, models.RejectNotEnoughLTFBars)
        return s
    }

    last := ltf[len(ltf)-1]
    prev := ltf[len(ltf)-2]

    bodyPct := 0.0
    if last.Close > 0 {
        bodyPct = candleBody(last) / last.Close
    }

    closePos := closePosInRange(last)

    // Weighted checks
    retestScore := weightedRetest(retestLevel, last, retestTol)
    closeScore := weightedClose(closePos, closeUpMin)
    reclaimOK := retestLevel > 0 && last.Close >= retestLevel*(1-retestTol)
    impulseScore := weightedImpulse(bodyPct, impulseMin)
    structureOK := last.Low >= prev.Low || last.Close > prev.High

    // Volume check
    volumeRatio := e.cfg.Strategy.V3.VolumeMinRatio
    if volumeRatio <= 0 {
        volumeRatio = 0.7
    }
    avgVol := computeSMAVolume(ltf, 20)
    if avgVol > 0 && last.Volume < avgVol*volumeRatio * 0.5 {
        s.Reasons = append(s.Reasons, models.RejectLowVolume)
        s.SetupOK = false
        s.Score = 0
        s.ContextOK = false
        return s
    }

    contextOK := true
    if mctx.Bias == models.MarketBiasBear {
        contextOK = false
        s.Reasons = append(s.Reasons, models.RejectHTFConflict)
    }
    if mctx.Compressed {
        contextOK = false
        s.Reasons = append(s.Reasons, models.RejectCompressedRange)
    }
    if mctx.OverextendedUp {
        contextOK = false
        s.Reasons = append(s.Reasons, models.RejectOverextendedUp)
    }

    score := retestScore + closeScore + boolScore(reclaimOK) + impulseScore + boolScore(structureOK)

    s.SetupOK = retestScore > 0
    s.ContextOK = contextOK
    s.RetestOK = retestScore > 0
    s.StrongClose = closeScore > 0
    s.ReclaimOK = reclaimOK
    s.ImpulseOK = impulseScore > 0
    s.StructureOK = structureOK

    s.Score = score

    if retestScore == 0 {
        s.Reasons = append(s.Reasons, models.RejectRetestNotConfirmed)
    }
    if closeScore == 0 {
        s.Reasons = append(s.Reasons, models.RejectWeakCloseUp)
    }
    if !reclaimOK {
        s.Reasons = append(s.Reasons, models.RejectReclaimFailed)
    }
    if impulseScore == 0 {
        s.Reasons = append(s.Reasons, models.RejectImpulseWeak)
    }
    if !structureOK {
        s.Reasons = append(s.Reasons, models.RejectStructureNotConfirmed)
    }
    if !mctx.VolatilityOK {
        s.Reasons = append(s.Reasons, models.RejectVolatilityTooLow)
    }

    return s
}
```

- [ ] **Step 4: Update `buildShortScore` symmetrically**

Same changes as Step 3 but for short side:
- Use `weightedRetest(retestLevel, last, retestTol)` with `last.High` logic (already in helper)
- Use `weightedCloseShort(closePos, closeDnMax)` for short close position
- Use `weightedImpulse` same way
- Use same volume check
- Score = retestScore + closeScore + boolScore(reclaimOK) + impulseScore + boolScore(structureOK) [-1 if low volume]

- [ ] **Step 5: Build**

```bash
go build ./cmd/bot
```

- [ ] **Step 6: Commit**

```bash
git add internal/models/reject_reason.go internal/modules/strategy/service/service_v3.go internal/modules/strategy/service/service_v3_helpers.go
git commit -m "feat: weighted scoring and volume confirmation for V3 entries

- Add RejectLowVolume reject reason
- Add weightedRetest (0-2), weightedClose (0-2), weightedImpulse (0-2)
- Add computeSMAVolume helper
- Update buildLongScore/buildShortScore with weighted scoring (max 8pts)
- Add volume confirmation: volume < 70% SMA(20) -> -1 score; < 35% -> reject
- Keep minEdge=2, minConfirm=5 unchanged"
```

---

### Task 4: Fix V3 partial close — connect StrategyState to runner_old

**Files:**
- Modify: `internal/modules/runner_old/service/on_candle_close.go` — add V3 partial check
- Modify: `internal/modules/strategy/service/service_v3.go` — ensure `manageOpenPositionV3` sets PartialDone (already does, just verify)
- Test: `go build ./cmd/bot`

**Interfaces:**
- Consumes: `strategy.Service.getStrategyStateLocked()`, `strategy.Service.stateV3` map
- Produces: partial close execution on OKX when V3 marks `PartialDone`

- [ ] **Step 1: Read on_candle_close.go to find the right hook point**

Read `runner_old/service/on_candle_close.go` to find where candle events arrive and open positions are processed. Look for where `trailOne` is called or where per-position checks happen.

- [ ] **Step 2: Add V3 state check in on_candle_close**

```go
// In on_candle_close.go, in the candle processing loop where positions are iterated:
// After processing trailing state, add:
if st.PartialDone && ts, ok := runner.getTrailState(instID, posSide); ok && !ts.TookPartial {
    // V3 marked partial done but runner hasn't executed it yet
    closeSz := ts.Size * float64(cfg.DefaultTrailing.PartialCloseFrac)
    if closeSz > 0 && closeSz < ts.Size {
        _, err := runner.Okx.CloseMarket(ctx, instID, posSide, closeSz)
        if err != nil {
            runner.Logger.Warn("v3 partial close failed", zap.Error(err))
        } else {
            runner.TrailMu.Lock()
            ts.Size -= closeSz
            ts.TookPartial = true
            ts.MovedToBE = true
            ts.SL = calcBEPrice(ts, ts.User.Settings)
            runner.TrailMu.Unlock()
            runner.Logger.Info("v3 partial executed", zap.String("instId", instID), zap.Float64("size", closeSz))
        }
    }
}
```

- [ ] **Step 3: Build**

```bash
go build ./cmd/bot
```

- [ ] **Step 4: Commit**

```bash
git add internal/modules/runner_old/service/on_candle_close.go
git commit -m "fix: connect V3 partial close to runner execution

- Add check in on_candle_close for V3 StrategyState.PartialDone
- Execute partial close via OKX client when V3 marks partial but runner hasn't
- Set TookPartial, MovedToBE, and update trailing stop"
```

---

### Task 5: Remove hardcoded BEOffsetR fallback in calcBEPrice

**Files:**
- Modify: `internal/modules/runner_old/sessions/trailing_helpers.go` — remove `offsetR = 0.10` fallback
- Test: `go test -count=1 ./internal/modules/runner_old/sessions/...`

- [ ] **Step 1: Read trailing_helpers.go and fix calcBEPrice**

Find `calcBEPrice` (around line 10). Remove the fallback:

```go
func calcBEPrice(st *models.PositionTrailState, cfg models.Settings) float64 {
    if st == nil || st.Entry <= 0 || st.RiskDist <= 0 {
        return 0
    }

    offsetR := cfg.TrailingConfig.BEOffsetR
    if offsetR <= 0 {
        return 0 // no offset configured -> don't move SL, caller must handle
    }

    if st.PosSide == "long" {
        return st.Entry + offsetR*st.RiskDist
    }
    return st.Entry - offsetR*st.RiskDist
}
```

- [ ] **Step 2: Check callers of calcBEPrice**

Read `trailing_helpers.go` and `trail_one.go`. The callers (`trailOne` at partial close, `decideTrail15m` at partial and BE) already check `improves(cand)` and `shouldImproveSL(st, cand)` before acting. If `calcBEPrice` returns 0, these callers will skip — which is correct (no BE if no offset configured). The stale path `calcStaleBEPrice` uses `sc.TightenToBER` which is a different path.

- [ ] **Step 3: Run tests**

```bash
go test -count=1 ./internal/modules/runner_old/sessions/...
```

Expected: all tests pass (the `trailing_helpers_test.go` covers this function).

- [ ] **Step 4: Commit**

```bash
git add internal/modules/runner_old/sessions/trailing_helpers.go
git commit -m "fix: remove hardcoded 0.10R offset fallback in calcBEPrice

- When BEOffsetR <= 0, return 0 instead of silently defaulting to 0.10R
- Callers already handle zero return via shouldImproveSL check"
```

---

### Task 6: Final verification

**Files:**
- Test: all tests + build

- [ ] **Step 1: Run all tests**

```bash
go test -count=1 -race ./...
```

- [ ] **Step 2: Build**

```bash
go build ./cmd/bot
```

- [ ] **Step 3: Final commit with all changes**

```bash
git add -A
git commit -m "feat: profit improvements for V3 Donchian strategy

- Exits: BE 0.45R, lock 0.8R, partial 0.7R, time_stop 8 bars, SL 2%, risk 0.4%
- Entries: weighted scoring (0-2 per check, max 8pts, volume confirmation 0.7 SMA)
- Config: V3Config section, stale section in YAML, ATR tag fix
- Fix: V3 partial close connected to runner execution
- Fix: remove hardcoded BEOffsetR fallback"
```
