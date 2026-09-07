package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/api/auth"
)

type settingsRouterStub struct {
	TradeRouter
	user    *models.UserSettings
	saved   *models.UserSettings
	saveErr error
	getErr  error
}

func (s *settingsRouterStub) GetUser(context.Context, int64) (*models.UserSettings, error) {
	return s.user, s.getErr
}
func (s *settingsRouterStub) ApplySettings(_ context.Context, u *models.UserSettings) error {
	s.saved = u
	return s.saveErr
}

func TestApplySettingsContract(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		saveErr    error
		status     int
	}{
		{"valid", `{"user":{"telegram_id":999,"Premium":false,"settings":{"TradingSettings":{"leverage":3,"confirm_timeout":60000000000},"TrailingConfig":{"BETriggerR":0.8}}}}`, nil, 204},
		{"old malformed envelope", `{"user":{"TradingSettings":{"leverage":3}}}`, nil, 400},
		{"missing settings", `{"user":{}}`, nil, 400},
		{"storage failure", `{"user":{"settings":{}}}`, errors.New("db unavailable"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := &models.UserSettings{TelegramID: 123, Name: "original", Premium: true, Status: true}
			stub := &settingsRouterStub{user: original, saveErr: tc.saveErr}
			req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(tc.body))
			req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey{}, int64(123)))
			w := httptest.NewRecorder()
			(&TradeController{r: stub}).ApplySettings(w, req)
			if w.Code != tc.status {
				t.Fatalf("status = %d; want %d", w.Code, tc.status)
			}
			if tc.status == 204 {
				if stub.saved.TelegramID != 123 || !stub.saved.Premium || !stub.saved.Status || stub.saved.Name != "original" {
					t.Fatal("server-owned fields overwritten")
				}
				if stub.saved.Settings.TradingSettings.Leverage != 3 || stub.saved.Settings.TrailingConfig.BETriggerR != .8 {
					t.Fatal("settings not decoded")
				}
				if original.Settings.TradingSettings.Leverage != 0 {
					t.Fatal("mutated shared user")
				}
			}
			if tc.status == 400 && stub.saved != nil {
				t.Fatal("invalid payload was saved")
			}
		})
	}
}

func TestEnableUserStopsOnLookupFailure(t *testing.T) {
	for _, stub := range []*settingsRouterStub{{getErr: errors.New("db unavailable")}, {}} {
		req := httptest.NewRequest(http.MethodPost, "/api/bot/enable", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey{}, int64(123)))
		w := httptest.NewRecorder()
		(&TradeController{r: stub}).EnableUser(w, req)
		if w.Code != 500 && w.Code != 404 {
			t.Fatalf("unexpected status %d", w.Code)
		}
	}
}
