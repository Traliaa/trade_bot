# Research collector completion implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the locally runnable, read-only prospective collector, without deploying or changing live trading.

**Architecture:** Reuse existing Store, snapshot validation and bounded public/private archives. Add metadata observations, explicit DB order attribution, a versioned content-addressed bundle graph, one-shot orchestration and a safe CLI. Missing external evidence remains an explicit coverage limitation, not a fabricated verified result.

**Tech Stack:** Go 1.27, pgx, standard library; Linux/macOS.

**Spec:** `docs/superpowers/specs/2026-10-05-prospective-exit-data-design.md`

## Global Constraints

- Existing master checkout by explicit user preference; preserve unrelated dirty changes. No production calls/writes, push, deploy, trade changes or automatic schedules.
- Protocol inclusion exactly 28 days; market horizon 72h regardless of actual closure. Initial protocol registration cannot backdate the start. Fixed limits: 200 pages/component, 10000 requests/day, 1 GET/s, 15s timeout, three retries, max 1 GiB.
- DB account-scoped SELECT-only views; all entry/order rows from one short read-only repeatable-read transaction, before exchange HTTP. No settings/key discovery.
- OKX Read-only credentials and DB DSN only through explicit inherited pipes. No raw private identifiers/responses/errors in exported files or logs.
- Capture, market, reconciliation and costs independent. No automatic replay v1 conversion, no authoritative funding from public rates, no inferred exit attribution by instrument/time.
- One-shot CLI only; repeated invocations reuse the private immutable store, quotas and manifest. No daemon or live startup in this task.

## Review Focus

- Cross-trade/reused order IDs must not attribute unrelated fills or certify net.
- Closed DB state plus incomplete/lagged fill windows must not imply a complete archive or zero funding.
- Restart with different manifest, corrupt references or older incomplete pass must not silently change protocol or lose prior coverage.
- Interrupted/quota-limited components must preserve independent partial results without publishing a replay-ready dataset.
- CLI parse/DB/connect/private errors must not echo secrets, paths containing secrets, private IDs or raw upstream text.

### Task 1: Metadata and immutable protocol registration

**Files:** `market_metadata.go`, `manifest.go`, respective tests; update `archive_http.go` narrow allowlist.
**Interfaces:** `CollectMetadata(ctx,symbol) (MetadataObservation,error)`; `Protocol` with Format, ID, Start, End, HorizonHours, MaxPages, MaxBytes; `RegisterProtocol(store, protocol, now) (ref,error)`, `LoadProtocol(store,ref)`, `Store.Refs()`.

- [x] Tests first: valid decimal whitelist; wrong type/currency/symbol, missing decimals, aliases; safe empty/missing metadata. Protocol fixed 28d/72h, no first-registration backdating, same protocol resume, reject changed protocol in same store; checksum and bounded decoding.
- [x] Run focused tests RED, implement, run focused GREEN.

### Task 2: Same-snapshot order links and conservative attribution

**Files:** `entries_pg.go`, `attribution.go`, tests and opt-in PG integration fixture.
**Interfaces:** preserve `ReadEntries`; add `ReadStudyEntries` using `research_collect.order_links_v1(row_id,order_id,role)`, bounded 10000 links. EntryRecord gains private exit links and link-completeness. `attributeEntry(entry, FillArchive, FillWindow) LedgerReport` projects only exact linked orders through Reconcile.

- [x] Tests first: same transaction; duplicate/conflicting/reused links, truncation; entry-only and linked exits, foreign fills excluded, missing links/volume/record coverage stay pending, no funding certification. All DB/key IDs remain memory-only.
- [x] Run RED, implement, run GREEN and PG opt-in integration if Docker is available.

### Task 3: Versioned bundle graph and one-pass orchestration

**Files:** `bundle.go`, `run.go`, tests.
**Interfaces:** `RunPass(ctx,store,protocolRef,batch,public,private,now) (PassReport,error)`; `ReadBundle(store,ref)` verifies graph/checksums/schema; immutable manifests contain capture and typed evidence refs, independent statuses, prior pass reference/time and coverage. Boundary trades chunked to bound records below Store max size. Only validated producer snapshot and typed normalized outputs persist.

- [x] Tests first: one synthetic complete 72h sample through fake HTTP/real Store; partial horizon, source failures, missing snapshots, no private client, rerun/reopen, corrupt refs, deterministic content for same inputs, no private IDs, no legacy replay format. DB truncated flag remains visible. Compare prior metadata observations without claiming complete history.
- [x] Run RED, implement, run GREEN. Use exact PlanSegments; collect only closed full minutes and ended boundary segments; future segments pending, no bars before entry.

### Task 4: Safe standalone CLI and final integration

**Files:** `cmd/research-collect/main.go`, CLI tests, `db_credentials.go`, docs.
**Interfaces:** subcommands `init`, `run`, `inspect`; explicit store/manifest reference, protocol/start on init, inherited `db-fd` required for run and optional `okx-fd`. No DSN/key flags/env discovery, no trade bootstrap imports. Standard signal cancellation; sanitized JSON summaries only; nonzero exit for incomplete pass/error.

- [x] Tests first: help/invalid flags redact, missing FDs, bounded pipe credentials, init cannot backdate, inspect graph offline, safe failure on bad store/manifest. Smoke compiled CLI against synthetic fixtures only.
- [x] Run RED, implement, run GREEN. Write setup/view SQL as operator documentation, never execute against production. Document account-view/key checks, one-shot usage, rollback and coverage caveats.
- [x] Full race suite, build, collector/CLI vet, Linux compile, complete diff self-review, one fresh independent final reviewer. Fix important findings with RED→GREEN then rerun suite. Keep changes local for combined release as requested.

Completion evidence 2026-10-09: final full race with dedicated PG17 PASS (22 tested packages; collector 119.410s); explicit restricted-snapshot integration PASS, not skipped; build all packages, targeted vet, Linux compile and compiled native CLI init/inspect smoke PASS. Global vet has three existing runner_old warnings. Final independent review's two Important history/resume defects and self-review's inherited-FD defect fixed with RED→GREEN; no deferred minor findings. No production/API calls, push or deploy. Existing dirty frontend/Telegram work preserved. Operational limitations and activation requirements are documented in docs/research-entry-capture.md.
