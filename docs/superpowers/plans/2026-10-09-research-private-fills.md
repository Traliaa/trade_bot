# Read-only OKX fills archive Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add credential-safe read-only account validation and bounded private fill pagination, without production access or automatically asserting ledger completeness.
**Architecture:** Reuse existing serialized, durable-budget HTTP loop through a private request helper; public API stays unsigned. A separate PrivateClient accepts explicitly supplied in-memory credentials loaded from an inherited pipe, signs only fixed config/fills-history GETs, verifies Read-only permissions and expected account before each archive pass. Private fills/IDs never go to Store.
**Tech Stack:** Go1.27 standard crypto/HTTP, existing collect Store/core; Linux/macOS.
**Spec:** docs/superpowers/specs/2026-10-05-prospective-exit-data-design.md, sections2–5.

**Execution status, 2026-10-09:** Tasks 1–2 implemented locally. RED→GREEN,
collector race, full Go race, collector vet, cmd build, Linux compile and
whitespace checks passed. Fresh review found JSON case-alias ambiguity;
regression tests reproduced it and exact-field validation fixed it. Self-review
fixed cancellation concurrent with the empty terminal page. Global vet still
reports three pre-existing runner_old warnings. DB/external opt-in integration
tests were not enabled in this slice. No commit, push, production collection or
rollout. Task checklists below preserve the original implementation brief.

## Global Constraints

- No real credentials, private production calls, code importing trading runner, orders, settings, push/deploy. Preserve uncommitted batch on master per prior user authorization; continue inline without intermediate handoffs.
- Credentials only from explicit inherited pipe FD: JSON<=4096B; read timeout<=5s plus context cancellation. Required api_key, secret_key, passphrase, expected_uid; unknown/duplicate fields rejected; no env/file-path/DB discovery. Private structs refuse JSON/fmt. Caller owns FD; Go cannot guarantee erasure of all runtime copies.
- Fixed HTTPS www.okx.com, only GET account/config and trade/fills-history. Fresh HMAC-SHA256/Base64 for each attempt, full encoded query signed. No credentials on public API. No redirects/proxy/cookies. Existing15s/4MiB/1GETs/3retries/durable10000/day apply to both clients together.
- Config must contain exactly one record with perm exactly read_only and matching expected_uid. Any error denies archive access; repeat validation on every pass, never cache indefinitely. Real role/key/account provisioning remains separately authorized.
- Fills scope is a millisecond-aligned half-open record-ts window, <=31days wide, >=now-80days (conservative inside documented last3months), To<=now; USDT swaps only. This is not an assertion about a position's complete execution-time history or funding.
- 1..200 pages/pass, 100 rows/page, <=20000 observed fills. Private bill cursor stays only in memory. Restart refetches the bounded window; no private checkpoint on disk. API begin/end expanded by1ms where needed; exact bounds enforced locally. Exhaustion requires empty page, never short-page assumption.

## Review Focus

1. Cancellation/timeout during credential pipe ingestion must not leak goroutines or log secret bytes.
2. Retries/redirects/concurrent public-private callers must not leak signed headers or bypass shared cooldown/quota.
3. Changed permissions/wrong account must prevent any fills request; credentials and raw errors must not become persisted JSON or printable logs.
4. Overlapping/repeated pages, conflicting bill IDs and saturated tied timestamps must never be marked complete or loop indefinitely.
5. ts versus fillTime, inclusive boundary ambiguity and archive retention must remain explicit; pagination completion cannot certify whole-position reconciliation/costs.

### Task 1: Private credentials and signed GET boundary

**Files:** credentials.go/credentials_test.go, private_http.go/private_http_test.go; archive_http.go.
**Interfaces:** `ReadPrivateCredentials(ctx context.Context, file *os.File) (*PrivateCredentials,error)` only deadline-capable named pipe; `NewPrivateClient(store *Store, creds *PrivateCredentials) (*PrivateClient,error)`; private `verifyAccount(ctx) error`, `getFillPage(ctx,q) ([]byte,error)`. Shared private `ArchiveClient.get(ctx,target,authorize)` and attempt authorization hook; existing public `Get` validates same allowlist and passes nil hook.

- [x] RED tests: credential validation/duplicate/oversize/blocked pipe cancellation/timeout/redaction; exact known signature with encoded query; renewed signature per retry, shared quota/public unsigned, denied permissions/wrong account, private redirect refusal and upstream-error redaction. Run `go test ./internal/research/collect -run 'TestPrivate|TestCredentials' -count=1`, expected missing APIs/behaviour FAIL.
- [x] Implement strict bounded pipe input and private credential types; extract shared existing HTTP loop without altering public contract; fixed private GETs and account verification. No public raw signed-GET API.
- [x] GREEN focused tests plus collect race; document sources and credential/provisioning boundaries. Keep local, no commit.

### Task 2: Bounded fills normalization and pagination

**Files:** private_fills.go/private_fills_test.go; docs/research-entry-capture.md.
**Interfaces:** `FillWindow{Symbol string; From,To time.Time; MaxPages int}`, `(*PrivateClient).CollectFills(ctx,window) (FillArchive,error)`; `FillArchive` private evidence, only `FillCoverage` serializable with status/reason/pages/count/record_window_complete. Private normalized records hold Fill plus RecordedAt and bill ID; execution uses fillTime not ts. API fields whitelist; extra fields discarded, required strings validated before use.

- [x] RED tests: multiple pages including 100 same-timestamp fills and overlaps; exact bounds with ts!=fillTime; empty initial/terminal page; duplicate conflict/wrong symbol/malformed timestamp/decimal/fee; no-progress/repeated cursor; page200 exhausted never complete; expired/future/unrepresentable scope; context/quota failure retains explicit incomplete status without raw errors; private evidence JSON/fmt/files leak checks. Expected FAIL for missing CollectFills.
- [x] Implement per-pass verifyAccount, GET-only query construction, bill-ID cursor progress with numeric comparison, dedup semantic equality and conflict rejection. Reject out-of-request data or unsupported records; retain last3months conservatism; only empty terminal page means observed record-window complete. Never set ledger ArchiveComplete or assign exit orders automatically.
- [x] GREEN tests, full Go race suite, vet/build/Linux compile, self-review and one independent reviewer. Local-only; no claim of production keys verified. Document remaining market/funding/bundle/CLI work and rollout checks.

## Sources and rulings

- OKX official docs checked 2026-10-09: https://www.okx.com/docs-v5/en/#trading-account-rest-api-get-account-configuration ; https://www.okx.com/docs-v5/en/#order-book-trading-trade-get-transaction-details-last-3-months ; https://www.okx.com/docs-v5/#overview-rest-authentication . They specify perm, signing, billId pagination and begin/end on ts, distinct fillTime.
- Preserve the requested staged implementation and later combined push. This slice cannot launch collection; cost of narrowing: remaining market/bundle/CLI integration is still required.
- Do not guess mapping between DB account and exchange account. Require expected_uid in the private descriptor and check config each pass; operator must provision the correct pair, otherwise no production collection.
