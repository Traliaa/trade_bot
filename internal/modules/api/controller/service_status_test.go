package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"trade_bot/internal/modules/api/auth"
	"trade_bot/internal/modules/telegram_public/public"
)

type healthWarmup bool

func (s healthWarmup) IsWarmupDone() bool { return bool(s) }

type healthMarket time.Time

func (s healthMarket) MarketDataUpdatedAt() time.Time { return time.Time(s) }

type healthDatabase struct {
	err      error
	deadline bool
}

func (s *healthDatabase) Ping(ctx context.Context) error {
	_, s.deadline = ctx.Deadline()
	return s.err
}

func TestServiceStatusRequiresAuthentication(t *testing.T) {
	w := httptest.NewRecorder()
	// Nil dependencies ensure unauthenticated requests cannot trigger any probes.
	(&ServiceStatusController{}).Get(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestServiceStatusHealthStates(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name         string
		done         bool
		market       time.Time
		dbErr        error
		startupError bool
		want         string
	}{
		{name: "preparing", market: now, want: "preparing"},
		{name: "ready", done: true, market: now, want: "ready"},
		{name: "missing data", done: true, want: "degraded"},
		{name: "stale data", done: true, market: now.Add(-2 * time.Minute), want: "degraded"},
		{name: "future data", done: true, market: now.Add(time.Minute), want: "degraded"},
		{name: "database down", done: true, market: now, dbErr: errors.New("private database address"), want: "degraded"},
		{name: "startup failed", startupError: true, market: now, want: "degraded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := public.NewService(nil, nil)
			state := public.StatePreparing
			if tc.startupError {
				state = public.StateError
			}
			_ = status.SendOrEdit(context.Background(), public.Status{State: state, Progress: 42, Instruments: 77, ErrorHint: "private startup detail"})
			database := &healthDatabase{err: tc.dbErr}
			c := &ServiceStatusController{status: status, warmup: healthWarmup(tc.done), market: healthMarket(tc.market), database: database, startedAt: now, version: "test"}
			req := httptest.NewRequest("GET", "/", nil)
			req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey{}, int64(123)))
			w := httptest.NewRecorder()
			c.Get(w, req)
			var response serviceStatusResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || response.State != tc.want || !database.deadline || response.CheckedAt.IsZero() || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d response=%+v deadline=%v", w.Code, response, database.deadline)
			}
			if strings.Contains(w.Body.String(), "private") {
				t.Fatal("health endpoint leaked diagnostics")
			}
			if tc.market.IsZero() && response.MarketUpdatedAt != nil {
				t.Fatal("absent market timestamp must be null")
			}
		})
	}
}

func TestReadyReturns503WithoutLeakingDetails(t *testing.T) {
	for _, done := range []bool{true, false} {
		c := &ServiceStatusController{status: public.NewService(nil, nil), warmup: healthWarmup(done), market: healthMarket(time.Now()), database: &healthDatabase{}}
		w := httptest.NewRecorder()
		c.Ready(w, httptest.NewRequest("GET", "/ready", nil))
		want := 503
		if done {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("status=%d want=%d", w.Code, want)
		}
		if strings.Contains(w.Body.String(), "instruments") || strings.Contains(w.Body.String(), "version") {
			t.Fatal("readiness leaked internal metadata")
		}
	}
}
