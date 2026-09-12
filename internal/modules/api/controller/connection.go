package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"trade_bot/internal/models"
)

type connectionStatus struct {
	Configured bool `json:"configured"`
}

func publicSettings(user models.UserSettings) settingResponse {
	ts := &user.Settings.TradingSettings
	configured := ts.OKXAPIKey != "" && ts.OKXAPISecret != "" && ts.OKXPassphrase != ""
	ts.OKXAPIKey, ts.OKXAPISecret, ts.OKXPassphrase = "", "", ""
	return settingResponse{Setting: user, Connection: connectionStatus{Configured: configured}}
}

// SaveOKXKeys replaces one complete credential set and never returns secrets.
func (c *TradeController) SaveOKXKeys(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var req struct {
		APIKey     string `json:"api_key"`
		APISecret  string `json:"api_secret"`
		Passphrase string `json:"passphrase"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid credential payload", http.StatusBadRequest)
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid credential payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.APIKey) == "" || strings.TrimSpace(req.APISecret) == "" || strings.TrimSpace(req.Passphrase) == "" {
		http.Error(w, "all three credentials are required", http.StatusBadRequest)
		return
	}
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()
	user, err := c.r.GetUser(r.Context(), userID)
	if err != nil || user == nil {
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return
	}
	updated := *user
	updated.Settings.TradingSettings.OKXAPIKey = req.APIKey
	updated.Settings.TradingSettings.OKXAPISecret = req.APISecret
	updated.Settings.TradingSettings.OKXPassphrase = req.Passphrase
	if err := c.r.ApplySettings(r.Context(), &updated); err != nil {
		http.Error(w, "could not save credentials", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
