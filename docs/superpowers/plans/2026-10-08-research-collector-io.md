# Research collector I/O implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add tested private immutable storage and a bounded public archive HTTP transport, without launching collection.
**Architecture:** New I/O primitives beside the pure core, never imported by the trading runner. Store owns its directory lock and persistent UTC daily request journal; client consumes the store budget. No generic URL, method or auth-header input.
**Tech Stack:** Go 1.27 standard library, existing x/sys/unix, Linux/macOS.
**Spec:** `docs/superpowers/specs/2026-10-05-prospective-exit-data-design.md`, sections 2–5.

## Global Constraints

- No production requests, credentials, DB writes, capture activation, trade changes, push or deploy. Preserve the earlier local batch on master, as requested.
- Existing explicitly supplied absolute private directory, 0700; files 0600. No git checkout, root or symlink path. Operator must ensure it is a dedicated non-web volume; code cannot discover every web server's document root.
- One writer (OS lock), immutable content-addressed JSON envelopes, bounded checksum validation on reopen, atomic no-clobber publication. Never delete or repair unknown/corrupt/stale files automatically.
- 1 GiB maximum including old and interrupted writes; lower explicit budgets allowed. Store is a byte persistence primitive, not a privacy scrubber: callers must pass whitelisted core/bundle output, never private HTTP responses.
- One HTTP request at a time, at least 1s between starts, 15s per attempt, at most 3 retries, 4 MiB response. Fixed HTTPS OKX host, GET-only public archive paths, no redirects/proxy/cookies, fixed-code errors.
- Persistent request reservations before every attempt, at most 10000/UTC day including retries/restarts. Corrupt quota journal fails closed. Future private auth/DB adapters, pagination/provenance, bundle manifest and CLI remain separate integration work.

## Review Focus

1. Symlinks, hardlinks, non-private permissions and a second writer must not bypass storage isolation.
2. Crash leftovers and a corrupt existing object must count toward capacity and must not be overwritten.
3. Daily quota must survive reopen, count retries and reject a malformed partial journal rather than resetting.
4. Redirects, foreign hosts/path traversal, secret-bearing upstream errors and oversized bodies must not escape HTTP boundaries.
5. Cancellation while queued/backing off and Retry-After beyond the wait cap must stop cleanly without an early retry.

### Task 1: Private immutable store and quota

**Files:** `internal/research/collect/store.go`, `store_test.go` (Linux/macOS).
**Interfaces:** `OpenStore(dir string, maxBytes int64) (*Store,error)`, `Put(payload []byte) (string,error)`, `Get(ref string) ([]byte,error)`, `ReserveRequest(at time.Time) error`, `Close() error`. Reference is SHA256 hex, no caller filenames. maxBytes<=1GiB; record payload<=4MiB. Reservations persist exact sequential records per UTC date, maximum10000. Fixed errors only; own lock file remains after close.

- [x] Write tests: immutable/idempotent roundtrip; reopen/checksum; reject traversal/unsafe path/permissions/link; second writer; concurrent puts; stale partial counts toward budget; no overwrite on corruption; quota reopen/limit/malformed journal. Expect RED for missing API with `go test ./internal/research/collect -run TestStore -count=1`.
- [x] Implement root-relative bounded file access, OS lock, temp sync then no-clobber hard-link publication and unlink temp (atomic rename-equivalent without overwrite), directory sync; refuse corrupt evidence, count leftovers, fail closed on I/O.
- [x] Same tests GREEN and race package. Document supported-filesystem assumptions and error recovery.

### Task 2: Public archive HTTP transport

**Files:** `internal/research/collect/archive_http.go`, `archive_http_test.go`; update `docs/research-entry-capture.md`.
**Interfaces:** `NewArchiveClient(store *Store) *ArchiveClient`, `Get(ctx context.Context, path string, query url.Values) ([]byte,error)`. Fixed public history-candles/history-trades/history-mark-price-candles/funding-rate-history paths. Never accept caller headers/credentials/URL. No private payload persistence by client. Private fields permit package-local fake transport/clock tests only, not exported configuration bypasses.

- [x] Write tests for GET target/encoded query, blocked path/redirect, bounded body, redacted transport/status errors, four attempts maximum, every attempt charged, limiter/cancellation and Retry-After >60s returns retry_later. Expect RED with `go test ./internal/research/collect -run TestArchive -count=1`.
- [x] Implement dedicated hardened transport/client, serialized cancellable queue, persistent budget reservations, per-attempt context timeout, backoff2/4/8s and Retry-After seconds/date with maximum60s (longer means defer, never retry early). Retry only network/transient5xx/429, not malformed responses/auth/4xx. Network requests are not evidence of pagination completeness.
- [x] Tests GREEN; full Go race suite, vet and cmd build; strict self-review and one fresh reviewer. Document remaining integration gates and no production collection.

## Rulings

- User explicitly requested no intermediate approval questions and a combined later push: continue inline from the existing architectural spec without new approval handoffs/commits. Keep this plan and prior changes locally.
- Split the remaining subsystem into this I/O slice and subsequent DB/private/archive/CLI integration. No production access is required to test these primitives. Cost: real collection still cannot launch from this stage alone.
