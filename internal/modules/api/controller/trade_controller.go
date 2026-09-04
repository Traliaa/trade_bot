package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
	"trade_bot/internal/modules/runner_old/sessions"

	"trade_bot/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TradeRouter interface {
	DisableUser(ctx context.Context, userID int64) bool
	EnableUser(ctx context.Context, user *models.UserSettings) (*sessions.UserSession, bool)
	ApplySettings(ctx context.Context, user *models.UserSettings)

	GetUserStatus(ctx context.Context, userID int64) (models.UserStatus, error)
	GetUser(ctx context.Context, userID int64) (*models.UserSettings, error)
	StatusForUser(ctx context.Context, userID int64) ([]models.OpenPosition, error)

	AutoTuneNow(ctx context.Context) (models.TuneDecision, models.RuntimeTuning, time.Time, time.Time, bool, models.TuneMode)
	ToggleTuneMode(ctx context.Context) models.TuneMode
	TuneMode(ctx context.Context) models.TuneMode
	StrategyRejects(reset bool) models.RejectSnapshot
	StrategyTuning() (models.RuntimeTuning, time.Time, time.Time)
	GetSession(userID int64) (*sessions.UserSession, bool)
	ListRecentTrades(ctx context.Context, userID int64, limit int) ([]models.TradeRecord, error)
	ListTradeFills(ctx context.Context, userID int64, guid uuid.UUID) ([]models.TradeFillRecord, error)
	GetTradeStats(ctx context.Context, userID int64) (models.TradeStats, error)
	ListOpenTrades(ctx context.Context, userID int64) ([]models.TradeRecord, error)
}

func (c *TradeController) SetRouter(r TradeRouter) {
	c.r = r
}

type TradeController struct {
	r TradeRouter
}

func NewTradeController() *TradeController {
	return &TradeController{}
}

type applySettingsRequest struct {
	User models.UserSettings `json:"user"`
}

type settingResponse struct {
	Setting models.UserSettings `json:"setting"`
}

type autoTuneResponse struct {
	Decision models.TuneDecision  `json:"decision"`
	Runtime  models.RuntimeTuning `json:"runtime"`
	From     time.Time            `json:"from"`
	To       time.Time            `json:"to"`
	Changed  bool                 `json:"changed"`
	Mode     models.TuneMode      `json:"mode"`
}

type tradesResponse struct {
	Trades []models.TradeRecord `json:"trades"`
}

type statsResponse struct {
	Stats models.TradeStats `json:"stats"`
}

type tradeFillsResponse struct {
	Fills []models.TradeFillRecord `json:"fills"`
}

type positionsResponse struct {
	Positions []models.OpenPosition `json:"positions"`
}

func (c *TradeController) DisableUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}

	c.r.DisableUser(r.Context(), userID)
	w.WriteHeader(http.StatusNoContent)
}

func (c *TradeController) EnableUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}

	resp, err := c.r.GetUser(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

	}
	_, _ = c.r.EnableUser(r.Context(), resp)
	w.WriteHeader(http.StatusNoContent)
}

func (c *TradeController) ApplySettings(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}

	var req applySettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	// user id не доверяем клиенту
	req.User.TelegramID = userID

	c.r.ApplySettings(r.Context(), &req.User)
	w.WriteHeader(http.StatusNoContent)
}

type statusResponse struct {
	BotRunning bool                   `json:"bot_running"`
	Account    models.AccountSnapshot `json:"account"`
	OpenTrades []models.TradeRecord   `json:"open_trades"`
}

func (c *TradeController) StatusForUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}

	status, err := c.r.GetUserStatus(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, statusResponse{
		BotRunning: status.BotRunning,
		Account:    status.Account,
		OpenTrades: status.OpenTrades,
	})
}

func (c *TradeController) GetSetting(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}

	session, ok := c.r.GetSession(userID)
	if ok && session != nil && session.User != nil {
		writeJSON(w, settingResponse{Setting: *session.User})
		return
	}

	user, err := c.r.GetUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "settings not found", http.StatusNotFound)
		return
	}

	writeJSON(w, settingResponse{Setting: *user})
}

func (c *TradeController) AutoTuneNow(w http.ResponseWriter, r *http.Request) {
	decision, runtime, from, to, changed, mode := c.r.AutoTuneNow(r.Context())
	writeJSON(w, autoTuneResponse{
		Decision: decision,
		Runtime:  runtime,
		From:     from,
		To:       to,
		Changed:  changed,
		Mode:     mode,
	})
}

func (c *TradeController) ToggleTuneMode(w http.ResponseWriter, r *http.Request) {
	mode := c.r.ToggleTuneMode(r.Context())
	writeJSON(w, map[string]any{"mode": mode})
}

func (c *TradeController) TuneMode(w http.ResponseWriter, r *http.Request) {
	mode := c.r.TuneMode(r.Context())
	writeJSON(w, map[string]any{"mode": mode})
}

func (c *TradeController) StrategyRejects(w http.ResponseWriter, r *http.Request) {
	reset := r.URL.Query().Get("reset") == "1" || r.URL.Query().Get("reset") == "true"
	snap := c.r.StrategyRejects(reset)
	writeJSON(w, snap)
}

func (c *TradeController) StrategyTuning(w http.ResponseWriter, r *http.Request) {
	runtime, from, to := c.r.StrategyTuning()
	writeJSON(w, map[string]any{
		"runtime": runtime,
		"from":    from,
		"to":      to,
	})
}

func (c *TradeController) RecentTrades(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}

	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := parsePositiveInt(s); err == nil && n > 0 {
			limit = n
		}
	}

	trades, err := c.r.ListRecentTrades(r.Context(), userID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, tradesResponse{Trades: trades})
}

func (c *TradeController) TradeFills(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}

	guid, err := uuid.Parse(chi.URLParam(r, "guid"))
	if err != nil {
		http.Error(w, "invalid trade guid", http.StatusBadRequest)
		return
	}

	fills, err := c.r.ListTradeFills(r.Context(), userID, guid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, tradeFillsResponse{Fills: fills})
}
func (c *TradeController) OpenTrades(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}

	trades, err := c.r.ListOpenTrades(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, tradesResponse{Trades: trades})
}

func (c *TradeController) Positions(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}

	positions, err := c.r.StatusForUser(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, positionsResponse{Positions: positions})
}

func (c *TradeController) TradeStats(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}

	stats, err := c.r.GetTradeStats(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, statsResponse{Stats: stats})
}
