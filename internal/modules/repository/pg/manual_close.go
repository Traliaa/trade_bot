package pg

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"trade_bot/internal/models"
)

func readManualClose(row pgx.Row) (m models.ManualClose, err error) {
	err = row.Scan(&m.RequestID, &m.TradeGUID, &m.Fraction, &m.Size, &m.OrderID, &m.Status, &m.Message)
	return
}

const closeColumns = "request_id, trade_guid, fraction, size, order_id, status, message"

func (u *User) GetManualClose(ctx context.Context, userID int64, guid, requestID uuid.UUID) (out models.ManualClose, err error) {
	err = u.db.RunMaster(ctx, func(ctx context.Context, tx pgx.Tx) error {
		out, err = readManualClose(tx.QueryRow(ctx, "SELECT "+closeColumns+" FROM manual_close_requests WHERE request_id=$1 AND trade_guid=$2 AND EXISTS (SELECT 1 FROM trade_history WHERE guid=$2 AND user_id=$3)", requestID, guid, userID))
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrCloseNotFound
		}
		return err
	})
	return
}

// Commit the reservation before contacting OKX. An uncertain request stays
// reserved across process restarts and may only be reconciled, never resent.
func (u *User) ClaimManualClose(ctx context.Context, userID int64, m models.ManualClose) (out models.ManualClose, claimed bool, err error) {
	err = u.db.RunMaster(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, "SELECT status FROM trade_history WHERE guid=$1 AND user_id=$2 FOR UPDATE", m.TradeGUID, userID).Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return models.ErrCloseNotFound
			}
			return err
		}
		var e error
		out, e = readManualClose(tx.QueryRow(ctx, "SELECT "+closeColumns+" FROM manual_close_requests WHERE request_id=$1", m.RequestID))
		if e == nil {
			if out.TradeGUID != m.TradeGUID || out.Fraction != m.Fraction {
				return models.ErrCloseConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if status != "open" {
			return models.ErrCloseConflict
		}
		tag, e := tx.Exec(ctx, "INSERT INTO manual_close_requests(request_id,trade_guid,fraction,status) VALUES($1,$2,$3,'pending') ON CONFLICT DO NOTHING", m.RequestID, m.TradeGUID, m.Fraction)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			out, e = readManualClose(tx.QueryRow(ctx, "SELECT "+closeColumns+" FROM manual_close_requests WHERE trade_guid=$1 AND status IN ('pending','accepted','unknown')", m.TradeGUID))
			if errors.Is(e, pgx.ErrNoRows) {
				return models.ErrCloseConflict
			}
			return e
		}
		out = m
		out.Status = "pending"
		claimed = true
		return nil
	})
	return
}

func (u *User) SaveManualClose(ctx context.Context, m models.ManualClose) error {
	return u.db.RunMaster(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE manual_close_requests SET size=$2,order_id=$3,status=$4,message=$5,updated_at=now() WHERE request_id=$1", m.RequestID, m.Size, m.OrderID, m.Status, m.Message)
		return err
	})
}

func (u *User) TagManualClose(ctx context.Context, m models.ManualClose) error {
	return u.db.RunMaster(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE trade_history SET close_reason='manual',updated_at=now()
		WHERE guid=$1 AND status='closed' AND $2<>'' AND $2=(
		 SELECT order_id FROM trade_fills WHERE trade_guid=$1 AND role='exit'
		 ORDER BY filled_at DESC,id DESC LIMIT 1)`, m.TradeGUID, m.OrderID)
		return err
	})
}
