package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/api/auth"
)

type reconciliationRouterStub struct {
	TradeRouter
	err error
}

func (s reconciliationRouterStub) GetTradeStats(_ context.Context, userID int64) (models.TradeStats, error) {
	if userID != 123 {
		return models.TradeStats{}, errors.New("wrong account")
	}
	return models.TradeStats{ClosedTrades: 2, Reconciliation: &models.TradeReconciliationReport{Scope: "stored_fills", FundingStatus: "not_checked", CheckedAt: time.Now().UTC(), Verified: 1, Incomplete: 1, Trades: []models.TradeReconciliation{}}}, s.err
}

func TestReconciliationStatsHTTPContract(t *testing.T) {
	for _, tc := range []struct {
		name       string
		authorized bool
		err        error
		status     int
	}{
		{"success", true, nil, 200}, {"unauthorized", false, nil, 401}, {"unavailable", true, errors.New("database unavailable"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stats?user_id=999", nil)
			if tc.authorized {
				req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey{}, int64(123)))
			}
			w := httptest.NewRecorder()
			(&TradeController{r: reconciliationRouterStub{err: tc.err}}).TradeStats(w, req)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			if tc.status == 200 {
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("personal reconciliation can be cached")
				}
				var body statsResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Stats.Reconciliation == nil || body.Stats.Reconciliation.FundingStatus != "not_checked" || body.Stats.Reconciliation.Verified != 1 || body.Stats.ClosedTrades != 2 {
					t.Fatalf("lost reconciliation contract: %s", w.Body)
				}
			}
		})
	}
}
