package user_settings

import (
	"context"
	"fmt"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/repository/pg/user_settings/sql"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/samber/lo"
)

// UserSettings implement db store
type UserSettings struct {
	sql *sql.Queries
}

// New instance
func New() *UserSettings {
	return &UserSettings{
		sql: sql.New(),
	}
}

func (u *UserSettings) Insert(ctx context.Context, tx pgx.Tx, user *models.UserSettings) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("UserSettings.Insert: %w", err)
		}
	}()

	var data []byte
	data, err = sonic.Marshal(user.Settings)
	if err != nil {
		return err
	}
	_, err = u.sql.Insert(ctx, tx, &sql.InsertParams{
		Chatid:   user.TelegramID,
		Name:     user.Name,
		Settings: data,
		Step:     user.Step,
		Status:   user.Status,
		Premium:  user.Premium,
	})
	if err != nil {
		return err
	}
	return
}

func (u *UserSettings) Update(ctx context.Context, tx pgx.Tx, user *models.UserSettings) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("UserSettings.Update: %w", err)
		}
	}()
	var data []byte
	data, err = sonic.Marshal(user.Settings)
	if err != nil {
		return err
	}
	return u.sql.Update(ctx, tx, &sql.UpdateParams{
		Chatid:   user.TelegramID,
		Name:     user.Name,
		Settings: data,
		Step:     user.Step,
		Status:   user.Status,
		Premium:  user.Premium,
	})
}

func (u *UserSettings) Delete(ctx context.Context, tx pgx.Tx, user *models.UserSettings) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("UserSettings.Delete: %w", err)
		}
	}()
	return u.sql.Delete(ctx, tx, &sql.DeleteParams{
		Chatid: user.TelegramID,
		ID:     user.ID,
	})
}

func (u *UserSettings) GetById(ctx context.Context, tx pgx.Tx, chatID int64) (user *models.UserSettings, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("UserSettings.GetById: %w", err)
		}
	}()
	resp, err := u.sql.GetById(ctx, tx, chatID)
	if err != nil {
		return nil, err
	}

	var t models.Settings
	err = sonic.Unmarshal(resp.Settings, &t)
	if err != nil {
		return nil, err
	}
	return &models.UserSettings{
		ID:         resp.ID,
		TelegramID: chatID,
		Name:       resp.Name,
		Settings:   t,
		Step:       resp.Step,
		Status:     resp.Status,
		Premium:    resp.Premium,
	}, nil
}

//	func (u *UserSettings) GetAll(ctx context.Context, tx pgx.Tx) (users []*models.UserSettings, err error) {
//		defer func() {
//			if err != nil {
//				err = fmt.Errorf("UserSettings.GetAll: %w", err)
//			}
//		}()
//		resp, err := u.sql.GetAll(ctx, tx)
//		if err != nil {
//			return nil, err
//		}
//		users = make([]*dto.UserSettings, len(resp))
//
//		for i := range resp {
//			users = append(users, &dto.UserSettings{
//				ID:       resp[i].ID,
//				ChatID:   resp[i].Chatid,
//				Name:     resp[i].Name,
//				AuthCode: resp[i].AuthCode,
//				Step:     resp[i].Step,
//			})
//		}
//		return users, nil
//	}
//
// ListEnabled возвращает пользователей, у которых Enabled=true
func (u *UserSettings) ListEnabled(ctx context.Context, tx pgx.Tx) (users []*models.UserSettings, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.ListEnabled: %w", err)
		}
	}()

	resp, err := u.sql.ListEnabled(ctx, tx)
	if err != nil {
		return nil, err
	}
	for i := range resp {
		var t models.Settings
		err = sonic.Unmarshal(resp[i].Settings, &t)
		if err != nil {
			return nil, err
		}
		users = append(users, &models.UserSettings{
			ID:         resp[i].ID,
			TelegramID: resp[i].Chatid,
			Name:       resp[i].Name,
			Settings:   t,
			Step:       lo.FromPtr(resp[i].Step),
			Status:     resp[i].Status,
			Premium:    resp[i].Premium,
		})
	}
	return users, err
}

func (u *UserSettings) CreateTrade(ctx context.Context, tx pgx.Tx, tr *models.TradeRecord) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.CreateTrade: %w", err)
		}
	}()

	err = u.sql.CreateTrade(ctx, tx, &sql.CreateTradeParams{
		Guid: ConvertUUIDToPgType(tr.GUID), UserID: tr.UserID, InstID: tr.InstID, PosSide: tr.PosSide, Side: tr.Side, Timeframe: tr.Timeframe, Strategy: tr.Strategy,
		EntryPrice: tr.EntryPrice, EntrySize: tr.EntrySize, EntryAt: ConvertTimeToPgTimestamptz(tr.EntryAt),
		StopLoss: tr.StopLoss, TakeProfit: tr.TakeProfit, Leverage: int32(tr.Leverage),
		OpenOrderID: tr.OpenOrderID, AlgoID: tr.AlgoID,
		Status: string(tr.Status),
	})
	if err != nil {
		return err
	}

	return err
}

func (u *UserSettings) ListOpenTrades(ctx context.Context, tx pgx.Tx, userID int64) (out []models.TradeRecord, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.ListEnabled: %w", err)
		}
	}()

	resp, err := u.sql.ListOpenTrades(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	for i := range resp {

		out = append(out, models.TradeRecord{
			GUID:        resp[i].Guid.Bytes,
			UserID:      resp[i].UserID,
			InstID:      resp[i].InstID,
			PosSide:     resp[i].PosSide,
			Side:        resp[i].Side,
			Timeframe:   resp[i].Timeframe,
			Strategy:    resp[i].Strategy,
			EntryPrice:  resp[i].EntryPrice,
			EntrySize:   resp[i].EntrySize,
			EntryAt:     resp[i].EntryAt.Time,
			StopLoss:    resp[i].StopLoss,
			TakeProfit:  resp[i].TakeProfit,
			Leverage:    int(resp[i].Leverage),
			OpenOrderID: resp[i].OpenOrderID,
			AlgoID:      resp[i].AlgoID,
			Status:      models.TradeStatus(resp[i].Status),
			CreatedAt:   resp[i].CreatedAt.Time,
			UpdatedAt:   resp[i].UpdatedAt.Time,
		})
	}
	return out, nil
}

func (u *UserSettings) CloseTrade(ctx context.Context, tx pgx.Tx, guid uuid.UUID, in models.TradeCloseInput) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.CloseTrade: %w", err)
		}
	}()

	err = u.sql.CloseTrade(ctx, tx, &sql.CloseTradeParams{
		Guid:           ConvertUUIDToPgType(guid),
		ExitPrice:      in.ExitPrice,
		ExitSize:       in.ExitSize,
		ExitAt:         ConvertTimeToPgTimestamptz(in.ExitAt),
		RealizedPnl:    in.RealizedPnL,
		RealizedPnlPct: in.RealizedPnLPct,
		CloseReason:    string(in.CloseReason),
	})
	return err
}

// ConvertUUIDToPgType ...
func ConvertUUIDToPgType(guid uuid.UUID) pgtype.UUID {
	return pgtype.UUID{
		Bytes: guid,
		Valid: true,
	}
}

// ConvertTimeToPgTimestamptz ...
func ConvertTimeToPgTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{Valid: false, Time: t}
	}
	return pgtype.Timestamptz{Valid: true, Time: t}
}

func (u *UserSettings) ListRecentTrades(ctx context.Context, tx pgx.Tx, userID int64, limit int) (out []models.TradeRecord, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.ListEnabled: %w", err)
		}
	}()

	resp, err := u.sql.ListRecentTrades(ctx, tx, &sql.ListRecentTradesParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, err
	}
	for i := range resp {

		out = append(out, models.TradeRecord{
			GUID:        resp[i].Guid.Bytes,
			UserID:      resp[i].UserID,
			InstID:      resp[i].InstID,
			PosSide:     resp[i].PosSide,
			Side:        resp[i].Side,
			Timeframe:   resp[i].Timeframe,
			Strategy:    resp[i].Strategy,
			EntryPrice:  resp[i].EntryPrice,
			EntrySize:   resp[i].EntrySize,
			EntryAt:     resp[i].EntryAt.Time,
			StopLoss:    resp[i].StopLoss,
			TakeProfit:  resp[i].TakeProfit,
			Leverage:    int(resp[i].Leverage),
			OpenOrderID: resp[i].OpenOrderID,
			AlgoID:      resp[i].AlgoID,
			Status:      models.TradeStatus(resp[i].Status),
			CreatedAt:   resp[i].CreatedAt.Time,
			UpdatedAt:   resp[i].UpdatedAt.Time,
		})
	}
	return out, nil

}

func (u *UserSettings) GetTradeStats(ctx context.Context, tx pgx.Tx, userID int64) (out models.TradeStats, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.GetTradeStats: %w", err)
		}
	}()

	resp, err := u.sql.GetTradeStats(ctx, tx, userID)
	if err != nil {
		return models.TradeStats{}, err
	}

	out = models.TradeStats{
		TotalTrades:   int(resp.TotalTrades),
		OpenTrades:    int(resp.OpenTrades),
		ClosedTrades:  int(resp.ClosedTrades),
		Wins:          int(resp.Wins),
		Losses:        int(resp.Losses),
		WinRatePct:    float64(resp.Wins),
		TotalPnL:      resp.TotalPnl,
		AvgPnL:        resp.AvgPnl,
		AvgWin:        resp.AvgWin,
		AvgLoss:       resp.AvgLoss,
		TPCount:       int(resp.TpCount),
		SLCount:       int(resp.SlCount),
		TimeStopCount: int(resp.TimeStopCount),
		PartialCount:  int(resp.PartialCount),
		ManualCount:   int(resp.ManualCount),
		UnknownCount:  int(resp.UnknownCount),
	}

	if err != nil {
		return models.TradeStats{}, err
	}

	if out.ClosedTrades > 0 {
		out.WinRatePct = float64(out.Wins) / float64(out.ClosedTrades) * 100
	}

	return out, nil
}
