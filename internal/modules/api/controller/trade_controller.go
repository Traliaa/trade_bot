package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
	"trade_bot/internal/modules/runner/sessions"

	"trade_bot/internal/models"

	"github.com/go-chi/chi/v5"
)

type TradeRouter interface {
	DisableUser(ctx context.Context, userID int64) bool
	EnableUser(context.Context, *models.UserSettings) (*sessions.UserSession, bool)
	ApplySettings(ctx context.Context, user *models.UserSettings)
	StatusForUser(ctx context.Context, userID int64) ([]models.OpenPosition, error)
	GetSession(int64) (*sessions.UserSession, bool)
	AutoTuneNow(ctx context.Context) (models.TuneDecision, models.RuntimeTuning, time.Time, time.Time, bool, models.TuneMode)
	ToggleTuneMode(ctx context.Context) models.TuneMode
	TuneMode(ctx context.Context) models.TuneMode
	StrategyRejects(reset bool) models.RejectSnapshot
	StrategyTuning() (models.RuntimeTuning, time.Time, time.Time)
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

type enableUserRequest struct {
	User models.UserSettings `json:"user"`
}

type statusResponse struct {
	Positions []models.OpenPosition `json:"positions"`
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

func (c *TradeController) DisableUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustUserID(w, r)
	if !ok {
		return
	}
	c.r.DisableUser(r.Context(), userID)
	w.WriteHeader(http.StatusNoContent)
}

func (c *TradeController) EnableUser(w http.ResponseWriter, r *http.Request) {
	var req enableUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	c.r.EnableUser(r.Context(), &req.User)
	w.WriteHeader(http.StatusNoContent)
}

func (c *TradeController) ApplySettings(w http.ResponseWriter, r *http.Request) {
	var req applySettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	c.r.ApplySettings(r.Context(), &req.User)
	w.WriteHeader(http.StatusNoContent)
}

func (c *TradeController) StatusForUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustUserID(w, r)
	if !ok {
		return
	}
	positions, err := c.r.StatusForUser(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, statusResponse{Positions: positions})
}

func (c *TradeController) GetSetting(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustUserID(w, r)
	if !ok {
		return
	}

	session, ok := c.r.GetSession(userID)
	if !ok {
		http.Error(w, "Настройки не найдены, попробуй /start", http.StatusInternalServerError)
	}
	writeJSON(w, settingResponse{Setting: *session.User})
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

// helpers

func mustUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "bad user id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
