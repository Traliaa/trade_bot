# Real-data exit comparison Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Получить проверяемое сравнение A/B/C на одинаковых реальных входах либо отчёт о конкретных препятствиях без ложного результата прибыльности.

**Architecture:** Начать с read-only preflight исторических источников; при отсутствии обязательных данных остановить зависимые задачи. Сбор и сверка изолированы от offline replay и production. Существующие v1-профили остаются неизменны; новый профиль C и v2-отчёт добавляются отдельно.

**Tech Stack:** Go, существующие pgx/v5 и стандартные net/http, encoding/json, math/big; существующий internal/research. Новых зависимостей и изменений go.mod нет.

**Spec:** `docs/superpowers/specs/2026-10-05-exit-comparison-real-data-design.md` (одобрено сообщением «продолжай» 5 октября).

## Global Constraints

- Не менять production-код, БД, риск, настройки, список инструментов, открытые позиции, ордера, webhook, деплой и расписание. Не отправлять торговые запросы.
- Только SELECT в read-only транзакциях и разрешённые GET OKX. Секреты — только в памяти, не в аргументах, файлах и логах.
- Все варианты используют одинаковые входы, исходные размеры, исходный риск, рынок, временной горизонт и модель расходов. Никакого перебора параметров.
- Зафиксировать единый горизонт 72 часа после входа; неполный горизонт не заменять фактическим временем выхода.
- C: первая частичная фиксация 50% исходного объёма после закрытия 1m на current R≥3; исполнение не раньше следующего открытия. Исходный R неизменен.
- BE/LOCK/time/stale до 3R не меняются; после активации runner time/stale выключены, стоп не ослабляется.
- История до 5 октября — исследовательская, не out-of-sample. Прибыль не является обязательным исходом и не разрешает live-деплой.
- Данные счёта не коммитить. В репозиторий попадают только обезличенные synthetic fixtures, код и проверенные агрегаты/отчёты покрытия.

## Review Focus

1. Ошибка источника не должна раскрывать URL с токеном, DSN, account/order ID или заголовки авторизации: тест задачи 2.
2. Пустая/повторяющаяся страница архива не доказывает полноту истории: preflight и тест задачи 2.
3. Дробный контракт, rebate и несовпадающая валюта комиссии не должны превращаться в правдоподобный неверный net: тест задачи 2.
4. После сигнала partial на 3R гэп может пересечь прежний SL: приоритет полного защитного выхода, без второго исполнения partial; тест задачи 3.
5. Незавершённый fixed-control не должен удалять закрытую тройку A/B/C из основной статистики: тест задачи 4.

## Карта файлов

- `docs/exit-research-coverage-2026-10-05.md`: preflight, источники и ограничения; не P&L несуществующего прогона.
- `internal/research/exitdata/{types,reconcile,collect}.go`, соответствующие `_test.go`: изолированный экспорт, сверка и coverage. Пакет импортирует research, но research не импортирует exitdata.
- `cmd/research-exit-data/main.go`, `main_test.go`: единственный сетевой/БД CLI этого этапа, только read-only адаптеры; не часть сервиса бота.
- `internal/research/exit_policy.go`, `exit_execution.go`, `exit_ledger.go` и тесты: профиль C, состояние и диагностика partial.
- `internal/research/exit_report_v2.go`, `exit_report_v2_test.go`: A/B/C-отчёт; действующий `CompareExits` v1 сохраняется.
- `cmd/research-exits/main.go`, `main_test.go`, `docs/exit-research.md`: явный выбор версии сравнения, offline CLI и документация.

---

### Task 1: Preflight реальных источников — обязательный шлюз

**Files:** Create `docs/exit-research-coverage-2026-10-05.md`.
**Interfaces:** вход — read-only БД, архивные GET OKX и существующие локальные исторические файлы; выход — `GO` или `BLOCKED` для задач 2–5 с причинами, а не разрешение на live.

- [ ] Зафиксировать cutoff UTC до чтения кандидатов; выборка `entry_at >= 2026-09-12T00:00:00+03:00` и `< min(cutoff, 2026-10-06T00:00:00+03:00)`, один ранее разрешённый аккаунт. Опубликовать лишь количество и агрегаты; открытые записи и недостаточный горизонт пометить отдельно.
- [ ] Проверить актуальные официальные контракты OKX для fills-history, history-candles, истории funding/mark и публичной tick-history: срок хранения, пагинация, время/единицы, лимиты. Ссылки и время проверки записать в coverage. Не считать наличие endpoint доказательством полноты ответа.
- [ ] Проверить локальные исторические snapshot правил/метаданных; не искать секреты широким рекурсивным чтением. Выбрать до трёх кандидатов с разных доступных дат (включая intraminute-вход), выбор фиксировать до просмотра результатов альтернативных выходов.
- [ ] Ограниченными запросами проверить доказуемое покрытие entry-tail, 72h 1m, исторических tick/lot/min/ctVal и правил. Зафиксировать границы архивов и отсутствие/наличие необходимого происхождения. Не загружать всю историю до этого шага.
- [ ] Проверить полную атрибуцию fills для кандидатов и как минимум один случай partial; AVAX не повышать до verified. Не считать пустой ответ за пределами срока хранения доказанным отсутствием исполнений.
- [ ] Выдать `GO` только при наличии хотя бы одного кандидата с доказуемыми обязательными данными и подтверждённых источников для массового сбора. Funding/спред могут дать лишь sensitivity-only; это не eligibility для live. Иначе `BLOCKED`: перечислить поля и остановить задачи 2–5, предложив отдельное согласование будущего сбора.
- [ ] Проверить отчёт на секреты/идентификаторы и `git diff --check`; локально закоммитить только coverage. Никаких synthetic результатов под видом real.

### Task 2: Изолированный read-only сборщик и сверка

**Prerequisite:** `GO` задачи 1. До реализации подтвердить источники и поля из preflight; не добавлять недоступные источники как фиктивные заглушки.
**Files:** Create `internal/research/exitdata/{types,reconcile,collect}.go`, `reconcile_test.go`, `collect_test.go`, `cmd/research-exit-data/{main,main_test}.go`.

**Interfaces:**
- `Fill`: внутренние `ID, OrderID, Symbol, Side, Role, FeeCurrency string`, `At time.Time`, десятичные строки `Size, Price, Fee, RealizedPnL` (не публичный JSON).
- `Candidate`: `Sample research.ExitSample`, `EntryOrderIDs, ExitOrderIDs []string`, `Fills []Fill`, `Evidence map[string]string`, `ObservedExitAt *time.Time` — только в памяти; Evidence содержит безопасное происхождение исходного риска, config, metadata и рынка.
- `CoverageItem`: `SampleID, Status string`, `Reasons []string`, `Evidence map[string]string`; разрешён только whitelist безопасных полей.
- `VerifyCandidate(c Candidate) (research.ExitSample, CoverageItem, error)` — чистая функция; объединение и атрибуция исполнений до выдачи `verified`.
- `Source.Candidates(ctx context.Context, from, to time.Time) ([]Candidate, error)` и `Source.Enrich(ctx context.Context, c Candidate, end time.Time) (Candidate, error)` — интерфейс для подмены источника в тестах; real-адаптер читает pgx и GET API.
- `Collect(ctx context.Context, src Source, from, to, cutoff time.Time, manifest research.ExitManifest) (research.ExitDataset, []CoverageItem, error)`; EndAt = EntryAt+72h.

- [ ] Написать `TestVerifyCandidateConservation`: точные десятичные fixtures entry=2, partial=0.5, final=1.5 допускаются; final=2 после partial исключается; одинаковый fill ID повторно не учитывается, конфликтующие копии одного ID — ошибка. Реализовать сравнение денег/объёмов через math/big.Rat до преобразования в replay float64; после преобразования проверить конечность.
- [ ] Добавить `TestVerifyCandidateAttributionAndCurrency`: чужой order ID, неизвестный источник увеличения позиции, AVAX-подобный entry=1/exit=1.5, неподтверждённая валюта комиссии не становятся verified; rebate остаётся положительным; entry fee не списывается дважды. Полный записанный net сравнивается с ledger, расхождение остаётся mismatch.
- [ ] Добавить `TestCollectCoverageNotInferred`: нет entry-tail, исторической metadata, полного config либо непрерывных 72h — исключение с причиной; неизвестный funding/спред сохраняет предупреждение sensitivity-only. Разные подтверждённые конфигурации не смешивать: один manifest на набор, отдельный coverage для остальных.
- [ ] Добавить `TestReadOnlySourceSafety`: fake HTTP транспорт отклоняет любой метод кроме GET и неразрешённый host/path; переходы на другие hosts запрещены; DB выполняет read-only repeatable-read snapshot. Пагинация хранит seen cursor, имеет пределы из preflight и прекращается с incomplete, а не verified, при повторе курсора/ошибке/исчерпании лимита. Контекст отмены и таймауты обязательны.
- [ ] Добавить `TestExportRedactsErrorsAndIDs`: sentinel-секреты и private IDs отсутствуют в stdout/stderr/JSON даже при HTTP/DB ошибках; входные credentials поступают через отдельный inherited FD/скрытый stdin, не флаги. Диагностика содержит код, источник и обезличенный sample ID, не raw response. Сравнить повторные выгрузки одного frozen fake snapshot: одинаковый безопасный JSON/hash.
- [ ] Запустить `go test ./internal/research/exitdata ./cmd/research-exit-data -count=1`: RED по отсутствующим интерфейсам. Реализовать минимальные адаптеры в выделенном CLI; не импортировать запуск приложения или mutating methods клиента бота.
- [ ] Проверить GREEN тем же тестом. В CLI флаги только `-from`, `-to`, `-cutoff`, `-output-dir`, `-manifest`; существующие выходные файлы не перезаписываются. Выходы 0600 в явно заданном приватном каталоге; ID порядка sample-0001 по отсортированным кандидатам, не хеш персональных идентификаторов. Коммит только кода/обезличенных тестов.

### Task 3: Профиль C — первый partial строго после +3R

**Files:** Modify `internal/research/exit_policy.go`, `exit_execution.go`, `exit_ledger.go`; tests `exit_policy_test.go`, `exit_execution_test.go`.
**Interfaces:** сохранить `NewExitState`, `DecideExit`, `ReplayExitSample`; добавить константу `ExitRunnerPartial3R ExitProfile = "runner_3r_partial_at_3r_v2"`, не добавлять её в глобальный список v1. `ExitState` хранит исходный объём отдельно от Remaining. `ExitDecision` получает `Warnings []string`; replay переносит их в Outcome, не меняя денежный ledger.

- [ ] Написать `TestPartial3RThresholdAndSymmetry`: entry=100, R=10, size=2; LONG при close=129.99 не делает partial, при close=130 планирует size=1 и SL=120; SHORT close=70 планирует size=1 и SL=80. High=131 при close=110 не активирует C и не создаёт ранний partial; для B прежнее поведение сохраняется.
- [ ] Добавить `TestPartial3RPreservesConfig`: config с partial 1.25R/50% одинаков у A/B/C и не модифицируется; C берёт 50% исходного объёма независимо от конфигурационной доли. Сам профиль явно включает эту гипотезу, а A/B учитывают Config как раньше.
- [ ] Добавить `TestPartial3RExecutionSafety`: исполнение на следующем open; гэп через прежний SL закрывает всё без partial; gap, делающий новый SL недопустимым, сохраняет прежнюю защиту с предупреждением; повторного partial после исполнения нет. При срабатывании BE/LOCK раньше 3R C может законно закрыться раньше.
- [ ] Добавить `TestPartial3RMinimumAndFallback`: size=1, lot=1, min=1 → `partial_below_minimum`, без закрытия и без увеличения размера; SL продолжает сопровождаться. Дробные lot и недостаточный остаток проверяются отдельно; повторение предупреждения не создаёт дополнительных fills.
- [ ] Запустить `go test ./internal/research -run 'TestPartial3R' -count=1`, получить RED. Реализовать ветку C без изменения A/B и исходного R, сохранив правила rounding/never-loosen/next-bar; после активации убрать time/stale.
- [ ] Запустить весь `go test ./internal/research -count=1`, получить GREEN; сравнить v1 synthetic отчёт с сохранённым до изменений fixture. Коммит только research-кода и тестов.

### Task 4: Версионированный отчёт A/B/C и offline CLI

**Files:** Create `internal/research/exit_report_v2.go`, `exit_report_v2_test.go`; modify `cmd/research-exits/main.go`, `main_test.go`, `docs/exit-research.md`.
**Interfaces:** `CompareExitsV2(d ExitDataset, stress bool) (ExitComparisonV2, error)`; `ExitComparisonV2` содержит существующий `ExitComparison` как embedded базу, `Pairs []ExitPairDeltaV2`, `Metrics []ExitMetricsV2`; ModelVersion=`exit-replay-v2`. `ExitPairDeltaV2` содержит `Profile ExitProfile` и embedded `ExitPairedDelta`; `ExitMetricsV2` содержит `Profile`, `CostMultiplier`, `MeanWin, MeanLoss *float64`, `PartialFills, PartialSkipped int`. V1 не меняет формат и смысл; в v2 старое неоднозначное Deltas не используется (пусто), пары в Pairs явные.

- [ ] Написать `TestExitV2CommonCohort`: A/B/C closed и fixed censored → sample включён; C censored → sample исключён из общего closed cohort, но остаётся в Outcomes с MTM. Base/x2 пересечения независимы; нет закрытых общих → `no_common_closed_samples`, не фиктивный нулевой PF.
- [ ] Написать `TestExitV2PairsAndMetrics`: на заранее заданных ledger проверить ровно B−A, C−B, C−A для каждого общего sample; MeanWin/MeanLoss null без соответствующих сделок, суммы net/fees/funding согласованы; partial counts и пропуски различаются. Fixed не влияет на основную сводку.
- [ ] Написать `TestExitCLIComparisonVersion`: новый флаг `-comparison v1|v2`, default v1; v2 выдаёт иной ModelVersion и явные пары. SHA входного файла и revision сохраняются, v1 legacy fixture не изменён. Ошибка входа/версии — nonzero без частичного успешного JSON; CLI остаётся строго offline.
- [ ] Запустить targeted tests и получить RED; реализовать v2 без дублирования ledger/replay и без изменения `CompareExits`. В документации явно отделить configured approximation от фактических выходов и исследовательский период от будущей независимой проверки.
- [ ] Запустить `go test ./internal/research ./cmd/research-exits -count=1`, получить GREEN. Коммит отчёта, CLI, тестов и документации.

### Task 5: Реальная выгрузка, итоговый прогон и handoff

**Files:** update coverage и `docs/exit-research.md`; create `docs/exit-research-results-2026-10-05.md` только при выполненном реальном прогоне. Локальные приватные JSON вне репозитория.
**Interfaces:** результаты Collect проходят существующий ValidateExitDataset; только затем `research-exits -comparison v2 -stress=true`.

- [ ] Зафиксировать toolchain. По go.mod требуется Go 1.27; если он недоступен, использовать временный compatibility modfile с Go 1.26.7 только для проверки, не менять go.mod и явно отметить, что native 1.27 не проверен.
- [ ] Запустить полный `go test ./... -count=1`, затем `go test -race ./internal/research/... ./cmd/research-exits ./cmd/research-exit-data -count=1` и `go build ./cmd/...`; для compatibility во всех командах один и тот же `-modfile`, не смешивать результаты разных конфигураций. Любой сбой исследовать до заявления о готовности.
- [ ] Выполнить read-only export с frozen cutoff/manifest задачи 1. Проверить coverage, суммы и перечень исключений; independently сверить один полный ledger с источником. При нуле пригодных samples остановиться на coverage, не запускать synthetic вместо real.
- [ ] Выполнить base/x2 на каждом согласованном manifest-наборе; не агрегировать разные исторические правила без отдельной маркировки. Сохранить input/output hashes, revision, параметры расходов, cohort IDs, exclusions, censored и предупреждения. Проверить повторный запуск на детерминированность.
- [ ] В итоговом отчёте показать A/B/C, денежные расходы и неполноту funding, пары, концентрацию и отличие от actual ledger. Прямо написать, что выбранные фактические входы и ограничения портфеля не моделируют доходность всего бота и что прошлый анализ уже повлиял на гипотезу.
- [ ] Провести финальное независимое ревью после согласованного способа выполнения, устранить замечания, повторить затронутые тесты. `git diff --check` и проверка staged paths: нет секретов, реальных приватных JSON, изменений production/UI/DB. Локальные коммиты без push/deploy.
- [ ] Передать результат или конкретный блокер данных пользователю; не запускать live и будущий сбор автоматически.

## Самопроверка плана

- Spec → задачи: источники/72h/атрибуция 1–2; профиль C и invariant R 3; A/B/C и цензура 4; воспроизводимость, stress и ограничения 5.
- Все пять Review Focus включены в именованные проверки. Тесты сетевых адаптеров используют fake transport, не производственную биржу.
- Preflight имеет явный STOP: отсутствие обязательных архивных данных не оправдывает обход валидатора или разработку ненужного сборщика.
- Способ выполнения ещё не выбран. Рекомендация: последовательно в этой задаче; сначала preflight, затем только необходимые зависимые задачи и финальное независимое ревью.

## Результат выполнения preflight — 5 октября

Пользователь подтвердил план и последовательное выполнение. Задача 1 дала
BLOCKED: [отчёт покрытия](../../exit-research-coverage-2026-10-05.md).
Задачи 2–5 не начаты; исторические metadata и полные правила не подтверждены.

Дополнительно выявлен дефект интерфейса плана: существующий валидатор требует
минутный EndAt, тогда как точный EntryAt+72h для intraminute-входов не выровнен.
Перед возобновлением нужно дополнить спецификацию/задачи v2-контрактом последнего
неполного бара и его тестами, сохраняя v1. Предыдущая карта задач не считается
готовой к исполнению до устранения этого противоречия. В этом этапе округление
горизонта, изменение валидатора и новый сбор не выполнялись.
