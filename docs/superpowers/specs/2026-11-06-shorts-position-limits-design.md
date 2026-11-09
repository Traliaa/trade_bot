# SHORT Trading and Position Limits Design

## Goal

Re-enable V3 SHORT signals and raise the configured position capacity to ten total positions, while limiting each direction to five positions.

## Configuration

Both shipped configurations (`values_local.yaml` and `values_ci.yaml`) use the same risk envelope:

- `strategy.v3.allow_shorts: true`
- `strategy.v3.max_open_positions: 10`
- `user_defaults.default_max_open_positions: 10`
- `user_defaults.default_max_long_positions: 5`
- `user_defaults.default_max_short_positions: 5`

The runner already applies the global user limit, caps V3 positions by the strategy limit, counts LONG and SHORT independently, and prevents two positions in the same instrument. No runner behavior changes are required.

## Compatibility

The `user_defaults` values apply when a user is created. Persisted user settings remain user-owned and are not silently overwritten. Existing users must receive the same 10/5/5 values through the normal settings update path if their stored limits differ.

## Verification

A configuration regression test parses every shipped YAML configuration and asserts that SHORT trading is enabled and the global and per-side limits match 10/5/5. The relevant Go packages and then the complete test suite must pass.
