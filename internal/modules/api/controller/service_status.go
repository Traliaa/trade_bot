package controller

import (
	"context"
	"net/http"
	"runtime/debug"
	"time"
	"trade_bot/internal/modules/config"
	okxws "trade_bot/internal/modules/okx_websocket/service"
	strategy "trade_bot/internal/modules/strategy/service"
	"trade_bot/internal/modules/telegram_public/public"
	"trade_bot/pkg/db"
)

type ServiceStatusController struct {
	status interface {
		Snapshot() (public.Status, []public.Status)
	}
	warmup    interface{ IsWarmupDone() bool }
	market    interface{ MarketDataUpdatedAt() time.Time }
	database  interface{ Ping(context.Context) error }
	startedAt time.Time
	version   string
	cfg       *config.Config
}

func NewServiceStatusController(status *public.Service, warmup *strategy.Service, market *okxws.Service, database *db.PgTxManager, cfg *config.Config) *ServiceStatusController {
	version := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				version = setting.Value
			}
		}
	}
	return &ServiceStatusController{status: status, warmup: warmup, market: market, database: database, startedAt: time.Now(), version: version, cfg: cfg}
}

type serviceEvent struct {
	State public.State `json:"state"`
	At    time.Time    `json:"at"`
}
type serviceStatusResponse struct {
	JournalPersistent bool                    `json:"journal_persistent"`
	Constraints       *strategyConstraints    `json:"constraints,omitempty"`
	Universe          *okxws.UniverseSnapshot `json:"universe,omitempty"`
	State             string                  `json:"state"`
	CheckedAt         time.Time               `json:"checked_at"`
	StartedAt         time.Time               `json:"started_at"`
	Version           string                  `json:"version"`
	WarmupDone        bool                    `json:"warmup_done"`
	Progress          int                     `json:"progress"`
	Instruments       int                     `json:"instruments"`
	MarketUpdatedAt   *time.Time              `json:"market_updated_at"`
	MarketFresh       bool                    `json:"market_fresh"`
	DatabaseOK        bool                    `json:"database_ok"`
	Events            []serviceEvent          `json:"events"`
}

func (c *ServiceStatusController) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := mustAuthUserID(w, r); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
	defer cancel()
	databaseOK := c.database.Ping(ctx) == nil
	status, events := c.status.Snapshot()
	now := time.Now()
	marketAt := c.market.MarketDataUpdatedAt()
	fresh := !marketAt.IsZero() && !marketAt.After(now) && now.Sub(marketAt) <= 90*time.Second
	done := c.warmup.IsWarmupDone()
	state := "preparing"
	if done {
		state = "ready"
	}
	if status.State == public.StateError || !databaseOK || done && !fresh {
		state = "degraded"
	}
	response := serviceStatusResponse{State: state, CheckedAt: now, StartedAt: c.startedAt, Version: c.version, WarmupDone: done, Progress: status.Progress, Instruments: status.Instruments, MarketFresh: fresh, DatabaseOK: databaseOK, Events: []serviceEvent{}}
	if history, ok := c.status.(interface {
		History(context.Context) ([]public.Status, bool)
	}); ok && databaseOK {
		events, response.JournalPersistent = history.History(ctx)
	}
	if c.cfg != nil {
		response.Constraints = constraintsFromConfig(c.cfg)
	}
	if market, ok := c.market.(interface {
		UniverseStatus(time.Time) okxws.UniverseSnapshot
	}); ok {
		universe := market.UniverseStatus(now)
		response.Universe = &universe
		if universe.RefreshError != "" || done && (universe.SelectedCount == 0 || universe.FreshCount != universe.SelectedCount) {
			response.State = "degraded"
		}
	}
	if !marketAt.IsZero() {
		response.MarketUpdatedAt = &marketAt
	}
	for _, event := range events {
		response.Events = append(response.Events, serviceEvent{State: event.State, At: event.UpdatedAt})
	}
	writeJSON(w, response)
}

type strategyConstraints struct {
	Strategy         string  `json:"strategy"`
	LTF              string  `json:"ltf"`
	HTF              string  `json:"htf"`
	MaxRiskPct       float64 `json:"max_risk_pct"`
	MaxOpenPositions int     `json:"max_open_positions"`
	AllowShorts      bool    `json:"allow_shorts"`
}

func constraintsFromConfig(cfg *config.Config) *strategyConstraints {
	return &strategyConstraints{Strategy: cfg.Strategy.Name, LTF: cfg.Strategy.LTF, HTF: cfg.Strategy.HTF, MaxRiskPct: cfg.Strategy.V3.MaxRiskPct, MaxOpenPositions: cfg.Strategy.V3.MaxOpenPositions, AllowShorts: cfg.Strategy.V3.AllowShorts}
}

// Ready reveals no account data and can be probed by an external uptime monitor.
func (c *ServiceStatusController) Ready(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
	defer cancel()
	now := time.Now()
	at := c.market.MarketDataUpdatedAt()
	status, _ := c.status.Snapshot()
	ready := c.database.Ping(ctx) == nil && c.warmup.IsWarmupDone() && status.State != public.StateError && !at.IsZero() && !at.After(now) && now.Sub(at) <= 90*time.Second
	if market, ok := c.market.(interface {
		UniverseStatus(time.Time) okxws.UniverseSnapshot
	}); ok {
		universe := market.UniverseStatus(now)
		ready = ready && universe.RefreshError == "" && universe.SelectedCount > 0 && universe.FreshCount == universe.SelectedCount
	}
	if !ready {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
