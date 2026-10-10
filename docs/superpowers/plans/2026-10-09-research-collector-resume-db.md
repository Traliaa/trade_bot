# Collector restart safety and DB snapshot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Persist request cooldowns across collector restarts and read a bounded, consistent batch from a restricted PostgreSQL view without starting collection.
**Architecture:** Extend the existing private Store with an append-only cooldown journal and fail-closed HTTP integration. Add a separate pgx read-only adapter for a fixed account-scoped view; no trading runner imports, credentials, or whole mutable payloads.
**Tech Stack:** Go 1.27, existing pgx/v5 and Store; Linux/macOS store.
**Spec:** `docs/superpowers/specs/2026-10-05-prospective-exit-data-design.md`, sections 2–5.

## Global Constraints

- Preserve all prior local changes on master (user authorized working in place); no commit/push/deploy/production access or changes to trading.
- Store remains owner-only, bounded to 1 GiB and 100000 files. No deletion of history or automatic corruption repair. One GET/s, 15s attempt, 3 retries, 10000 requests/UTC day.
- Persist cooldown before releasing the HTTP gate, including cancellation and last retry. A storage failure blocks subsequent requests in that process. Durable response-receipt crash marker fails closed across restart.
- DB: fixed `research_collect.entries_v1` view, account scope enforced by independently provisioned restricted role/view; no account selector, `user_settings`, DSN handling, SQL identifier input, or networking while a DB transaction is open beyond the DB itself.
- Read-only repeatable-read, 5s context and statement timeout, 1s lock timeout, maximum1000 rows per 28-day window; overflow means explicit incomplete batch, never silent success. Raw rows/private identifiers stay in memory and refuse JSON/format logging.
- Invalid/missing snapshots remain counted, not invented. No ledger matches or replay coverage inferred from DB alone.

## Review Focus

1. Restart, interrupted cooldown writes, and disk exhaustion must not allow a request before a received server deadline.
2. A transient request interrupted before its response is recorded must leave evidence requiring manual investigation, not silently reset cooldown.
3. Malformed/large DB values must be bounded before transfer, have redacted errors, and not leak private row/order IDs.
4. Cancellation, scan/query/commit failure and repeat invocations must release rows and the transaction without returning a partial successful batch.
5. Same-timestamp rows and more than the row budget must be deterministic and explicitly truncated, not asserted as complete or skipped using invented cursors.

### Task 1: Durable HTTP cooldown and in-flight recovery

**Files:** `internal/research/collect/cooldown.go`, `cooldown_test.go`, `store.go`, `archive_http.go`, `archive_http_test.go`.
**Interfaces:** private Store methods `beginAttempt(at time.Time) error`, `finishAttempt(until time.Time) error`; append-only `.requests` journal with records containing kind and UTC deadline, strict bounded parsing. A begin without finish (including an empty created journal) fails closed on reopen. No private request identity persisted. Persistent deadline never decreases. Existing daily reservations remain authoritative.

- [x] Add behavioral tests for server Retry-After surviving reopen, minimum inter-request spacing after success/reopen, interrupted/empty/corrupt journal refusal, disk/file budget refusal and same-process storage failure block. Run `go test ./internal/research/collect -run 'TestCooldown|TestArchiveDurable' -count=1`; expected RED (missing methods/early requests).
- [x] Implement append+fsync+directory sync under Store mutex; recover deadline on OpenStore. Begin before network and finish immediately after response/error, prior to cancellation checks or gate release. A storage failure poisons the Store for further HTTP use. Record conservative one-second minimum; Retry-After can extend it, never shorten.
- [x] Run focused tests and package race tests; expected PASS. Document crash-marker/manual recovery and compatibility: old stores without journal allowed, previous binaries cannot open new journals.

### Task 2: Bounded PostgreSQL entry snapshot adapter

**Files:** `internal/research/collect/entries_pg.go`, `entries_pg_test.go`, `entries_pg_integration_test.go`; `docs/research-entry-capture.md`.
**Interfaces:** `ReadEntries(ctx context.Context, pool *pgxpool.Pool, window EntryWindow) (EntryBatch,error)`; window UTC From/To, Protocol string, Limit int. Fixed view columns: `row_id uuid, inst_id text, status text, entry_at timestamptz, open_order_id text, research_entry_snapshot jsonb`. Private rows retain row/order IDs for future in-memory attribution; only `EntryCoverage` may be serialized. No private DB cursor on disk.

- [x] Write tests exercising emitted transaction options/SQL parameters, 5s deadline, rows close before commit, rollback on errors/cancel, nil pool, invalid windows, missing/invalid snapshots, row/snapshot mismatch, duplicate captures, limit+1 overflow and redaction. Run `go test ./internal/research/collect -run TestEntries -count=1`; expected RED (missing adapter).
- [x] Implement fixed parameterized SELECT using bounded CASE expressions (32KiB snapshot/128byte IDs/40byte symbol/16byte state), limit+1 and stable entry_at,row_id order, short read-only repeatable-read transaction, fixed-code errors, validation via DecodeSnapshot. Record invalid rows as coverage issues; never persist raw rows. Scope is inclusion window, not exchange EntryAt evidence. Reject submicrosecond window bounds (pgx would truncate and could silently omit a boundary row); real-PG regression RED→GREEN.
- [x] Add opt-in integration against isolated loopback PostgreSQL (database `trade_report_test`, no production credentials). Fixture view excludes a second synthetic account, JSONB canonical decode, read-only enforced and repeatable-read snapshot behaviour. If Docker unavailable report test as not run, not passed.
- [x] Run full Go race suite, vet and build, Linux compile, strict self-review and one fresh reviewer. Expected PASS except explicitly unavailable opt-in integrations. Document provisioning contract and remaining private/archive/bundle/CLI gates.

## Rulings

- Continue inline without new approval handoffs, honoring the user's earlier request. Keep work uncommitted for the combined later push; preserve per-plan ledger until then.
- This slice reads immutable entry evidence only; exit-order attribution and private OKX reconciliation remain separate. It cannot launch research or certify profitability by itself.
