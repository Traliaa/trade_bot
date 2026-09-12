package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"trade_bot/internal/modules/api/auth"
	"trade_bot/internal/modules/api/controller"

	"github.com/go-chi/chi/v5"
)

func TestRegisteredAdminRoutesRejectOrdinaryUsers(t *testing.T) {
	t.Setenv("ADMIN_TELEGRAM_USER_ID", "123")
	secret := []byte("test-only-secret")
	router := chi.NewRouter()
	registerRoutes(Params{Router: router}, &controller.TgSessionController{}, controller.NewMeController(), secret, controller.NewHealthController(), &controller.TradeController{}, &controller.ServiceStatusController{})
	token, err := auth.SignHS256(secret, auth.Claims{TgUserID: 124, Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/strategy/runtime"}, {"GET", "/api/strategy/rejects?reset=true"},
		{"GET", "/api/strategy/execution"},
		{"GET", "/api/strategy/tune/mode"}, {"POST", "/api/strategy/tune/auto"}, {"POST", "/api/strategy/tune/toggle"},
	} {
		for _, authorized := range []bool{false, true} {
			req := httptest.NewRequest(route.method, route.path, nil)
			want := http.StatusUnauthorized
			if authorized {
				req.Header.Set("Authorization", "Bearer "+token)
				want = http.StatusForbidden
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != want {
				t.Fatalf("%s %s: status=%d want=%d", route.method, route.path, w.Code, want)
			}
		}
	}
	for _, id := range []int64{123, 124} {
		token, _ := auth.SignHS256(secret, auth.Claims{TgUserID: id, Exp: time.Now().Add(time.Hour).Unix()})
		req := httptest.NewRequest("GET", "/api/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var profile struct {
			IsAdmin bool `json:"is_admin"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil {
			t.Fatal(err)
		}
		if profile.IsAdmin != (id == 123) {
			t.Fatalf("id=%d profile=%+v", id, profile)
		}
	}
}
