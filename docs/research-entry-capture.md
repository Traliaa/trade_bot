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

Существующая торговая `FillTime` берётся из OKX `ts` (время записи), и capture
не меняет эту семантику. Настоящее время исполнения из `fillTime` сохраняется
отдельно только для исследования; `FirstFillAt`/`LastFillAt` используют его.
Источник времени входа обозначается `exchange_record`. Отсутствующий или
невалидный `fillTime` даёт `fill_time_missing`, отличие последнего исполнения
от торгового времени входа — `entry_time_not_execution`; оба случая incomplete.
Источник различия полей: [OKX transaction details](https://www.okx.com/docs-v5/en/#order-book-trading-trade-get-transaction-details-last-3-days).

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

### Результат локальной проверки 7 октября 2026

После разрешения пользователя запущен отдельный одноразовый PostgreSQL 17 в
Docker Desktop: только loopback, данные в tmpfs, синтетические fixtures.
Production, существующие контейнеры и реальные биржевые ордера не использовались.

- `TestResearchSnapshotPostgresCompatibility` — PASS с реальной БД, не SKIP.
- `TestCaptureOnOffTradeParity` — PASS для успешной защиты и отказа SL;
  биржевые HTTP-запросы перехвачены тестом, последовательность on/off совпала.
- `go test -p 1 -race ./... -count=1` с локальным DSN — PASS, 18 пакетов с
  тестами. Внешний opt-in тест публичных свечей OKX не включался; PostgreSQL
  тесты включены. Sonic сообщил о fallback к encoding/json на Go 1.27.
- Дополнительная ручная проверка реальных Up-миграций 0003 → fixture → 0008
  в отдельной одноразовой БД: старая строка и payload сохранены, новая колонка
  SQL NULL. Down не выполнялся.

`BenchmarkResearchSnapshotInsert -benchmem -count=5`, Apple M3 Pro / Go 1.27:

| JSON, байт | INSERT, мс/операцию (min–max) | Медиана, мс | B/op | allocs/op |
| ---: | ---: | ---: | ---: | ---: |
| NULL / 0 | 0.676–0.802 | 0.749 | 4236–4250 | 44 |
| 4107 | 0.778–1.070 | 1.067 | 19392–19397 | 49 |
| 32751 | 1.250–1.877 | 1.380 | 119640–119670 | 49 |

Это синтетический, хорошо сжимаемый JSON, не настоящие снимки; tmpfs и Docker
не моделируют production-диск/сеть. Измерение включает создание fixture UUID,
payload и вызов repository INSERT, исключает DELETE; builder в него не входит.
Разница медиан с NULL — около +0.318 и +0.632 мс, но это не допустимый бюджет
для production: порог и контроль p95 требуют отдельного rollout-согласования.

Отложенное небольшое замечание review: автоматический compatibility-тест
нужно дополнить fixture до миграции и сравнением семантики входного JSON
с сохранённым. Ручная проверка старой строки не заменяет этот regression-тест.

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
