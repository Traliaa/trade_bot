package pg

import (
	"context"
	"fmt"
	"sort"
	"trade_bot/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateTradeHistory ...
func (u *User) CreateTradeHistory(ctx context.Context, tr models.TradeRecord) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.CreateTradeHistory: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		err = u.user.CreateTradeHistory(ctxTx, tx, tr)
		return err
	})
	return err
}

// UpdatePayload ....
func (u *User) UpdatePayload(ctx context.Context, guid uuid.UUID, payloadModel models.TradePayload) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.UpdatePayload: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		err = u.user.UpdatePayload(ctxTx, tx, guid, payloadModel)
		return err
	})
	return err
}

// CloseTrade ...
func (u *User) CloseTrade(ctx context.Context, guid uuid.UUID,
	in models.TradeCloseInput) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.CloseTrade: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		err = u.user.Close(ctxTx, tx, guid, in.CloseReason, in.Payload, in.ExitAt)
		return err
	})
	return err
}
func (u *User) GetByGUID(ctx context.Context, guid uuid.UUID) (out *models.TradeRecord, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.GetByGUID: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		out, err = u.user.GetByGUID(ctxTx, tx, guid)
		return err
	})
	return out, err
}
func (u *User) FindOpenTrade(ctx context.Context, userID int64, instID string) (out *models.TradeRecord, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.FindOpenTrade: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		out, err = u.user.FindOpenTrade(ctxTx, tx, userID, instID)
		return err
	})
	return out, err
}
func (u *User) ListRecentTrades(ctx context.Context, userID int64, limit int32) (out []models.TradeRecord, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.ListRecentTrades: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		out, err = u.user.ListRecentTrades(ctxTx, tx, userID, limit)
		return err
	})
	return out, err
}
func (u *User) ListOpenTrades(ctx context.Context, userID int64) (out []models.TradeRecord, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.ListOpenTrades: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		out, err = u.user.ListOpenTrades(ctxTx, tx, userID)
		return err
	})
	return out, err
}
func (u *User) ListClosedTradesByUser(ctx context.Context, userID int64, limit int32) (out []models.TradeRecord, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.ListClosedTradesByUser: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		out, err = u.user.ListClosedTradesByUser(ctxTx, tx, userID, limit)
		return err
	})
	return out, err
}

func (r *User) GetTradeStats(ctx context.Context, userID int64) (models.TradeStats, error) {
	trades, err := r.ListClosedTradesByUser(ctx, userID, 500)
	if err != nil {
		return models.TradeStats{}, err
	}

	return buildTradeStats(trades), nil
}
func buildTradeStats(trades []models.TradeRecord) models.TradeStats {
	var st models.TradeStats

	st.ClosedTrades = int64(len(trades))
	st.TotalTrades = st.ClosedTrades

	if len(trades) == 0 {
		return st
	}

	rVals := make([]float64, 0, len(trades))

	var grossProfit float64
	var grossLoss float64
	var totalDuration int64
	var totalMFER float64
	var totalMAER float64
	var mfeCount int64
	var maeCount int64

	st.BestTradeR = 0
	st.WorstTradeR = 0

	for i, tr := range trades {
		p := tr.Payload

		st.TotalPnL += p.RealizedPnL
		st.TotalR += p.RMultiple
		totalDuration += p.DurationSec

		rVals = append(rVals, p.RMultiple)

		if i == 0 {
			st.BestTradeR = p.RMultiple
			st.WorstTradeR = p.RMultiple
		} else {
			if p.RMultiple > st.BestTradeR {
				st.BestTradeR = p.RMultiple
			}
			if p.RMultiple < st.WorstTradeR {
				st.WorstTradeR = p.RMultiple
			}
		}

		switch {
		case p.RMultiple > 0:
			st.Wins++
			grossProfit += p.RealizedPnL
		case p.RMultiple < 0:
			st.Losses++
			grossLoss += -p.RealizedPnL
		default:
			st.BreakevenTrades++
		}

		if p.TookPartial {
			st.PartialTrades++
		}

		switch tr.CloseReason {
		case models.CloseReasonTP:
			st.TPCount++
		case models.CloseReasonSL:
			st.SLCount++
		case models.CloseReasonBreakEven:
			st.BreakEvenCount++
		case models.CloseReasonLockProfit:
			st.LockProfitCount++
		case models.CloseReasonPartialExit:
			st.PartialExitCount++
		case models.CloseReasonTimeStopEarly:
			st.TimeStopEarlyCount++
		case models.CloseReasonTimeStopStale:
			st.TimeStopStaleCount++
		case models.CloseReasonManual:
			st.ManualCloseCount++
		case models.CloseReasonRecovery:
			st.RecoveryCloseCount++
		case models.CloseReasonForceClose:
			st.ForceCloseCount++
		default:
			st.UnknownCloseCount++
		}

		if p.MFER != 0 {
			totalMFER += p.MFER
			mfeCount++
		}
		if p.MAER != 0 {
			totalMAER += p.MAER
			maeCount++
		}
	}

	st.AvgPnL = st.TotalPnL / float64(len(trades))
	st.AvgR = st.TotalR / float64(len(trades))
	st.WinRate = float64(st.Wins) / float64(len(trades)) * 100
	st.AvgDurationSec = totalDuration / int64(len(trades))

	if grossLoss > 0 {
		st.ProfitFactor = grossProfit / grossLoss
	}
	if mfeCount > 0 {
		st.AvgMFER = totalMFER / float64(mfeCount)
	}
	if maeCount > 0 {
		st.AvgMAER = totalMAER / float64(maeCount)
	}

	st.MedianR = medianFloat64(rVals)

	return st
}

func medianFloat64(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}

	cp := append([]float64(nil), vals...)
	sort.Float64s(cp)

	n := len(cp)
	mid := n / 2

	if n%2 == 0 {
		return (cp[mid-1] + cp[mid]) / 2
	}
	return cp[mid]
}
