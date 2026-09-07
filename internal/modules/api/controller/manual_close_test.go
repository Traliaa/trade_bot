package controller

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/api/auth"
)

type manualRouterFake struct {
	TradeRouter
	calls  int
	userID int64
	err    error
}

func (f *manualRouterFake) ManualCloseTrade(_ context.Context, user int64, guid, id uuid.UUID, fraction float64) (models.ManualClose, error) {
	f.calls++
	f.userID = user
	return models.ManualClose{RequestID: id, TradeGUID: guid, Fraction: fraction, Status: "accepted"}, f.err
}
func TestManualCloseHTTPContract(t *testing.T) {
	guid := uuid.New().String()
	id := uuid.New().String()
	for _, tc := range []struct {
		name, body string
		authed     bool
		err        error
		code       int
	}{
		{"unauthenticated", `{}`, false, nil, 401},
		{"bad fraction", `{"request_id":"` + id + `","fraction":2}`, true, nil, 400},
		{"missing id", `{"fraction":0.5}`, true, nil, 400},
		{"accepted", `{"request_id":"` + id + `","fraction":0.5,"user_id":999}`, true, nil, 200},
		{"foreign trade", `{"request_id":"` + id + `","fraction":1}`, true, models.ErrCloseNotFound, 404},
		{"conflict", `{"request_id":"` + id + `","fraction":1}`, true, models.ErrCloseConflict, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &manualRouterFake{err: tc.err}
			c := &TradeController{r: f}
			router := chi.NewRouter()
			router.Post("/trades/{guid}/close", c.ManualClose)
			req := httptest.NewRequest(http.MethodPost, "/trades/"+guid+"/close", strings.NewReader(tc.body))
			if tc.authed {
				req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey{}, int64(123)))
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.code {
				t.Fatalf("status %d want %d", w.Code, tc.code)
			}
			if tc.code == 400 || tc.code == 401 {
				if f.calls != 0 {
					t.Fatal("invalid request reached service")
				}
			} else if f.userID != 123 {
				t.Fatal("client-controlled identity")
			}
		})
	}
}
