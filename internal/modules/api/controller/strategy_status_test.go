package controller

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/api/auth"
)

type strategyStatusStub struct {
	TradeRouter
	changed bool
}

func (s strategyStatusStub) AutoTuneNow(context.Context) (models.TuneDecision, models.RuntimeTuning, time.Time, time.Time, bool, models.TuneMode) {
	return models.TuneDecision{Changed: s.changed, Why: models.TuneWhyCooldown}, models.RuntimeTuning{}, time.Now(), time.Now(), !s.changed, models.TuneManual
}
func (s strategyStatusStub) GetUserStatus(context.Context, int64) (models.UserStatus, error) {
	return models.UserStatus{LastSignal: &models.SignalSnapshot{Symbol: "ETH-USDT-SWAP", Price: 2000, CreatedAt: time.Now()}}, nil
}
func TestTuneChangedIsNotWarmupFlag(t *testing.T) {
	for _, changed := range []bool{false, true} {
		w := httptest.NewRecorder()
		(&TradeController{r: strategyStatusStub{changed: changed}}).AutoTuneNow(w, httptest.NewRequest("POST", "/", nil))
		var response autoTuneResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Changed != changed {
			t.Fatalf("changed=%v want %v", response.Changed, changed)
		}
	}
}
func TestStatusReturnsSignalWithoutTrades(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey{}, int64(123)))
	w := httptest.NewRecorder()
	(&TradeController{r: strategyStatusStub{}}).StatusForUser(w, req)
	var response statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.LastSignal == nil || response.LastSignal.Symbol != "ETH-USDT-SWAP" || len(response.OpenTrades) != 0 {
		t.Fatalf("unexpected status: %+v", response)
	}
}
