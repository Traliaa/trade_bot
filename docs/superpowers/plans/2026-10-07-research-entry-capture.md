# Research Entry Capture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Сохранять неизменяемый снимок фактически использованных параметров нового входа без изменения торговли.

**Architecture:** Локальные whitelist-наблюдения сопровождают существующие результаты расчёта и открытия; builder создаёт ограниченный JSON-снимок перед существующим INSERT сделки. Отдельная nullable JSONB-колонка не входит в mutable payload или публичный API. Внешний collector — отдельный этап, не зависимость бота.

**Tech Stack:** Go по `go.mod` (1.27.0), PostgreSQL, pgx/v5, существующий sqlc generator, стандартные encoding/json, crypto/sha256, runtime/debug; без новых production-зависимостей.

**Spec:** [2026-10-05-prospective-exit-data-design.md](../specs/2026-10-05-prospective-exit-data-design.md), раздел 1 и относящиеся к capture проверки раздела 5.

## Global Constraints

- `RESEARCH_CAPTURE_ENABLED=false` по умолчанию; snapshot schema=1, максимум 32 KiB.
- Не менять сигналы, размеры, лимиты, плечо, исходные SL/TP, сопровождение, universe, уведомления и расписание.
- Никаких новых GET, очередей, workers или отдельной DB-транзакции в live entry.
- Не менять порядок существующих чтений SettingsSnapshot и арифметику float64.
- Raw metadata хранить рядом с используемыми float; отсутствующие строки не реконструировать из float.
- Ошибка snapshot даёт NULL и агрегированный счётчик; ошибка существующего INSERT остаётся ошибкой INSERT.
- Не сохранять Settings целиком, секреты, account/Telegram/order/private trade IDs, DSN, raw HTTP errors.
- Старые строки остаются NULL; старый бинарник совместим после additive-миграции. Откат приложения не удаляет snapshot.
- Без push, deploy, production-миграции, live-включения capture, ордеров и выдачи прав.
- Этот план не чинит ARB и не реализует collector, bundle/replay v2 или профили A/B/C.

## Review Focus

1. `OpenPositionWithTpSl` меняет params после fill: исходные planned данные должны остаться исходными — Task 2, `TestCapturePreservesPlannedBeforeFillMutation`.
2. Настройки читаются в нескольких местах: нельзя выдать смешанные версии за одну — Task 2, `TestCaptureSettingsChangeIsIncomplete`.
3. API-раздача TradeRecord и ошибочный JSON могут раскрыть лишние данные — Task 1, `TestCaptureWhitelistAndNullOnFailure`, Task 3, `TestResearchSnapshotNotInTradeJSON`.
4. Старый INSERT и новые Update/Close не должны стирать snapshot — Task 3, `TestResearchSnapshotPostgresCompatibility`.
5. Дробные/неполные fills и fallback времени нельзя объявлять доказанным биржевым входом — Task 2, `TestCaptureFillEvidence`, включая synthetic 0.1+1.1+0.6 и multi-time.

---

## Порядок и границы

Пользователь ранее выбрал native execution: реализация главным агентом и один
независимый reviewer в конце. После review этого плана сохраняем этот выбор.
Разрешение на master уже дано; перед реализацией снова проверить dirty state.

Сводка 7 октября и P0 ARB вынесены в
[решения дня](../../strategy-decisions-2026-10-07.md). Capture полезен независимо
от исправления ARB, но его нельзя представлять как исправление production.
Live-включение исследования требует сначала устранить либо явно изолировать
несогласованные записи учёта и согласовать операционные условия.

## Карта файлов

| Файлы | Ответственность |
| --- | --- |
| `internal/models/research_capture.go`, `_test.go` (новые) | Типизированный whitelist, проекции настроек, checksum, builder |
| `internal/models/okx.go`, `internal/models/session.go` | Неэкспортируемые в JSON указатели на локальные наблюдения в результатах |
| `internal/modules/config/research_capture.go`, `_test.go` (новые), `config.go` | Opt-in env-настройки и безопасный отказ только capture |
| `internal/modules/okx_client/service/get_instrument_meta.go`, новый `research_capture_test.go` | Raw metadata и время получения существующего ответа |
| `internal/modules/runner_old/sessions/{v3.go,calc_trade_params.go,calc_size_by_risk_with_meta.go,user_session.go}`, новый `research_capture_test.go` | Наблюдения в существующих точках чтения/изменения params |
| `internal/modules/runner_old/service/research_capture.go`, `_test.go` (новые), `service.go` | Сбор snapshot, build identity, агрегированные счётчики, один INSERT |
| `internal/models/trade.go` | ResearchEntrySnapshot с `json:"-"`, отдельно от TradePayload |
| `migrations/0008_add_research_entry_snapshot.sql` (новый) | Additive nullable JSONB |
| `.sqlc.base.yaml`, `internal/modules/repository/pg/user_settings/sql/query.sql` и generated `query.sql_sqlc.go` | Nullable параметр только INSERT, локальная генерация |
| `internal/modules/repository/pg/user_settings/user_settings.go`, новый `internal/modules/repository/pg/research_capture_integration_test.go` | Маппинг записи и интеграционные проверки |
| `docs/research-entry-capture.md` (новый) | Контракт, ограничения, локальные тесты, ручной rollout checklist |

## Task 1: Whitelist-контракт и fail-open builder

**Files:** create `internal/models/research_capture.go`, `internal/models/research_capture_test.go`.

**Interfaces:**
- `ResearchBuildIdentity { Revision string; Dirty bool; Unknown bool }`.
- `ResearchRules`: собственная копия всех числовых/bool-полей `TrailingConfig` с явными snake_case JSON-ключами, без embedding/omitempty; raw и effective отдельные значения одного типа.
- `ResearchSettingsObservation { Stage string; ObservedAt time.Time; RelevantHash string; Raw ResearchRules; Effective ResearchRules; RiskPct float64; EffectiveRiskPct float64; Leverage int }`.
- `ResearchMetadataObservation`: raw строки TickSz/LotSz/MinSz/CtVal/CtMult, использованные float TickSz/LotSz/MinSz/EffectiveCtVal, Kind/SettleCcy/CtValCcy, ReceivedAt/ExchangeAt; неизвестное время = null, не now.
- `ResearchTradeValues { Entry, Size, SL, TP, RiskDist float64 }`; `ResearchEntryEvidence { Status, TimeSource string; FirstFillAt, LastFillAt *time.Time; FilledSize float64; FillCount int; Reasons []string }`.
- `ResearchEntryObservation { Planned ResearchTradeValues; Metadata ResearchMetadataObservation; Settings []ResearchSettingsObservation }`.
- `ResearchCaptureInput { CaptureID, ProtocolID, Symbol, Side, Timeframe string; Build ResearchBuildIdentity; EntryAt time.Time; Observation ResearchEntryObservation; Actual ResearchTradeValues; Evidence ResearchEntryEvidence }`.
- `ProjectResearchSettings(stage string, at time.Time, cfg Settings, effectiveRiskPct float64) ResearchSettingsObservation`.
- `BuildResearchEntrySnapshot(in ResearchCaptureInput) (json.RawMessage, string)`; second result = bounded error code, empty on success. Result contains schema=1, status, reasons and SHA-256 checksum; no raw error text.

- [ ] **Step 1: Write tests.** `TestCaptureWhitelistAndNullOnFailure`: secret/ID sentinel values in Settings never appear in projection or JSON; false/zero keys are present; invalid float, invalid capture UUID, empty protocol, encoded length >32768 yield nil plus a fixed error code. `TestCaptureCanonicalChecksum`: fixed input marshals identically; checksum excludes its own field; changing SL changes checksum. `TestCaptureDefaults`: raw stale zero remains zero; effective stale equals existing `GetStaleConfig` (16/.35/.25/-.03/-.65/6/.30/.05).

  Core assertions for a valid fixed input and projected zero/default fixture:
  ```go
  raw, code := BuildResearchEntrySnapshot(in)
  if code != "" || len(raw) > 32768 { t.Fatal("invalid capture") }
  if bytes.Contains(raw, []byte("SECRET_SENTINEL")) { t.Fatal("secret leaked") }
  if obs.Raw.StaleAfterBars != 0 || obs.Effective.StaleAfterBars != 16 { t.Fatal("defaults lost") }
  if !bytes.Contains(raw, []byte(`"partial_enabled":false`)) { t.Fatal("false omitted") }
  ```
- [ ] **Step 2: Red.** `go test ./internal/models -run 'TestCapture' -count=1`; expect missing builder/types, not environment failure.
- [ ] **Step 3: Implement contract and builder.** Explicit struct projection only; no reflection over Settings or generic JSON map copying. Canonical payload uses stable struct field order and sorted/deduplicated reason codes, UTC timestamps. Compute checksum before adding checksum field, then enforce final 32768-byte bound. Missing source evidence gives `incomplete` with reason, not invented values. Hash only relevant safe settings fields.
- [ ] **Step 4: Green.** Run same command plus `go test -race ./internal/models`; all named tests pass.
- [ ] **Step 5: Commit.** `feat: define bounded research entry snapshot contract`; stage only Task 1 files.

## Task 2: Наблюдение без изменения расчёта и защиты

**Files:** modify `internal/models/okx.go`, `internal/models/session.go`, config and entry files from map; create their `research_capture_test.go` tests and config `research_capture.go`/test.

**Interfaces:**
- `config.ResearchCaptureConfig { Enabled bool; ProtocolID string }`, member `Config.ResearchCapture`.
- `config.ParseResearchCaptureConfig(lookup func(string) (string, bool)) (ResearchCaptureConfig, string)`; reads only `RESEARCH_CAPTURE_ENABLED` and `RESEARCH_PROTOCOL_ID`. Default off. Invalid enabled value or enabled with absent/unsafe protocol disables capture and returns bounded error code; does not stop trading process. Protocol: 1..64 ASCII letters/digits/underscore/hyphen; identifier only, not an arbitrary description.
- `Instrument.ResearchMetadata *ResearchMetadataObservation`, `SizeCalcResult.ResearchSettings *ResearchSettingsObservation`, `TradeParams.ResearchEntry *ResearchEntryObservation`, `OpenResult.ResearchEvidence *ResearchEntryEvidence`; all `json:"-"`.
- Consume Task 1 projection/types. Existing public trading method signatures remain unchanged.

- [ ] **Step 1: Write regression tests.** `TestResearchCaptureConfig`: off by default, explicit true+valid protocol on, invalid values disable only capture. `TestCapturePreservesPlannedBeforeFillMutation`: params planned 100/2/95/115/5, actual fills at 101 -> planned unchanged, actual uses existing result. `TestCaptureSettingsChangeIsIncomplete`: safely project settings at calc/sizing/open reads; differing relevant values produce reason `settings_changed_during_entry`, identical values do not. `TestCaptureFillEvidence`: distinguish timeout/local fallback, empty fills, size mismatch, one valid fill, multiple timestamps. Synthetic split 0.1+1.1+0.6 produces float64 1.8000000000000003, explicitly not claimed to be ARB evidence; preserve observed volume, do not silently clamp. WaitOrderFills completion is source-reported, not independent decimal reconciliation; uncertainty gets `fill_volume_unverified`.

  Core assertions after the fake fill execution with capture enabled:
  ```go
  if params.ResearchEntry.Planned.Entry != 100 || params.Entry != 101 { t.Fatal("planned overwritten") }
  if params.ResearchEntry.Planned.Size != 2 { t.Fatal("planned size lost") }
  if !reflect.DeepEqual(callsOn, callsOff) { t.Fatal("capture changed trading calls") }
  ```
- [ ] **Step 2: Red.** `go test ./internal/modules/config ./internal/modules/okx_client/service ./internal/modules/runner_old/sessions -run 'Test(ResearchCapture|Capture)' -count=1`; expect missing observations/config.
- [ ] **Step 3: Implement metadata observation.** Preserve raw fields and response-received time in the existing GetInstrumentMeta result; no new requests or parsing changes to trade math. Preserve absence of exchange timestamp and ctMult raw string even when old math defaults multiplier to 1. Metadata provenance alone can be added to existing return value; it must not cause secret-bearing logs or independent I/O.
- [ ] **Step 4: Implement opt-in copies at existing reads.** In V3 and legacy calc, snapshot projected cfg at the already existing read; carry sizing's actual cfg separately in SizeCalcResult. Copy planned values before OpenPositionWithTpSl can mutate params. Observe its existing cfg read as another stage without moving it. Obtain fill completeness and fallback source at the current fillErr/VWAP branch; no extra WaitOrderFills/SettingsSnapshot. New snapshots do not reuse post-open `tradeConfigSnapshot` as effective entry truth.
- [ ] **Step 5: Green and call-trace comparison.** `TestCaptureOnOffTradeParity` runs enabled/disabled with fake OKX/repo/notifier, asserts equal params excluding new observations and identical ordered calls/arguments for PlaceMarket, WaitOrderFills, SL, TP, emergency-close failure path. `TestCaptureNoAdditionalSettingsRead` pins observations to those existing reads with settings updates between stages; no post-open refresh. Run command from Step 2, then existing protection/profit-runner tests and `go test -race ./internal/modules/runner_old/...`.
- [ ] **Step 6: Commit.** `feat: observe actual entry inputs without changing execution`; stage only Task 2 files.

## Task 3: Неизменяемая колонка и безопасный INSERT

**Files:** migration, model TradeRecord, sqlc config/query/generated files, user_settings repository mapping and new integration test from map. No Update/Close mutation of snapshot.

**Interfaces:**
- `TradeRecord.ResearchEntrySnapshot json.RawMessage` with `json:"-"`.
- SQL column `research_entry_snapshot jsonb NULL` without default/backfill/index.
- CreateTradeHistory nullable SQL parameter; column-specific sqlc override maps to nullable `*string`, not a global JSONB mapping change. Repository nil maps to SQL NULL, not JSON null or empty string.
- Normal Get/List projections may remain unchanged: collector will have its own scoped SELECT. UpdatePayload/CloseTradeHistory must never assign new column.

- [ ] **Step 1: Write tests.** `TestResearchSnapshotNotInTradeJSON` asserts serialized TradeRecord/API contains neither snapshot nor sentinel fields. `TestResearchSnapshotPostgresCompatibility` uses isolated loopback DB only: old INSERT without column succeeds and yields NULL; new INSERT roundtrips JSONB semantically; UpdatePayload and Close leave original column unchanged; nil stores SQL NULL; existing rows untouched by migration. Use fixed safe snapshot, never live credentials.

  Assertions use direct SELECT of the research column before/after existing Update/Close:
  ```go
  if !reflect.DeepEqual(beforeJSON, afterJSON) { t.Fatal("entry evidence mutated") }
  if !oldInsertColumnIsSQLNull { t.Fatal("old insert no longer compatible") }
  if bytes.Contains(apiJSON, []byte("research_entry_snapshot")) { t.Fatal("research data exposed") }
  ```
- [ ] **Step 2: Red.** Run `go test ./internal/modules/repository/pg ./internal/models -run 'TestResearchSnapshot' -count=1`. Integration skip is not a pass; test requires `TRADE_REPORT_TEST_DSN` pointing only to 127.0.0.1 database `trade_report_test`, following existing tests' hard guard.
- [ ] **Step 3: Implement additive schema and binding.** Up adds nullable JSONB only; do not run Down during application rollback. Mark Down as destructive/manual-only in migration comments and runbook. Modify explicit INSERT and mapping; regenerate via `make generate-sql`, inspect generated diff. The generator writes/removes root sqlc.yaml: first verify that no user-owned sqlc.yaml exists; if it does, stop and preserve it. No production schema invocation. Preserve existing CreateTradeHistory transaction and error semantics.
- [ ] **Step 4: Green.** Repeat guarded local integration test, including comparison with pre-migration SQL INSERT text. Run `git diff --check`; no accidental changes in other SQLC mappings/generated files.
- [ ] **Step 5: Commit.** `feat: persist immutable research entry snapshots`; stage only Task 3 files.

## Task 4: Подключение builder, измерение и инструкция включения

**Files:** runner/service `research_capture.go`, `_test.go`, `service.go`; `docs/research-entry-capture.md`; benchmark in repository integration test.

**Interfaces:**
- `prepareResearchEntrySnapshot(cfg config.ResearchCaptureConfig, tr models.TradeRecord, params *models.TradeParams, opened *models.OpenResult, build models.ResearchBuildIdentity, newCaptureID func() (string, error)) (json.RawMessage, string)` in runner service. Passed TradeRecord is used only for safe symbol/side/time fields; never marshalled wholesale.
- `researchBuildIdentity() models.ResearchBuildIdentity` uses runtime/debug VCS revision/modified; missing identity explicit Unknown=true.
- Use existing `Service.countDecision(reason string)` / `ExecutionStats() map[string]uint64` from `diagnostics.go`: fixed keys `research_capture_complete`, `research_capture_incomplete`, `research_capture_failed`, `research_capture_disabled`; failure subkeys only from builder's fixed error-code whitelist. No labels with capture IDs/symbol/account, no new counters subsystem/public endpoint or per-entry Telegram messages.

- [ ] **Step 1: Write tests.** `TestPrepareResearchCaptureDisabled`: nil result, no UUID generation. `TestPrepareResearchCaptureFailureKeepsTradeInsert`: failed UUID, nonfinite field, oversized result each still reaches exactly one existing CreateTradeHistory with nil snapshot and unchanged payload; existing DB failure still propagates. `TestCaptureBuildUnknown`: missing build remains unknown, not a made-up revision. `TestCaptureOnOffServiceParity`: successful/failed capture changes no signals, exchange calls, protection, notifications, or old ConfigSnapshot.

  Assertions for a forced capture failure, using the existing service test fakes:
  ```go
  if insertCalls != 1 || inserted.ResearchEntrySnapshot != nil { t.Fatal("capture blocked trade insert") }
  if !reflect.DeepEqual(inserted.Payload, expectedPayload) { t.Fatal("capture changed accounting") }
  if stats["research_capture_failed"] != 1 { t.Fatal("failure not counted") }
  ```
- [ ] **Step 2: Red.** `go test ./internal/modules/runner_old/service -run 'Test(PrepareResearchCapture|Capture)' -count=1`; expect undefined integration helper/counters.
- [ ] **Step 3: Implement final assembly before existing CreateTradeHistory.** Generate unrelated random capture UUID; copy actual result after existing fill/protection flow. Assign separate TradeRecord field. Capture's error is counted and not returned as a trading error. Count incomplete separately. Do not delay SL placement to compute JSON or build identity; cache build identity outside hot entry path.
- [ ] **Step 4: Verify and measure.** Run all named tests, `go test -race ./internal/models ./internal/modules/config ./internal/modules/runner_old/... ./internal/modules/okx_client/service/... ./internal/modules/repository/pg/...`, then `go test ./...`. Run guarded `BenchmarkResearchSnapshotInsert` for NULL, representative snapshot and near-limit valid JSON with `-benchmem -count=5`; record sizes, allocations and local INSERT latency comparison. No live volume/load test. No arbitrary claim of zero DB cost; actual production threshold is decided at rollout review.
- [ ] **Step 5: Document acceptance/rollout.** Runbook covers safe contract, false/zero/defaults, immutable column, evidence statuses, no retrospective backfill, collector still absent, DB rollback by reverting binary without dropping column. Live approval checklist: schema backup and additive migration, compatible deployment, explicit protocol ID, flag enablement and rollback-off, measured INSERT cost, first sample audit, separate collector Read-key/scoped-role/private-volume approval. No automatic run of these steps.
- [ ] **Step 6: Commit.** `feat: wire opt-in entry capture with safe failure handling`; stage Task 4 files only. Run final independent review after all tasks and fix concrete findings before claiming completion.

## Верификация среды и следующий этап

На 7 октября установлен Go 1.26.7, а go.mod требует 1.27.0. До red/green получить
совместимый toolchain обычным разрешённым способом. Не редактировать go.mod
для сокрытия расхождения; если используется временный совместимый modfile,
отметить результат как дополнительную проверку, не подтверждение Go 1.27.
Нет локальной тестовой БД или sqlc — соответствующий gate остаётся непроверенным,
не заменяется production-запросом. Не устанавливать инструменты скрытно.

После capture отдельный план collector реализует оставшиеся разделы спецификации:
Read-key/scoped SELECT, exact decimal fills, 72h boundary segments, funding/mark
с отдельной достоверностью, metadata observations, quotas/pagination, atomic
private bundle и coverage. Потребитель принимает этот snapshot schema=1,
не legacy TradeConfigSnapshot; unknown/incomplete не повышаются до complete.
Его план должен включить regressions из сводки: DB-open/OKX-flat, дробный объём,
отсутствующий exit ledger, исключённый ARB и непроверенный funding.

## Self-review плана

- Раздел 1 спецификации распределён по Tasks 1–4; tests capture из раздела 5
  покрыты. Остальные разделы принадлежат явно отложенному collector, не потеряны.
- Interfaces producer/consumer совпадают; capture pointers не сериализуются
  автоматически. Planned копируется до мутации, actual — после неё.
- Пять Review Focus привязаны к именованным tests. Историческая гипотеза ARB
  не подменяется синтетической fixture и не считается исправленной capture.
- Обновление исполнения 7 октября: пользователь согласовал план; код, файл
  миграции и тесты реализованы в локальных коммитах. Независимое review выявило
  смешение `ts`/`fillTime`; оно исправляется отдельным regression-тестом.
  PostgreSQL/parity gates и INSERT benchmark ещё не выполнены: Docker Desktop
  остановлен, запрошено разрешение на временную локальную БД. Миграция в
  production, push, deployment и включение capture не выполнялись. Этот статус
  не заменяет непроверенные Expected/gates и не означает готовность к деплою.
