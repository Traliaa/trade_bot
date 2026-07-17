# Profit Improvements for trade_bot (V3 Donchian)

**Date:** 2026-07-17
**Status:** Approved design
**Context:** Стратегия donchian_v3_smart показывает negative total R (-34.36) и PF 0.82 за 500 сделок.
Основные проблемы: 206 SL vs 124 TP (1.66:1) при win rate 50.8%.

## 1. Цель

Привести PF > 1.0 и total R в положительную зону за счёт:
- Улучшения выходов (trailing stops, risk control)
- Улучшения качества входов (weighted scoring)

## 2. Изменения в выходах

### 2.1 Новые дефолты трейлинга

Файл: `configs/values_local.yaml` (default_trailing секция)

| Параметр | Было | Стало | Эффект |
|----------|------|-------|--------|
| `be_trigger_r` | 0.8 | **0.45** | Ранний выход в безубыток — после 0.45R стоп уходит в entry |
| `lock_trigger_r` | 1.2 | **0.8** | Lock сразу после BE — фиксируем профит раньше |
| `lock_offset_r` | 0.5 | **0.25** | Не отпускаем профит: при 0.8R MFE фиксируется +0.55R |
| `partial_trigger_r` | 1.1 | **0.7** | Частичная фиксация 50% на 0.7R |
| `early_time_stop_bars` | 4 | **3** | Если за 45 мин нет 0.15R движения → выход |
| `time_stop_bars` | 12 | **8** | Stale stop через 2ч вместо 3ч |
| `default_stop_pct` | 3.0 | **2.0** | Каждый SL на 1% меньше |
| `default_risk_pct` | 0.5 | **0.4** | Риск на сделку 0.4% |
| `default_take_profit_rr` | 2.0 | **1.5** | TP ближе |

### 2.2 Фикс V3 partial close

Файл: `internal/modules/strategy/service/service_v3.go`, метод `manageOpenPositionV3`

**Проблема:** На строках 829 и 862 есть `// TODO: вызвать частичное закрытие позиции`. V3-стратегия отмечает `PartialDone`,
но не вызывает фактическое частичное закрытие на OKX. Позиция получает BE через отметку в StrategyState, но runner_old
не всегда подхватывает это состояние.

**Решение:** После установки `PartialDone` и `TrailingStop` в `manageOpenPositionV3` — не дублировать OKX-логику,
а сигнализировать runner_old через существующий канал TradePayload или через установку поля `v3st` так,
чтобы `runner_old` подхватил на следующей свече через существующий trail_one.go механизм.

**Решение:** Runner_old на каждом `on_candle_close` проверяет `StrategyState.PartialDone` через
поле `stateV3[instID].PartialDone`. Если V3 отметил partial, но runner_old ещё не обработал его
через `trail_one`, runner вызывает частичное закрытие через OKX клиент.

Конкретный механизм — в `runner_old/service/on_candle_close.go` добавить проверку:
```go
v3st := strategy.getStrategyStateLocked(instID)
if v3st.PartialDone && !trailState.TookPartial {
    // runner ещё не обработал partial от V3 — выполняем через OKX
    // устанавливаем Size = fullSize * partialCloseFrac
}
```
После выполнения partial runner устанавливает `trailState.TookPartial = true`, чтобы не дублировать.

## 3. Изменения в качестве входов

### 3.1 Weighted scoring (небинарная оценка)

Файл: `internal/modules/strategy/service/service_v3.go` — методы `buildLongScore` и `buildShortScore`

Текущая система: 5 бинарных проверок, каждая 0 или 1 очко. Max = 5, MinConfirm = 5.

Новая система:

| Проверка | Вес | Условия |
|----------|-----|---------|
| retestOK | 0-2 | 0 = no retest; 1 = wick-touch только; **2 = close/body overlap** (более сильная конфирмация) |
| strongClose | 0-2 | 0 = close в середине range; 1 = close в верхней/нижней трети; **2 = close у экстремума (top/bottom 10%)** |
| reclaimOK | 0-1 | 0 = не подтверждён; 1 = reclaim после retest |
| impulseOK | 0-2 | 0 = body < min; 1 = body > min; **2 = body > 2×min (сильный импульс)** |
| structureOK | 0-1 | 0 = структура сломана; 1 = цела |

**Max: 8 очков, MinConfirm: 5**

Ключевой эффект: слабый 5/5 (все еле-еле прошли) даст ~4 балла и будет отклонён.
Сильный 4/5 (retest с overlap + strong impulse + strong close) даст 6+ баллов и пройдёт.

**Изменения в коде `buildLongScore` / `buildShortScore`:**
- `boolScore(retestOK)` → `weightedRetest(retestOK, last, retestLevel)` — возвращает 0, 1 или 2
  - 0: нет retest
  - 1: wick-touch (старое условие `wickTouch`)
  - 2: close near + body cross (старое условие `closeNear || bodyCross`)
- `boolScore(strongClose)` → `weightedClose(closePos, closeUpMin)` — возвращает 0, 1 или 2
  - 0: closePos < closeUpMin для long, > closeDnMax для short
  - 1: closeUpMin ≤ closePos < closeUpMin+0.15 (верхняя треть)
  - 2: closePos ≥ closeUpMin+0.15 (экстремум)
- `boolScore(impulseOK)` → `weightedImpulse(bodyPct, impulseMin)` — возвращает 0, 1 или 2
  - 0: bodyPct < impulseMin
  - 1: impulseMin ≤ bodyPct < 2×impulseMin
  - 2: bodyPct ≥ 2×impulseMin
- `boolScore(reclaimOK)` и `boolScore(structureOK)` остаются 0/1

### 3.2 Volume confirmation

Новый параметр: `volume_min_ratio` в `strategy` секции config.

Новая reject причина: `RejectLowVolume`. Новый параметр `volume_min_ratio` со значением по умолчанию **0.7** (объём свечи не ниже 70% SMA(20)).

**Реализация:** в `buildLongScore` / `buildShortScore` добавить проверку:
- Вычисляем SMA объёма за последние 20 свечей
- Если объём текущей свечи < avg\_volume × volume\_min\_ratio → штраф -1 к score
- Если объём < avg\_volume × volume\_min\_ratio × 0.5 → reject с `RejectLowVolume`

Volume уже доступен в `CandleTick.Volume`, данные есть в LTF свечах.

### 3.3 Что НЕ меняется

- `minEdge` = 2 — остаётся
- `HTF bias` thresholds — остаются (0.4-0.6)
- `RetestTolerancePct` — не меняем
- `StrongCloseMin`/`Max` thresholds — не меняем
- `V3 autotune` path — не меняем

## 4. Рефакторинг конфигурации

Текущее состояние: параметры размазаны по 6+ местам, 4 независимых источника trailing-дефолтов,
ATR-поля с `mapstructure` тегом не читаются из YAML, stale-параметры не видны в конфиге.

### 4.1 Проблемы

1. **Приоритеты размыты:** YAML → ApplyV3Defaults → RuntimeTuning → Presets → код-дефолты.
   Невозможно понять, какое значение реально применяется, не прочитав весь код.
2. **Скрытые параметры:** 20 V3-параметров заданы ТОЛЬКО в `ApplyV3Defaults()`, не видны в values.yaml.
3. **4 источника trailing-дефолтов:** YAML (default_trailing), TrailingPresets (preset.go),
   struct-комментарии (user.go), хардкод-fallback (trailing_helpers.go) — значения везде разные.
4. **Stale-параметры вне YAML:** 8 stale-параметров определены только в `GetStaleConfig()` (user.go),
   не копируются при создании пользователя (`NewTradingSettingsFromDefaults` их пропускает).
5. **ATR-поля нечитаемы:** `ATRPeriod`, `ATRStopMult`, `UseATRGuard` имеют тег `mapstructure` вместо `yaml` —
   YAML-декодер их игнорирует. Даже если добавить в values.yaml — не распарсятся.
6. **Дубляж effectiveV3TuningLocked / effectiveV3Params:** Оба делают одно и то же
   (tune-override поверх config), но в разной форме.

### 4.2 Решение

#### 4.2.1 Один источник дефолтов — ApplyV3Defaults

- Все V3-параметры, которые могут быть изменены оператором, переносятся в values.yaml
   (секция `strategy.v3`). ApplyV3Defaults остаётся только как fallback.
- Убирается дубляж `effectiveV3TuningLocked` → остаётся только `effectiveV3Params`.

Новая секция YAML:
```yaml
strategy:
  v3:
    min_confirm_score: 5
    retest_tolerance_pct: 0.0015
    impulse_body_min_pct: 0.003
    compression_threshold_pct: 0.012
    strong_close_min: 0.70
    strong_close_max: 0.30
    volume_min_ratio: 0.7
    min_rr: 1.5
    sl_buffer_pct: 0.001
    swing_lookback_bars: 5
    target_lookback_bars: 20
    use_percent_fallback: false
    fallback_stop_pct: 0.01
    atr_period: 14
    atr_stop_mult: 0.8
    use_atr_guard: false
```

#### 4.2.2 Stale-секция в YAML

Добавить `stale` подсекцию в `default_trailing`:

```yaml
default_trailing:
  stale:
    after_bars: 16
    min_mfe_r: 0.35
    exit_profit_r: 0.25
    near_be_r: -0.03
    max_adverse_r: -0.65
    grace_bars: 6
    worse_by_r: 0.30
    tighten_to_be_r: 0.05
```

Инициализировать копирование stale-полей в `NewTradingSettingsFromDefaults`.

#### 4.2.3 Правка тегов ATR

`ATRPeriod`, `ATRStopMult`, `UseATRGuard` — заменить `mapstructure` на `yaml`.

#### 4.2.4 Чистка дубляжа effectiveV3TuningLocked

Удалить `effectiveV3TuningLocked` (используется только в `AutoTuneV3Now` для `before`/`after`).
`AutoTuneV3Now` получает before через `effectiveV3Params()`, модифицирует, сохраняет в `e.tune`.

### 4.3 Полный список файлов для изменения

| Файл | Изменения |
|------|-----------|
| `configs/values_local.yaml` | Новая секция `strategy.v3`, `default_trailing.stale`, обновлённые trailing defaults |
| `internal/modules/config/config.go` | `StrategyConfig` → добавить `V3Config` вложенную структуру, заменить `mapstructure` на `yaml`, новый параметр `VolumeMinRatio` |
| `internal/modules/strategy/service/service_v3.go` | Weighted scoring в `buildLongScore`/`buildShortScore`, volume check, убрать `effectiveV3TuningLocked`, упростить `effectiveV3Params` |
| `internal/modules/strategy/service/service_v3_helpers.go` | Новые helper-функции для weighted scoring (`weightedRetest`, `weightedClose`, `weightedImpulse`) |
| `internal/models/reject_reason.go` | Новая причина `RejectLowVolume` |
| `internal/models/user.go` | Копирование stale-полей в `NewTradingSettingsFromDefaults`, `GetStaleConfig` |
| `internal/models/tune.go` | RuntimeTuning может остаться для runtime override |
| `internal/modules/runner_old/sessions/trailing_helpers.go` | Убрать хардкод-fallback `offsetR = 0.10` в `calcBEPrice` |
| `internal/modules/runner_old/service/on_candle_close.go` | Синхронизация V3 partial close — проверка `v3st.PartialDone` |
| `docs/superpowers/specs/2026-07-17-profit-improvements-design.md` | Этот spec |

## 5. Verification

1. `go test -count=1 ./internal/models/...` — убедиться, что существующие тесты проходят
2. `go test -count=1 ./internal/modules/runner_old/sessions/...` — trailing helpers tests
3. `go build ./cmd/bot` — проверить компиляцию
4. После деплоя: проверить `/readyz`, `/healthz`, мониторинг rejects и trade stats
5. Через 1-2 дня: проверить соотношение SL/TP, total R, PF
