# Entry exclusions and protective-order safety

## Deployment

Apply migration `0007_instrument_entry_blocks.sql` before starting this version.
The Docker entrypoint already runs Goose before the bot. If migration fails,
do not bypass it: an unavailable exclusion store deliberately blocks new entries.
Existing position exits and protection do not consult the exclusion table.

Only explicit OKX order rejection `sCode=51155` persists an account/instrument
entry block. Timeouts, insufficient funds, leverage and size errors do not.
Blocks survive restarts, have no automatic expiry, and produce a first-observation
notification rather than a message for every skipped signal. If persistence fails,
the session retains the block in memory and retries saving before another entry.
A process crash before persistence remains a durability limitation.

Review exclusions read-only:

```sql
SELECT user_id, inst_id, code, blocked_at
FROM public.instrument_entry_blocks
ORDER BY blocked_at DESC;
```

Re-enabling an instrument requires a separately authorized administrative action
after verifying that the restriction has been lifted for that account. Do not use
a live order as a probe. No UI/API for unblocking is introduced here.

## Protection changes

- Query live conditional and OCO orders per instrument; failures mean unknown
  protection, not a silent successful check. A potentially truncated result is
  also an error. Guard warnings no longer permanently disable themselves.
- Save replacement TP/SL IDs and the current SL separately from original SL/risk.
- Check fresh exchange positions before entry, refusing to merge a new bot trade
  with an existing position on the same instrument, including the opposite side.
  This is a preflight, not an atomic lock against simultaneous manual trading.
- Normalize partial exits to lot/minimum size in both regular and V3 paths,
  retaining a valid remainder. An impossible or rejected regular partial does not
  prevent an otherwise eligible SL improvement.

Entry-safety changes do not attribute merged fills to individual owners or
restore an old TP that market price already passed. The October 1 runner below
can reconcile protective IDs for an eligible tracked residual; it refuses
externally increased positions. Pre-runner partial-fill timeout behavior is
unchanged.

## Profit runner

### September 27: existing LOCK-stage correction

`LockedProfit` is a reporting flag: it can already be true after a positive
break-even move. It no longer prevents evaluating the configured LOCK target.
The decision compares the target with the current SL and still requires the
existing minimum improvement, so it neither repeats an achieved target nor
loosens a more protective stop. Restored positions use the same rule without
rewriting their original entry, initial stop, risk distance or partial state.

The web app displays a valid `current_stop_loss` for open trades, falling back
to the original `stop_loss` for legacy records. Closed history retains the
original stop. This shows the bot's persisted level, not a fresh verification
that the exchange still has that protective order.

These fixes do not adopt an untracked/merged exchange position or repair AVAX
orders remotely. They are separate from the future runner behavior below.

### October 1: current-price 1R runner

On a closed 1-minute candle at or beyond +3 initial R, tracked positions enter
the runner path. The activation is rechecked against a fresh OKX last-price
ticker, using the original stored entry/risk, not a changed exchange average.
LONG follows `last - R`; SHORT follows `last + R`. Round away from the market
to tick size and retain any more protective stored or live SL. A reached stop
is not loosened or replaced with a market close: reconciliation warns instead.
Runner updates are throttled to once per minute, not the legacy 15m slot.

Each update reads the actual open size and live conditional/OCO IDs. Increased
positions, unknown protection, unsupported trigger semantics or invalid data
fail closed. A smaller remainder suppresses further automatic partials. The
runner intent is persisted before placing a replacement; its live ID/price/size
must be confirmed and persisted before cancelling old protection, including
the fixed TP. A failed cancellation is retried on a subsequent reconciliation.
The guard accepts a missing TP only for runner positions and still requires SL.

Partial settings remain in effect. Unexecuted partials require a valid lot-size
remainder. Runner partial intent is persisted before submission; ambiguous
submission/fill outcomes never blindly resubmit, including after restart.
An unresolved intent requires inspection if no smaller exchange size appears.
Partial fills are stored when available, and a fresh position size drives SL
resizing, never the requested order quantity. Runner updates, regular/V3 exits,
in-app manual submissions and reporting writes are serialized per session.
An active manual-close request (`pending`, `accepted`, `unknown`) is checked
before reading the runner's position snapshot. It suppresses automatic partials
for that entire cycle even if the manual order fills during SL reconciliation;
failure to read this guard also suppresses partials, but not stop management.

This change does not alter initial TP placement or pre-3R exit rules: positions
whose existing TP executes below 3R will not reach this mode. Regular/V3 partial
submission recovery before 3R is unchanged. There is no cross-process lock;
run only one bot instance per account. External exchange actions can race REST
snapshots; reduce-only closing semantics prevent a new reverse position but do
not make those snapshots atomic. Unsupported external position increases are
not adopted. Existing AVAX is managed only if it is a tracked trade with valid
original risk, after fresh exchange verification; no live orders are changed by
tests or by editing this repository. Profit is never guaranteed.

Runner fields are backward-compatible JSON payload fields, so no new migration
is needed beyond migration 7. Deploying this version enables this behavior for
eligible existing tracked positions as well as future ones. Do not roll back an
active runner to an older binary that ignores its mode without reviewing its
live protection first.

## Tests

Unit tests cover typed error classification, account isolation, persistence retry,
pending-order queries, recovery and rounding. The opt-in integration tests use
`TRADE_REPORT_TEST_DSN` and reject any host/database except `127.0.0.1` /
`trade_report_test`. Use a disposable database and run the repository and session
integration tests sequentially. The repository test recreates its exclusion table.
The session test uses real SQL and a fully intercepted HTTP transport, never OKX.
`TestProfitRunnerSessionPostgres` additionally covers minute updates, partial
resizing, persistent intent, restored state and replacement failures. The helper
and intercepted-client tests cover LONG/SHORT calculations, rounding, no
loosening, real-ID reconciliation, confirmation failure, persistence failure,
hedge closing orders and cancellation failure.
