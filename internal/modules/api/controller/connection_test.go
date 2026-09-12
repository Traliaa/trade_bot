package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/api/auth"
	"trade_bot/internal/modules/runner_old/sessions"
)

func (s *settingsRouterStub) GetSession(int64) (*sessions.UserSession, bool) { return nil, false }
func userWithKeys() *models.UserSettings {
	return &models.UserSettings{TelegramID: 123, Settings: models.Settings{TradingSettings: models.TradingSettings{OKXAPIKey: "original-key", OKXAPISecret: "original-secret", OKXPassphrase: "original-pass", Leverage: 3}}}
}
func settingsRequest(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/", strings.NewReader(body))
	return r.WithContext(context.WithValue(r.Context(), auth.UserIDContextKey{}, int64(123)))
}
func TestSettingsNeverReturnKeys(t *testing.T) {
	user := userWithKeys()
	w := httptest.NewRecorder()
	(&TradeController{r: &settingsRouterStub{user: user}}).GetSetting(w, settingsRequest("GET", ""))
	if strings.Contains(w.Body.String(), "original-") {
		t.Fatal("credentials leaked")
	}
	var response settingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Connection.Configured || user.Settings.TradingSettings.OKXAPISecret != "original-secret" {
		t.Fatal("scrubbing modified shared settings")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("settings are cacheable")
	}
}
func TestNormalSettingsPreserveCredentials(t *testing.T) {
	stub := &settingsRouterStub{user: userWithKeys()}
	w := httptest.NewRecorder()
	(&TradeController{r: stub}).ApplySettings(w, settingsRequest("POST", `{"user":{"settings":{"TradingSettings":{"leverage":5,"okx_api_key":"stale-key","okx_api_secret":"","okx_passphrase":""}}}}`))
	if w.Code != 204 || stub.saved.Settings.TradingSettings.OKXAPIKey != "original-key" || stub.saved.Settings.TradingSettings.OKXAPISecret != "original-secret" || stub.saved.Settings.TradingSettings.Leverage != 5 {
		t.Fatal("settings replaced credentials")
	}
}
func TestCredentialReplacementRequiresCompleteSet(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"api_key":"new-key","api_secret":"new-secret","passphrase":"new-pass"}`, 204},
		{`{"api_key":"new-key"}`, 400}, {`{"api_key":"","api_secret":"new-secret","passphrase":"new-pass"}`, 400},
		{`{"api_key":"new-key","api_secret":"new-secret","passphrase":"new-pass","user_id":999}`, 400},
		{`{"api_key":"new-key","api_secret":"new-secret","passphrase":"new-pass"} {}`, 400},
	} {
		stub := &settingsRouterStub{user: userWithKeys()}
		w := httptest.NewRecorder()
		(&TradeController{r: stub}).SaveOKXKeys(w, settingsRequest("POST", tc.body))
		if w.Code != tc.status {
			t.Fatalf("status=%d want=%d", w.Code, tc.status)
		}
		if strings.Contains(w.Body.String(), "new-") {
			t.Fatal("credential response leaks input")
		}
		if tc.status == 400 && stub.saved != nil {
			t.Fatal("saved incomplete credential set")
		}
		if tc.status == 204 && (stub.saved.TelegramID != 123 || stub.saved.Settings.TradingSettings.Leverage != 3 || stub.saved.Settings.TradingSettings.OKXAPISecret != "new-secret") {
			t.Fatal("replacement modified unrelated settings")
		}
	}
	w := httptest.NewRecorder()
	(&TradeController{}).SaveOKXKeys(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 401 {
		t.Fatal("unauthenticated credential change allowed")
	}
}
