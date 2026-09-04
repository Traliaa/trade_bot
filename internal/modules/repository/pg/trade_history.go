package pg

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"
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

func (u *User) UpsertTradeFills(ctx context.Context, fills []models.TradeFillRecord) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.UpsertTradeFills: %w", err)
		}
	}()

	return u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		return u.user.UpsertTradeFills(ctxTx, tx, fills)
	})
}

func (u *User) ListTradeFills(ctx context.Context, guid uuid.UUID) (out []models.TradeFillRecord, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.ListTradeFills: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		out, err = u.user.ListTradeFills(ctxTx, tx, guid)
		return err
	})
	return out, err
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

func (u *User) ListAllClosedTradesByUser(ctx context.Context, userID int64) (out []models.TradeRecord, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.ListAllClosedTradesByUser: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		out, err = u.user.ListAllClosedTradesByUser(ctxTx, tx, userID)
		return err
	})
	return out, err
}

func (r *User) GetTradeStats(ctx context.Context, userID int64) (models.TradeStats, error) {
	trades, err := r.ListAllClosedTradesByUser(ctx, userID)
	if err != nil {
		return models.TradeStats{}, err
	}

	openTrades, err := r.ListOpenTrades(ctx, userID)
	if err != nil {
		return models.TradeStats{}, err
	}

	stats := buildTradeStats(trades)
	stats.OpenTrades = int64(len(openTrades))
	stats.TotalTrades = stats.ClosedTrades + stats.OpenTrades
	for _, trade := range openTrades {
		stats.OpenPnL += trade.Payload.UnrealizedPnL
	}
	return stats, nil
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
		case p.RMultiple < 0:
			st.Losses++
		default:
			st.BreakevenTrades++
		}
		if p.RealizedPnL > 0 {
			grossProfit += p.RealizedPnL
		} else if p.RealizedPnL < 0 {
			grossLoss += -p.RealizedPnL
		}

		if p.TookPartial {
			st.PartialTrades++
		}
		st.SLReplaceAttempts += int64(p.SLReplaceAttempts)
		st.SLReplaceFailures += int64(p.SLReplaceFailures)
		st.TPReplaceAttempts += int64(p.TPReplaceAttempts)
		st.TPReplaceFailures += int64(p.TPReplaceFailures)
		st.AlgoCancelFailures += int64(p.AlgoCancelFailures)
		st.BEReplaceAttempts += int64(p.BEReplaceAttempts)
		st.BEReplaceFailures += int64(p.BEReplaceFailures)

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
		case models.CloseReasonTimeStop:
			st.TimeStopCount++
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
	st.ByDirection = buildStatsBreakdown(trades, func(tr models.TradeRecord) string {
		if tr.Payload.PosSide == "" {
			return "unknown"
		}
		return tr.Payload.PosSide
	})
	st.BySignalScore = buildStatsBreakdown(trades, func(tr models.TradeRecord) string {
		if tr.Payload.EntrySignalScore <= 0 {
			return "unknown"
		}
		return strconv.Itoa(tr.Payload.EntrySignalScore)
	})
	st.ByImpulse = buildStatsBreakdown(trades, func(tr models.TradeRecord) string {
		return impulseBucket(tr.Payload.EntryImpulseBodyPct)
	})
	st.ByCloseReason = buildStatsBreakdown(trades, func(tr models.TradeRecord) string {
		return string(tr.CloseReason)
	})
	st.ByMonth = buildStatsBreakdown(trades, func(tr models.TradeRecord) string {
		if tr.ExitAt == nil || tr.ExitAt.IsZero() {
			return "unknown"
		}
		return tr.ExitAt.UTC().Format("2006-01")
	})
	st.Windows = buildPerformanceWindows(trades)

	return st
}

type breakdownAccumulator struct {
	stats       models.TradeStatsBreakdown
	grossProfit float64
	grossLoss   float64
}

func buildStatsBreakdown(trades []models.TradeRecord, keyFn func(models.TradeRecord) string) []models.TradeStatsBreakdown {
	groups := make(map[string]*breakdownAccumulator)
	for _, trade := range trades {
		key := keyFn(trade)
		if key == "" {
			key = "unknown"
		}
		acc := groups[key]
		if acc == nil {
			acc = &breakdownAccumulator{stats: models.TradeStatsBreakdown{Key: key}}
			groups[key] = acc
		}

		p := trade.Payload
		acc.stats.Trades++
		acc.stats.TotalPnL += p.RealizedPnL
		acc.stats.TotalR += p.RMultiple
		switch {
		case p.RMultiple > 0:
			acc.stats.Wins++
		case p.RMultiple < 0:
			acc.stats.Losses++
		}
		if p.RealizedPnL > 0 {
			acc.grossProfit += p.RealizedPnL
		} else if p.RealizedPnL < 0 {
			acc.grossLoss += -p.RealizedPnL
		}
	}

	result := make([]models.TradeStatsBreakdown, 0, len(groups))
	for _, acc := range groups {
		if acc.stats.Trades > 0 {
			acc.stats.WinRate = float64(acc.stats.Wins) / float64(acc.stats.Trades) * 100
			acc.stats.AvgR = acc.stats.TotalR / float64(acc.stats.Trades)
		}
		if acc.grossLoss > 0 {
			acc.stats.ProfitFactor = acc.grossProfit / acc.grossLoss
		}
		result = append(result, acc.stats)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func impulseBucket(v float64) string {
	switch {
	case v <= 0:
		return "unknown"
	case v < 0.003:
		return "<0.30%"
	case v <= 0.006:
		return "0.30-0.60%"
	default:
		return ">0.60%"
	}
}

func buildPerformanceWindows(trades []models.TradeRecord) []models.TradePerformanceWindow {
	if len(trades) == 0 {
		return nil
	}

	windows := make([]models.TradePerformanceWindow, 0, 3)
	windows = append(windows, performanceWindow("recent_50", sliceTrades(trades, 0, 50)))
	windows = append(windows, performanceWindow("recent_100", sliceTrades(trades, 0, 100)))
	if len(trades) > 100 {
		windows = append(windows, performanceWindow("previous_100", sliceTrades(trades, 100, 200)))
	}
	return windows
}

func sliceTrades(trades []models.TradeRecord, from, to int) []models.TradeRecord {
	if from >= len(trades) {
		return nil
	}
	if to > len(trades) {
		to = len(trades)
	}
	return trades[from:to]
}

func performanceWindow(key string, trades []models.TradeRecord) models.TradePerformanceWindow {
	window := models.TradePerformanceWindow{Key: key, Trades: int64(len(trades))}
	if len(trades) == 0 {
		return window
	}

	var grossProfit, grossLoss float64
	for _, trade := range trades {
		p := trade.Payload
		window.TotalR += p.RMultiple
		window.TotalPnL += p.RealizedPnL
		if p.RMultiple > 0 {
			window.Wins++
		} else if p.RMultiple < 0 {
			window.Losses++
		}
		if p.RealizedPnL > 0 {
			grossProfit += p.RealizedPnL
		} else if p.RealizedPnL < 0 {
			grossLoss += -p.RealizedPnL
		}
	}

	window.WinRate = float64(window.Wins) / float64(window.Trades) * 100
	window.AvgR = window.TotalR / float64(window.Trades)
	if grossLoss > 0 {
		window.ProfitFactor = grossProfit / grossLoss
	}

	newest := trades[0].ExitAt
	oldest := trades[len(trades)-1].ExitAt
	if oldest != nil {
		window.From = oldest.UTC().Format(time.RFC3339)
	}
	if newest != nil {
		window.To = newest.UTC().Format(time.RFC3339)
	}
	return window
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
