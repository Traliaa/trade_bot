# Exit Research Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Execution method is awaiting user selection; native execution is recommended for these sequentially dependent tasks.

**Goal:** Воспроизводимо сравнить три модели выхода на одинаковых проверенных входах, не меняя реальную торговлю.

**Architecture:** Отдельные типы, валидатор, чистая модель решений, исполнитель с ledger и парный отчёт в `internal/research`. Новый локальный CLI `cmd/research-exits` не импортирует клиенты биржи/БД и не изменяет старый replay. Исследование не является точной копией production, портфельным backtest или доказательством прибыльности.

**Tech Stack:** Go, standard library, существующий `internal/research`; без новых зависимостей. Репозиторий требует Go 1.27.0.

**Spec:** `docs/superpowers/specs/2026-10-01-exit-research-design.md`, согласована пользователем 1 октября 2026. Читать целиком вместе с `docs/strategy-audit-2026-10-01.md`.

**Status:** План на согласовании. Ни один implementation step не выполнен.

## Global Constraints

- Не менять production runner, настройки, риск, UI, БД, ордера и состав торгуемых инструментов. Не исправлять спорную историю автоматически.
- Новый CLI читает только локальный JSON; без клиентов OKX, БД, сетевых запросов и доступа к ключам. Существующий `cmd/research` не меняется.
- Три фиксированных профиля: `fixed_v1`, `configured_trailing_v1`, `runner_3r_v1`; без автоматического подбора.
- Контракты и исходный риск одинаковы для вариантов одной записи. 1R = расстояние фактического исходного входа до исходного SL, неизменно.
- В основное сравнение допускается только `verified`; общие/неатрибутированные позиции, включая спорную AVAX, исключаются.
- BE с 1R, offset 0,1R; LOCK с 1,2R, offset 0,6R; partial 50% с 1,25R; time-stop 12 × 15 минут при current R < 0; early time-stop выключен.
- Stale: 16 баров, min MFE 0,35R, exit profit 0,25R, near BE −0,03R, max adverse −0,65R, grace 6 баров, worse-by 0,30R, tighten-to-BE 0,05R.
- Runner: активация по закрытию 1m при current R >= 3; LONG SL = close − 1R, SHORT SL = close + 1R; LONG округляется вниз по tick, SHORT вверх; лучший стоп не ослабляется.
- Configured: не больше одного успешного действия в 15m-слоте; runner — не больше одного действия на 1m. После активации runner нет time/stale выхода.
- Partial округляется вниз; закрываемая часть и остаток должны соответствовать lot/min size. Невозможный partial не блокирует SL.
- TP полного остатка отсутствует в `runner_3r_v1` с самого начала; другие профили используют исходный TP.
- Комиссия входа учитывается один раз. Ledger: gross + signed fees + funding. Валюта первого этапа — линейные USDT-контракты; иные валюты исключаются, не конвертируются молча.
- Одинаковые завершённые примеры образуют парный итог; незакрытые — `censored`, mark-to-market отдельно. Нет доходности счёта/портфельной просадки.
- Неизвестные funding/спреды явно помечаются; такие результаты — чувствительность к допущениям, не основание для live.
- Реальные данные не подменять синтетическими или современным составом монет. Ни вывод CLI, ни тесты не запускают торговлю.

## Review Focus

1. Вход на несколько секунд позже начала минуты: high/low до входа нельзя использовать; отсутствие сегмента после входа исключает пример (Task 1).
2. На открытии после сигнала partial уже нарушен SL: сначала полное закрытие по SL, без двойной продажи; новый SL не действует задним числом (Task 3).
3. Funding ровно во время входа/partial и внутри неоднозначной OHLC: размер берётся до события, не из первоначального размера на всю сделку; неопределённость отмечается (Task 3).
4. Partial меньше min size, а BE уже достижим: пропуск partial не останавливает защиту и не увеличивает объём (Task 2).
5. Выгодный профиль остался открытым в конце данных: его нельзя закрыть фиктивно или сравнить на более удобном подмножестве; пустая выборка не означает нулевую прибыль (Task 4).

## Файлы, границы и проверки окружения

Новые production-файлы только в `internal/research/exit_*.go` и `cmd/research-exits/main.go`.
Новые тесты рядом; синтетический fixture в `internal/research/testdata/exits-v1.json`.
Документация — `docs/exit-research.md`. Никаких миграций и изменений `go.mod`.
Типы нового режима не расширяют старые `Dataset`, `Options`, `Trade`, `Report`.

Перед исполнением проверить git status и изоляцию по using-git-worktrees; уже созданную
системой изоляцию не дублировать. Пользовательские изменения не переносить/перезаписывать.
Команды ниже запускаются из корня нужного checkout. Сначала `go version`.
Если Go 1.27 недоступен, явно сообщить ограничение: допустима дополнительная проверка
Go 1.26.7 с временным modfile вне репозитория, но не объявлять её нативной Go 1.27.
Не использовать production DSN, сетевые интеграционные тесты и env с ключами.

## Task 1: Контракт данных, проверка покрытия и синтетический пример

**Files:** Create `internal/research/exit_types.go`, `exit_dataset.go`, `exit_example.go`, `exit_dataset_test.go`, `testdata/exits-v1.json`.

**Interfaces:**
- `DecodeExitDataset(io.Reader) (ExitDataset, error)` — strict JSON, один документ, максимум 128 MiB; без чтения путей/URL из JSON.
- `ValidateExitDataset(ExitDataset) (ExitValidation, error)` — структурные ошибки фатальны; непригодные примеры возвращаются как exclusions.
- `DefaultExitManifest() ExitManifest`; `ExampleExitDataset() ExitDataset` — явно синтетические данные и полный manifest.
- `ExitDataset`: `Schema int`, `Provenance string`, `Manifest ExitManifest`, `Samples []ExitSample`; JSON snake_case, `schema=1`, отдельный формат от старого replay.
- `ExitManifest`: `Version string` (`exit-policies-v1`), `Config ExitPolicyConfig`, `Costs ExitCosts`.
- `ExitPolicyConfig`: собственные числовые поля с именами соответствующих параметров `models.TrailingConfig` из спецификации; дополнительно `FixedTimeStopBars int` = 12. Не включать `models.Settings` с API-ключами. Значения по умолчанию только в example, не подставлять скрыто при декодировании.
- `ExitCosts`: `ExitFeeBPS float64` (положительное = расход), `SlippageBPS float64` (неотрицательное). Зафиксированные допущения примера: 5 bps и 3 bps; не выдавать за реальные тарифы.
- `ExitSample`: `ID, Symbol, Side, Provenance, ReconciliationStatus, FeeCurrency string`; `EntryAt, EndAt time.Time`; `Entry, InitialStop, InitialTarget, Contracts, ContractValue, TickSize, LotSize, MinSize, EntryFee float64`; `Config ExitPolicyConfig`, `SpreadBPS *float64`, `AssumedSpreadBPS float64`, `FundingComplete bool`, `Bars []ExitBar`, `Funding []Funding`, `EntryTail *ExitBar`. Config — явный snapshot эффективных правил на входе; для первого фиксированного исследования должен совпадать с Manifest.Config, иначе exclusion `config_snapshot_mismatch`, а не молчаливая замена исторических правил.
- `ExitBar`: `Start, End time.Time`, `Open, High, Low, Close float64`, `Source string`. Основные бары ровно 1m; EntryTail допускает короткий сегмент от точного EntryAt до следующей минутной границы, полученный из более детальных данных, с непустым Source.
- `ExitValidation`: `Accepted []ExitSample`, `Excluded []ExitExclusion`, `Warnings []string`; `ExitExclusion`: `SampleID, Reason string`. Accepted — детерминированная копия, отсортированная по ID; входные данные не мутировать.

- [ ] **1. Write failing tests** в `exit_dataset_test.go`:
  - `TestExitDatasetStrictJSON`: round-trip example успешен; неизвестное поле, лишний JSON, неизвестная схема/manifest, пустое provenance, повтор ID и файл >128 MiB возвращают error. Тест лимита — генерируемый reader, не файл в 128 MiB.
  - `TestExitDatasetNumericAndGeometry`: NaN/Inf через прямой Go API, отрицательная цена/размер, неверные OHLC, SL/TP не с той стороны, некратный лоту объём, нулевой tick и переполнение `contracts*ctVal*riskDist` дают error.
  - `TestExitDatasetCoverage`: duplicate/перекрывающиеся/пропущенные 1m внутри `[EntryAt, EndAt)` дают error; данные после EndAt не влияют на принятую траекторию; нет покрытия до EndAt — exclusion `insufficient_coverage`.
  - `TestExitDatasetIntraminuteEntry`: вход 12:00:02 без EntryTail исключён с `intraminute_entry_coverage_missing`; полный 12:00 OHLC не заменяет tail; tail 12:00:02—12:01 + дальнейшие 1m принят; tail с началом до входа отвергнут.
  - `TestExitDatasetEligibility`: mismatch/incomplete исключаются с `reconciliation_not_verified`; USDC fee исключается с `unsupported_fee_currency`; неизвестный статус — error; другой Config исключается с `config_snapshot_mismatch`; funding incomplete и null spread оставляют предупреждения, а не «нулевые расходы».
  - `TestExitDefaultManifest`: точные значения всех Global Constraints; `EntryFee` может быть положительным rebate; ошибочные enabled partial fraction вне `(0,1)` отвергнуты.
- [ ] **2. Run RED:** `go test ./internal/research -run 'TestExit(Dataset|Default)' -count=1`; ожидается FAIL из-за отсутствующих типов/функций.
- [ ] **3. Implement** перечисленные интерфейсы. Decode проверяет наличие обязательных JSON-полей (в том числе нулевых fee/config) до преобразования в значения: отсутствующее не равно нулю. Для missing spread использовать только явно заданный `AssumedSpreadBPS` и warning. Проверять funding currency через USDT-область sample, конечность rate/mark, symbol и уникальность timestamp. EndAt — граница минутного покрытия; не сдвигать её молча.
- [ ] **4. Run GREEN:** та же команда и `go test ./internal/research -count=1`; ожидается PASS, старые тесты без изменения.
- [ ] **5. Commit:** добавить только файлы Task 1; `git commit -m "feat: define validated exit research datasets"`.

## Task 2: Чистая модель решений и ограничения partial/SL

**Files:** Create `internal/research/exit_policy.go`, `exit_policy_test.go`.
**Read references:** `sessions/trailing_helpers.go`, `sessions/protection_safety.go`, `sessions/profit_runner.go`, `helper.TrailSlot15m`, `models.GetStaleConfig`. Не изменять их и не импортировать live sessions.

**Interfaces:**
- Consumes: типы Task 1.
- `ExitProfile string`: три значения из Global Constraints; неизвестное значение — error.
- `NewExitState(ExitSample, ExitProfile) (ExitState, error)`.
- `DecideExit(ExitState, ExitPolicyConfig, ExitBar) (ExitState, ExitDecision, error)` — обновляет только наблюдаемое состояние/намерение; не исполняет ордер и не списывает деньги.
- `ExitState`: `Profile ExitProfile`, `Side string`, `EntryAt, LastActionSlot, LastRunnerActionAt, StaleSince time.Time`; `Entry, InitialRiskDist, InitialContracts, ContractValue, Remaining, Stop, Target, TickSize, LotSize, MinSize, MFEPrice, StaleMarkedAtR float64`; `PartialDone, BEActivated, LockedProfit, RunnerActive, IsStale bool`.
- `ExitDecision`: `Kind string` (`none`, `move_stop`, `partial`, `close`), `Reason string`, `At time.Time`, `Size, NewStop float64`. NewStop у partial применяется только после исполнения; первоначальный Stop остаётся действующим до этого.

- [ ] **1. Write failing tests:**
  - `TestExitPolicyRunnerThreshold`: LONG entry100/SL90, partial выключен: close129.99 не активирует runner, close130 активирует со SL120; SHORT entry100/SL110, close70 -> SL80. Предварительные BE/LOCK проверяются отдельно и не отменяются.
  - `TestExitPolicyNeverLoosensAndRounds`: активный LONG stop125, close132 ->125; SHORT зеркально. Tick0.3, LONG close130.1 ->120, SHORT close69.9 ->80.1; 1R остаётся10.
  - `TestExitPolicyPartialMinimum`: qty1/min1/lot1, requested0.5 -> без partial, но допустимый BE сохранён; qty2 -> partial1, остаток1; PartialDone=true запрещает повтор.
  - `TestExitPolicyConfiguredPriority`: при time-stop и partial одновременно выбран close; до 180 минут time-stop нет, в180 минут currentR<0 — есть; currentR=0 не закрывается. Stale grace/degradation и min-improve LOCK0.1R сравниваются с зафиксированными таблицами reference-кода.
  - `TestExitPolicySlotsAndState`: использованный 15m-slot запрещает второе configured-действие, следующий разрешает; runner обходит 15m-slot, но не повторяет минуту; stale state сохраняется даже при `none`.
  - `TestExitPolicyProfileSeparation`: fixed не переносит SL; configured оставляет initial TP; runner target отключён с начала. После RunnerActive time/stale не закрывают позицию, даже при откате ниже3R.
  - `TestExitPolicyMFEVsClose`: configured partial/BE оцениваются по MFE, runner активируется по close; будущие бары не доступны функции.
- [ ] **2. Run RED:** `go test ./internal/research -run TestExitPolicy -count=1`; ожидается FAIL из-за отсутствующего evaluator.
- [ ] **3. Implement** evaluator: порядок reference из спецификации, clamp остатка по lot вниз, fallback без partial к защитному решению, монотонный SL. Для откатившей цены нельзя устанавливать новый SL по неправильную сторону текущего close; вернуть `none` с причиной `stop_not_placeable`, не расширять риск. Успешность действия/LastActionSlot фиксирует исполнитель Task 3, не сам факт предложенного partial.
- [ ] **4. Run GREEN:** та же команда, затем все `./internal/research`; PASS. Fixtures называют источник правил, но не утверждают полную parity с V3 callback или биржевым исполнением.
- [ ] **5. Commit:** только файлы Task 2; `git commit -m "feat: model configured exits and three-R runners"`.

## Task 3: Исполнение по времени и полный денежный ledger

**Files:** Create `internal/research/exit_execution.go`, `exit_ledger.go`, `exit_execution_test.go`.

**Interfaces:**
- Consumes: Task 1–2, принятую sample и manifest.
- `ReplayExitSample(ExitSample, ExitManifest, ExitProfile, float64) (ExitOutcome, error)`; последний аргумент — cost multiplier ровно1 или2.
- `ExitOutcome`: `SampleID string`, `Profile ExitProfile`, `CostMultiplier float64`, `Status string` (`closed`, `censored`), `CloseReason string`, `CloseAt *time.Time`, `Gross, Fees, Funding, Net, NetR, Remaining, MarkToMarket, RemainingRisk float64`, `Ledger []ExitLedgerEvent`, `Warnings []string`, `Ambiguities int`.
- `ExitLedgerEvent`: `At time.Time`, `Kind, Reason string`, `Price, Size, Gross, Fee, Funding, RemainingAfter float64`; Kind=`entry`, `partial`, `exit`, `funding`, `stop_move`; один entry event на исходную sample. Нулевые денежные поля не скрывать.
- `recordExitFill(*ExitState, *[]ExitLedgerEvent, time.Time, float64, float64, float64, string) error` — цена, размер, signed fee, reason; единственное место расчёта gross/остатка. Не принимает внешние биржевые fills.

- [ ] **1. Write failing tests:**
  - `TestExitExecutionLedgerConservation`: entry100×2/ctVal1/fee−0.1, partial1@112.5/fee−0.05625, exit1@120/fee−0.06, funding−0.12: gross32.5, fees−0.21625, net32.16375, остаток0; риск20, NetR=net/20. Отдельно положительный rebate и запрет sell размера больше Remaining.
  - `TestExitExecutionNoRetroactiveStop`: high пересёк3R, close тоже, low ниже будущего SL но выше старого; в этой свече новый стоп не срабатывает, в следующей — может. Зеркально SHORT.
  - `TestExitExecutionStopFirstAndGap`: entry100/SL90/TP115, OHLC100/116/89/110 -> SL90; следующая open85 ->85 до расходов. Оба касания увеличивают Ambiguities.
  - `TestExitExecutionPendingPartialVsStop`: partial ожидает следующее open, open ниже действующего SL -> закрыть весь остаток, partial не исполнить. Если SL не нарушен, partial исполняется по next open с расходами, не по прошлому close.
  - `TestExitExecutionFundingSizes`: на timestamp входа funding0; при rate0.001/mark120 и остатке1 -> LONG−0.12, SHORT+0.12; timestamp partial начисляет на размер до partial; timestamp полного закрытия — на удержанный до него остаток. Funding внутри свечи с неизвестным временем SL/TP помечается `intrabar_funding_ambiguous`, не заявляется точная биржевая сумма.
  - `TestExitExecutionCensoring`: на EndAt есть остаток -> censored, без фиктивного exit fee/gross; pending market-action после последнего close не исполняется; mark-to-market остатка отдельно от реализованного ledger.
  - `TestExitExecutionStressAndFuture`: x2 удваивает отрицательные комиссии и slippage; не удваивает положительный rebate, spread или funding; фактическая entry price/qty не меняются. Добавление баров после EndAt не меняет output; повторный прогон DeepEqual.
- [ ] **2. Run RED:** `go test ./internal/research -run TestExitExecution -count=1`; ожидается FAIL из-за отсутствующего replay.
- [ ] **3. Implement** event ordering: funding на границе для старого остатка -> уже нарушенный защитный SL/TP на open -> pending market action -> действующие intrabar SL/TP (stop-first) -> close-наблюдение/решение. Новый SL начинает действовать только на следующем open; partial+SL применяются согласованно после частичного fill. Защитный выход при неоднозначном внутриминутном времени датируется End и помечается как модельное допущение.
  - Цена выхода корректируется неблагоприятно на `(slippageBPS*multiplier + spreadBPS/2)/10000`, fee считается с фактического exit notional. Вход уже исполнен: не начислять повторный entry spread/slippage.
  - Funding внутри свечи обрабатывается на известный до неопределённого выхода размер с warning; если точный порядок не восстановить, результат sensitivity-only.
  - Для `fixed_v1` таймер `FixedTimeStopBars*15m` без условия currentR; для других — правила Task 2. У censored Net — реализованная часть с уже списанными расходами; NetR так же, MTM остатка отдельно.
  - Проверять конечность каждого денежного произведения и сумму событий; числовая ошибка возвращает error, не NaN JSON. Толеранс инвариантов: `1e-8 + 1e-9*max(abs(a),abs(b))`.
- [ ] **4. Run GREEN:** та же команда; `go test -race ./internal/research -count=1`; PASS, старый replay неизменён.
- [ ] **5. Commit:** только файлы Task 3; `git commit -m "feat: replay exit fills and funding without lookahead"`.

## Task 4: Парный отчёт, offline CLI и пользовательская инструкция

**Files:** Create `internal/research/exit_report.go`, `exit_report_test.go`, `cmd/research-exits/main.go`, `cmd/research-exits/main_test.go`, `docs/exit-research.md`.

**Interfaces:**
- `CompareExits(ExitDataset, bool) (ExitComparison, error)` — bool включает x2 costs, всегда вычисляет все три профиля.
- `ExitComparison`: `ModelVersion, DatasetSHA256, CodeRevision, Provenance string`, `Manifest ExitManifest`, `Excluded []ExitExclusion`, `Outcomes []ExitOutcome`, `Cohorts []ExitCohort`, `Warnings []string`. SHA/revision заполняет CLI по исходным bytes/build info, библиотека не читает git/файлы.
- `ExitCohort`: `CostMultiplier float64`, `SampleIDs []string`, `Profiles []ExitProfileStats`, `Deltas []ExitPairedDelta`, `Status string` (`available`, `no_common_closed_samples`). Cohort — пересечение закрытых ID всех трёх профилей на одном multiplier, явно отдельно от exclusions/censored.
- `ExitProfileStats`: `Profile ExitProfile`, `Count, Wins, Losses, Breakeven int`, `Net, Fees, Funding, MeanNetR, WinRate float64`, `ProfitFactor, Top1ProfitShare, Top3ProfitShare *float64`. PF=null без убытков; concentration — доля top положительных результатов в сумме положительных, null без выигрышей.
- `ExitPairedDelta`: `Against ExitProfile`, `SampleID string`, `CostMultiplier, NetDifference, RDifference float64`; runner сравнивается с fixed и configured на общем cohort. Профиль runner подразумевается и явно описывается в JSON schema/docs.
- CLI `run(args []string, stdout, stderr io.Writer) error`, собственный `flag.FlagSet`; `main` только выводит ошибку и возвращает nonzero. Флаги `-input <path>`, `-example`, `-stress=true`; default input отсутствует, URL не открываются.

- [ ] **1. Write failing tests:**
  - `TestExitComparisonCommonCohort`: один пример закрыт во всех профилях, другой censored только в runner -> Count1 в каждой сводке; второй остаётся в Outcomes со статусом, не пропадает. Пустой common cohort -> статус `no_common_closed_samples`, Profiles пустой, предупреждение; не три «прибыль=0» сводки.
  - `TestExitComparisonMetrics`: net `[2,-1,1]` -> PF3, winrate2/3, top1share2/3, top3share1; без losses PF null; без wins shares null. NetR использует исходный денежный риск, не margin/leverage. Переполнение сумм/отношений возвращает error, не NaN/Inf JSON.
  - `TestExitComparisonPairedStress`: сортировка по multiplier/profile/ID стабильна; результаты base/stress разделены; нет смешивания ID между cohort. Смена порядка samples/bars не меняет расчёт после канонизации; duplicate bars остаются ошибкой.
  - `TestExitComparisonWarnings`: retained exclusions, funding/spread assumptions, missing V3 callback, intrabar ambiguity, censoring и непортфельная область всегда видны. Поля `eligible_for_live`/«рекомендуемые live-настройки» отсутствуют.
  - `TestExitCLIExampleRoundTrip`: example -> Decode -> Compare -> JSON success; SHA-256 соответствует исходным bytes; версия модели присутствует, revision включает dirty либо явно unknown; build info не выдумывается.
  - `TestExitCLIRejectsInvalidInput`: missing flags/file, malformed/trailing JSON, size limit, URL input -> error, stdout без частичного success JSON. Проверка нового CLI через Go AST/imports запрещает DB/OKX/net/http-клиенты, exec и чтение env-ключей; синтетический прогон не требует credentials.
- [ ] **2. Run RED:** `go test ./internal/research ./cmd/research-exits -run 'TestExit(Comparison|CLI)' -count=1`; ожидается FAIL до создания отчёта/CLI.
- [ ] **3. Implement** перечисленные контракты, расчёты по ledger/outcomes и детерминированный pretty JSON. Все показатели вычислять только на заявленной выборке. В `docs/exit-research.md` описать schema, команды, profiles, время исполнения, signed fees, ограничения и причины исключения; привести синтетический пример, не обещание доходности.
- [ ] **4. Run GREEN and branch verification:**
  - `go test ./internal/research ./cmd/research-exits -count=1` — PASS.
  - `go test -race ./internal/research ./cmd/research-exits -count=1` — PASS.
  - `go run ./cmd/research-exits -input internal/research/testdata/exits-v1.json -stress=true` — JSON с base/x2 outcomes всех профилей и предупреждением synthetic.
  - `go test ./... -count=1`, `go build ./cmd/...`, `git diff --check` — PASS; при toolchain/environment blocker сообщить точную невыполненную проверку.
  - Просмотреть diff: отсутствуют изменения live-кода, старого replay, env/keys, миграций и production-параметров.
- [ ] **5. Commit:** только файлы Task 4; `git commit -m "feat: expose reproducible paired exit research reports"`. Независимое ревью по выбранному workflow, замечания покрывать тестами. Не push/deploy без запроса.

## После реализации: реальные данные и пределы вывода

Это контрольная точка исследования, не скрытая задача сетевого скачивания внутри CLI.
Составить coverage-отчёт по проверенным входам, исходным SL/TP, fee currency,
tail первой минуты, 1m до общего горизонта, funding и spread. Для каждого пробела
назвать источник, доступность и влияние. Реальные account/order IDs и ключи в git не сохранять.
Без достоверного tail большинство входов с секундами внутри минуты будут исключены;
не исправлять это округлением timestamp. Длительное хранение/сбор tick-данных — отдельное решение.

Проверить входной dataset на неизвестные/повторные записи; AVAX с mismatch не включать.
Если real dataset пригоден, выполнить три профиля и x2 costs на заранее зафиксированном
периоде, не использованном для подбора. Если все реальные записи исключены или есть
существенные пропуски, выдать coverage-отчёт, а не «стратегия улучшена».
Проверенный симулятор сам по себе не доказывает прибыльность и не завершает
последующие исследования входов, списка монет и портфеля.

## Самопроверка плана

- Spec coverage: данные/покрытие → Task1; профили/пороговые решения → Task2;
  время, SL/partial/funding/ledger → Task3; paired/stress/provenance/CLI → Task4.
- Все пять Review Focus имеют именованные тесты у владельца реализации.
- Реализуемые интерфейсы перечислены перед потребителями; названия новых типов
  не пересекаются со старым API. Production session/config не импортируются.
- Известные ограничения явно сохранены: intraminute data, V3 callback, портфель,
  неизвестные расходы, funding ambiguity, Go toolchain.
- Нет автоматического выбора победителя, изменения trading settings или исправления AVAX.
