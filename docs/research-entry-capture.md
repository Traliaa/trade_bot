# Снимки входов для исследования выходов

Реализован opt-in capture новых входов. Это не новая стратегия, не исправление
закрытия ARB и не сборщик исторических котировок. Входы, риск, SL/TP, runner,
список монет и Telegram-уведомления не меняются.

## Включение

По умолчанию выключен: `RESEARCH_CAPTURE_ENABLED=false`.
Для отдельно согласованного запуска нужны `RESEARCH_CAPTURE_ENABLED=true` и
`RESEARCH_PROTOCOL_ID` (1–64 ASCII буквы/цифры, `_`, `-`). Не использовать секрет
или идентификатор аккаунта в качестве protocol ID. Ошибочная настройка отключает
только capture и выводит фиксированный код ошибки без её значения.

До запуска нового бинарника, даже с выключенным capture, обязательна additive
миграция `0008_add_research_entry_snapshot.sql`: новый INSERT использует колонку.
Этот документ не запускает миграцию или деплой.

## Контракт данных

`trade_history.research_entry_snapshot` — nullable JSONB, отдельно от payload.
Обычные update/close и API Get/List её не читают/не изменяют. Старые строки
остаются NULL; нет backfill. Ошибка UUID/сериализации/размера даёт NULL,
агрегированный счётчик и тот же один INSERT сделки. Ошибка самого INSERT
по-прежнему возвращается вызывающему коду; дополнительных повторов нет.

Формат: `{ "payload": { "schema": 1, ... }, "checksum": "sha256-hex" }`.
Checksum рассчитан по компактному JSON typed payload в порядке полей builder.
PostgreSQL JSONB меняет порядок ключей: нельзя проверять хеш по `jsonb::text`.
Будущий collector должен восстановить канонический порядок модели, UTC-время
и отсортированные причины. Checksum доказывает только неизменность содержимого.

Максимум 32768 байт, включая envelope. Только whitelist полей:

- Случайный capture UUID, protocol ID, инструмент/сторона/TF, revision/dirty/unknown.
- Planned до корректировки по fills и actual после существующего исполнения.
- Raw metadata из использованного ответа и effective floats, время его получения.
  Нет дополнительного запроса, выдуманного exchange timestamp или raw из float.
- Raw/effective BE, LOCK, partial, time и все stale-поля, включая false/0;
  effective stale использует существующий `GetStaleConfig`.
- Отдельные наблюдения существующих чтений calc/sizing/open. Разные relevant
  hashes → `settings_changed_during_entry`, не единый выдуманный конфиг.
- Источник времени, первый/последний fill, количество и наблюдаемый объём.

`complete` означает полноту захваченных наблюдений, а не сверку с архивом биржи.
`reported_complete` — результат существующего ожидания fills с проверкой
наблюдаемого объёма. Timeout, короткий/неоднозначный объём, разные времена fills,
неизвестная сборка/metadata дают incomplete. Сумма floats не исправляется
молча до ожидаемого размера; точная decimal-сверка принадлежит collector.
Локальное fallback-время не называется биржевым.

Нет Settings целиком, API-ключей, DSN, account/Telegram/order/private trade IDs
или текстов приватных ошибок. Research-снимок исключён из JSON TradeRecord.
Счётчики доступны через существующий ExecutionStats: `research_capture_complete`,
`research_capture_incomplete`, `research_capture_failed`, `research_capture_disabled`
и фиксированные `research_capture_error_*`. Дополнительных Telegram-сообщений нет.

## Проверки

Требуется Go 1.27.0 по go.mod. На машине с `GOTOOLCHAIN=local` и Go 1.26.7:
`GOSUMDB=sum.golang.org GOTOOLCHAIN=go1.27.0 go test -race ./...`.
При sandbox-ограничении cache задать GOCACHE внутри отдельного temp-каталога.
Sonic на Go 1.27 предупреждает о fallback к encoding/json; warning не скрывать.

Интеграционные тесты принимают только явно заданный `TRADE_REPORT_TEST_DSN`
с host `127.0.0.1`, database `trade_report_test`. Это одноразовая локальная БД,
никогда production. Не вставлять реальные пароли в командную строку/документы.

```sh
go test -p 1 ./internal/modules/repository/pg ./internal/modules/runner_old/sessions -run 'Test(ResearchSnapshotPostgresCompatibility|CaptureOnOffTradeParity)' -count=1 -v
go test ./internal/modules/repository/pg -run '^$' -bench BenchmarkResearchSnapshotInsert -benchmem -count=5
```

Первый тест проверяет старый INSERT, SQL NULL, сохранность снимка при Update/Close.
Второй — реальный session/SQL и fake HTTP, одинаковые вызовы входа/SL/TP и
аварийного закрытия при capture on/off. Настоящих ордеров тест не отправляет.
Benchmark сравнивает NULL, 4KiB и near-limit запись. Это локальная оценка
накладных расходов, не гарантия нулевого влияния в production. SKIP не равен PASS.

## Gate перед production

1. Проверить архивные исполнения/учёт ARB отдельно. Не считать capture его исправлением.
2. Выполнить локальные PostgreSQL/parity-тесты и сохранить результат benchmark.
3. Получить отдельное согласование backup и additive-миграции; проверить старый бинарник.
4. Согласовать deployment, protocol ID и контрольную малую выборку; capture сначала off.
5. После включения проверить размер/INSERT latency, counts incomplete/failed и содержимое
   безопасного снимка. При проблемах выключить flag; snapshot не удалять.
6. Откат приложения — старый бинарник, сохранённая новая колонка. `goose Down`
   удаляет данные и не является процедурой обычного rollback.
7. Collector, read-only ключ/роль, приватный volume, 28-дневный период и replay v2
   требуют отдельного следующего этапа. Не подавать snapshot в legacy replay v1.
