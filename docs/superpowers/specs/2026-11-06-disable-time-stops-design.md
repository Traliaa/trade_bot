# Disable Time Stops Design

## Intent

Start a clean live-statistics series without time-based forced exits. In the parsed 44-trade sample, 27 `time_stop` exits produced `-7.93R`, making this the largest repeated non-SL loss source.

## Goals

- Disable both early and regular time-stop decisions for the shipped bot configuration.
- Keep stop-loss, take-profit, partial close, break-even, profit lock, stale handling, profit runner, and manual close behavior unchanged.
- Preserve the implementation and configuration fields so the experiment can be reversed without a code migration.
- Make the disabled state explicit and regression-tested in every shipped configuration.

## Non-goals

- Do not remove time-stop code, close-reason history, metrics, or research-model support.
- Do not change per-trade risk, leverage, entry filters, stale thresholds, or protective-order behavior in this change.
- Do not claim that disabling time stops makes the strategy profitable; evaluate it on a new series.

## Approaches Considered

1. Disable early stop and delay regular stop to 12 bars with a `-0.3R` threshold. Recommended initially because it retained a loss-cutting backstop.
2. **Selected:** disable both time stops by setting their bar counts to zero. This provides the cleanest measurement of their effect and uses the existing `> 0` guards without altering runner behavior.
3. Keep the 8-bar timer but lower the threshold to `-0.3R`. Rejected because two hours may still truncate trades before the signal develops.

## Configuration and Compatibility

Set `default_trailing.early_time_stop_bars: 0` and `default_trailing.time_stop_bars: 0` in `values_local.yaml` and `values_ci.yaml`. Threshold values remain documented but inactive.

These are defaults for newly created users. Existing persisted users retain their current trailing settings until updated through the normal settings path. The rollout must apply zero bar counts to the active user's stored settings before treating the experiment as started.

## Verification and Measurement

- Add a regression test that parses both shipped YAML files and requires both bar counts to be zero.
- Keep the existing decision-level tests proving time-stop behavior when a non-zero value is explicitly supplied; this preserves reversibility.
- Run the complete Go test suite.
- Treat deployment/update time as the start of a new cohort. Compare at least trade count, net R, profit factor, SL share, average holding time, and open-position occupancy against the previous series; do not mix cohorts.
