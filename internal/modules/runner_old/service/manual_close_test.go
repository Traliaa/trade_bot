package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"math"
	"testing"
	"trade_bot/internal/models"
)

type closeRepoFake struct {
	closeRepository
	trade models.TradeRecord
	saved *models.ManualClose
}

func (r *closeRepoFake) GetByGUID(context.Context, uuid.UUID) (*models.TradeRecord, error) {
	return &r.trade, nil
}
func (r *closeRepoFake) ClaimManualClose(_ context.Context, _ int64, m models.ManualClose) (models.ManualClose, bool, error) {
	if r.saved != nil {
		if r.saved.RequestID != m.RequestID || r.saved.Fraction != m.Fraction {
			return m, false, models.ErrCloseConflict
		}
		return *r.saved, false, nil
	}
	if r.trade.Status != models.TradeStatusOpen {
		return m, false, models.ErrCloseConflict
	}
	m.Status = "pending"
	r.saved = &m
	return m, true, nil
}
func (r *closeRepoFake) SaveManualClose(_ context.Context, m models.ManualClose) error {
	r.saved = &m
	return nil
}

type closeExchangeFake struct {
	closeExchange
	submits int
	size    float64
	side    string
	err     error
}

func (e *closeExchangeFake) ClosingPosition(context.Context, string, string) (float64, error) {
	return 8, nil
}
func (e *closeExchangeFake) GetInstrumentMeta(context.Context, string) (models.Instrument, error) {
	return models.Instrument{LotSz: 1, MinSz: 1}, nil
}
func (e *closeExchangeFake) SubmitManualClose(_ context.Context, _ string, side string, size float64, _ string) (string, error) {
	e.submits++
	e.size = size
	e.side = side
	return "order-1", e.err
}

func TestManualCloseUsesLiveSizeAndDoesNotResubmit(t *testing.T) {
	for _, side := range []string{"long", "short"} {
		for _, failed := range []bool{false, true} {
			t.Run(side+map[bool]string{true: " timeout", false: " accepted"}[failed], func(t *testing.T) {
				repo := &closeRepoFake{trade: models.TradeRecord{UserID: 123, Status: models.TradeStatusOpen, Payload: models.TradePayload{PosSide: side, EntrySize: 100}}}
				exchange := &closeExchangeFake{}
				if failed {
					exchange.err = errors.New("timeout")
				}
				m := models.ManualClose{RequestID: uuid.New(), TradeGUID: uuid.New(), Fraction: .5}
				first, err := executeManualClose(context.Background(), repo, exchange, 123, m)
				if err != nil {
					t.Fatal(err)
				}
				if exchange.size != 4 || exchange.side != side {
					t.Fatalf("wrong close: %+v", exchange)
				}
				second, err := executeManualClose(context.Background(), repo, exchange, 123, m)
				if err != nil {
					t.Fatal(err)
				}
				if exchange.submits != 1 || first.Status != second.Status {
					t.Fatal("duplicate submitted")
				}
				if failed && first.Status != "unknown" {
					t.Fatal("timeout treated as definite failure")
				}
			})
		}
	}
}
func TestManualCloseRejectsOtherUserAndClosedTrade(t *testing.T) {
	for _, tc := range []struct {
		user   int64
		status models.TradeStatus
	}{{999, models.TradeStatusOpen}, {123, models.TradeStatusClosed}} {
		repo := &closeRepoFake{trade: models.TradeRecord{UserID: tc.user, Status: tc.status, Payload: models.TradePayload{PosSide: "long"}}}
		exchange := &closeExchangeFake{}
		_, err := executeManualClose(context.Background(), repo, exchange, 123, models.ManualClose{RequestID: uuid.New(), TradeGUID: uuid.New(), Fraction: 1})
		if err == nil || exchange.submits != 0 {
			t.Fatal("invalid close reached exchange")
		}
	}
}
func TestManualCloseSizeBoundaries(t *testing.T) {
	meta := models.Instrument{LotSz: .1, MinSz: .1, MaxMktSz: 100}
	for _, tc := range []struct {
		pos, frac, want float64
		bad             bool
	}{{1.1, .5, .5, false}, {1.1, 1, 1.1, false}, {.1, .5, 0, true}, {0, 1, 0, true}, {10, 1.1, 0, true}, {math.NaN(), 1, 0, true}, {101, 1, 0, true}} {
		got, err := closeSize(tc.pos, tc.frac, meta)
		if (err != nil) != tc.bad {
			t.Fatalf("%+v: err %v", tc, err)
		}
		if !tc.bad && math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("got %v want %v", got, tc.want)
		}
	}
}
