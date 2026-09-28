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

This does not automatically repair pre-existing merged positions, attribute their
fills to individual owners, or restore an old TP that market price already passed.
Such positions require a separately reviewed management decision. The existing
partial-fill timeout reconciliation behavior is unchanged.

## Agreed follow-up: profit runner (not implemented here)

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

The user wants a 3R profit reference for 1R initial risk, retaining partial profit
taking and trailing the remainder by SL rather than closing all of it at a fixed
TP. SL must not loosen. TP movement, trigger levels, trail distance and exchange
order-race handling need an explicit implementation design and tests before
enabling this mode. This patch does not change live strategy settings or promise
that all trades close profitably.

## Tests

Unit tests cover typed error classification, account isolation, persistence retry,
pending-order queries, recovery and rounding. The opt-in integration tests use
`TRADE_REPORT_TEST_DSN` and reject any host/database except `127.0.0.1` /
`trade_report_test`. Use a disposable database and run the repository and session
integration tests sequentially. The repository test recreates its exclusion table.
The session test uses real SQL and a fully intercepted HTTP transport, never OKX.
