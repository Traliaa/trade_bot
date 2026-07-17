# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

A Go-based algorithmic trading bot for OKX exchange. Streams real-time candlestick data via OKX WebSocket, generates trading signals using Donchian channel breakout strategies (V2/V3), manages user positions with trailing stops/break-even/profit-locking, and provides Telegram-based control + REST API frontend.

## Key commands

```bash
go build ./cmd/bot              # Build the bot binary
go build ./cmd/sqlc             # Build the sqlc codegen tool
go test -count=1 -race ./...    # Run all tests with race detection
make test                       # Same alias
make lint                       # Run golangci-lint (installs if missing)
make generate-sql               # Regenerate sqlc Go code from .sql queries
make lint-sql                   # Lint SQL migrations with oh-my-pg-tool
```

One test file exists at `internal/models/user_test.go` — run it with:
```bash
go test -v -count=1 ./internal/models/...
```

Local dev: Docker for Postgres (`docker-compose up`), config from `configs/values_local.yaml` (path set via `CONFIG_FILE` env var).

## Architecture

### DI wiring (`cmd/bot/main.go`)

The app uses `go.uber.org/fx` for dependency injection. Modules register via `fx.Module()` and provide/consume services via `fx.Provide` + `fx.Invoke`. Two global channels carry signals between modules:
- `chan models.Signal` — strategy → runner (trade signals)
- `chan models.CandleTick` — OKX WS → runner (candle data)

### Module layout (`internal/modules/`)

Each module lives in its own subdirectory and exposes an `fx.Option` via `Module()`:

| Module | Purpose |
|--------|---------|
| `okx_websocket` | Connects to OKX WS API, streams real-time 1m candles, builds higher-TF candles (15m/1h), selects top-volatile universe |
| `okx_client` | REST client for OKX — place/cancel orders, TP/SL, position metadata |
| `strategy` | Donchian channel breakout (V3 "smart") — evaluates candle patterns, HTF bias, retests, compression and generates `Signal`. Strategy state per instrument in `V3MarketState`. |
| `runner_old` | Per-user session manager — receives signals, creates/closes positions on OKX, manages trailing stops, trade history, position guards. Each user has a `UserSession`. |
| `telegram_bot` | Telegram bot interface — settings, stats, manual trades, auto-tune, presets. UX via inline keyboards. |
| `telegram_public` | Public status channel — periodic heartbeat with position/balance summary sent to a dedicated Telegram chat. |
| `api` | REST API (chi router) — JWT auth, CORS. Endpoints: enable/disable bot, settings, positions, trades, stats, strategy tuning. |
| `httpserver` | Chi HTTP server with `/livez`, `/readyz`, `/healthz`. |
| `config` | YAML config loader with env overrides. |
| `postgres` | PGX pool + transaction manager. |
| `repository` | Data access — file-based user store (legacy) and PG-based repositories: `user_settings` (sqlc-generated), `trade_history`. |
| `bootstrap` | Warmup sequence (collect enough candles before trading), lifecycle logging. |

### Core data flow

```
OKX WebSocket → CandleTick (1m) → CandleAgg (15m/1h bars)
                                       ↓
                              Strategy Service (V3 Donchian)
                                       ↓
                                 Signal{instId, side, price, score}
                                       ↓
                              Runner (per-user sessions)
                                       ↓
                        OKX REST API (place/cancel orders, TP/SL)
                                       ↓
                          Position guard + trailing workers (goroutine per position)
```

### Data models (`internal/models/`)

- **Trade**: open/closed trades with close reasons (TP, SL, break-even, lock profit, time stop, manual, etc.)
- **Strategy**: `Signal`, `SignalScore`, `MarketContext`, `MarketBias`, `StrategyType` (emarsi, donchian, donchianV2, donchian_v3_smart)
- **Strategy V3**: `V3MarketState` (LTF/HTF candles per instrument), `StrategyState` (entry price, SL/TP, trailing stop, phase), `PendingSignal`, `PositionPhase` (fresh/working/stalled/recovery/invalidated)
- **Position**: `OpenPosition`, `PositionTrailState` (entry, SL, TP, MFE, algo ID, trailing flags), `PosKey`
- **Trailing**: `TrailingDecision`, `TrailingAction`, various trailing presets
- **User**: `UserSettings` per Telegram user with trading settings, API keys, feature flags (pro mode, charts, simulation, auto-recommend), account snapshot
- **Tune**: `RuntimeTuning` — runtime-adjustable strategy parameters (min channel %, body %, retest tolerance, compression threshold, etc.)
- **Session**: user session state (open positions, pending orders, warmup status)

### Position lifecycle (`internal/modules/runner_old/sessions/`)

Each position goes through phases managed by:
- `position_state.go` — phase transitions and decision making
- `trail_one.go` — per-position trailing logic (BE, lock profit, partial closes, time stops)
- `position_guard_worker.go` — periodic guard checks
- `position_cache_worker.go` — refresh position state from OKX
- `trailing_helpers.go` — trailing calculations (tested in `trailing_helpers_test.go`)
- `v3.go` — V3-specific position management
- `trade_history_worker.go` — persist closed trades to DB

### HTTP API routes (`internal/modules/api/module.go`)

Authenticated routes (JWT):
- `/api/bot/enable`, `/api/bot/disable` — toggle trading per user
- `/api/settings` — get/apply user settings
- `/api/status` — per-user trading status
- `/api/positions`, `/api/open_trades`, `/api/trades` — position/trade data
- `/api/stats` — trade statistics
- `/api/strategy/runtime`, `/api/strategy/rejects` — strategy diagnostics
- `/api/strategy/tune/auto`, `/api/strategy/tune/toggle`, `/api/strategy/tune/mode` — runtime autotuning

### OKX integration

- **WebSocket**: streams candles, order book, account updates. Universe selection via top-volatile screening.
- **REST client**: single-order placement, TP/SL via algo orders, cancellation, position/instrument metadata.

### Database (`migrations/`)

3 migrations: job table (for goose), active users, trade history. Postgres via `pgx/v5`.

### Technology stack

- **Go 1.25** with `go.uber.org/fx` DI framework
- **Chi** router (`github.com/go-chi/chi/v5`) for REST API
- **pgx/v5** for PostgreSQL access, **sqlc** for type-safe query generation
- **Gorilla WebSocket** for OKX WebSocket connection
- **go-telegram-bot-api** for Telegram bot interface
- **Viper** for config (`configs/values_*.yaml` + env overrides)
- **Zap** for structured logging
- **Jaeger** (OpenTracing) for distributed tracing
- **Sonic** for fast JSON encoding
- **goose** for DB migrations (via Dockerfile entrypoint)
