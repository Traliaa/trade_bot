package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"trade_bot/internal/modules/api/auth"
)

func TestDevSessionFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		env, enabled, user string
		want               int
	}{
		{"", "", "", http.StatusNotFound},
		{"production", "true", "123", http.StatusNotFound},
		{"dev", "", "123", http.StatusNotFound},
		{"local", "true", "", http.StatusServiceUnavailable},
		{"local", "true", "-1", http.StatusServiceUnavailable},
		{"local", "true", "123", http.StatusOK},
	} {
		t.Run(tc.env+"/"+tc.enabled+"/"+tc.user, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.env)
			t.Setenv("ENABLE_DEV_AUTH", tc.enabled)
			t.Setenv("DEV_TELEGRAM_USER_ID", tc.user)
			w := httptest.NewRecorder()
			secret := []byte("test-only-secret")
			(&TgSessionController{JWTSecret: secret}).CreateDevSession(w, httptest.NewRequest("POST", "/api/dev/session", nil))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
			if tc.want == http.StatusOK {
				var response tgSessionResp
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				claims, err := auth.VerifyHS256(secret, response.Token)
				if err != nil || claims.TgUserID != 123 {
					t.Fatalf("claims=%+v err=%v", claims, err)
				}
			}
		})
	}
}
