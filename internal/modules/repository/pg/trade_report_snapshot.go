package pg

import (
	"context"
	"fmt"
	"time"
	"trade_bot/internal/models"

	"github.com/google/uuid"
)

// One SELECT gives history, open counts, and fills the same MVCC snapshot.
// User-scoped history and matching fills are loaded without N+1 requests.
const tradeReportSnapshotSQL = `
SELECT t.guid, t.user_id, t.inst_id, t.strategy, t.timeframe, t.status,
       t.close_reason, t.entry_at, t.exit_at, t.payload, t.created_at, t.updated_at,
       f.trade_id IS NOT NULL,
       COALESCE(f.trade_guid,t.guid), COALESCE(f.trade_id,''), COALESCE(f.order_id,''),
       COALESCE(f.algo_id,''), COALESCE(f.inst_id,''), COALESCE(f.pos_side,''),
       COALESCE(f.side,''), COALESCE(f.role,''), COALESCE(f.fill_price,0),
       COALESCE(f.fill_size,0), COALESCE(f.fee,0), COALESCE(f.realized_pnl,0),
       COALESCE(f.filled_at,'epoch'::timestamptz), statement_timestamp()
FROM public.trade_history t
LEFT JOIN public.trade_fills f ON f.trade_guid = t.guid AND t.status = 'closed'
WHERE t.user_id = $1 AND t.status IN ('open', 'closed')
ORDER BY t.exit_at DESC NULLS LAST, t.guid, f.filled_at, f.id`

func (r *User) readTradeReportSnapshot(ctx context.Context, userID int64) ([]tradeReportSnapshot, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	checkedAt := time.Now().UTC()
	rows, err := r.db.Conn().Query(ctx, tradeReportSnapshotSQL, userID)
	if err != nil {
		return nil, checkedAt, fmt.Errorf("read trade report: %w", err)
	}
	defer rows.Close()
	result := []tradeReportSnapshot{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var s tradeReportSnapshot
		t := &s.Trade
		var payload []byte
		var hasFill bool
		var f models.TradeFillRecord
		if err := rows.Scan(&t.GUID, &t.UserID, &t.InstID, &t.Strategy, &t.Timeframe, &t.Status, &t.CloseReason, &t.EntryAt, &t.ExitAt, &payload, &t.CreatedAt, &t.UpdatedAt,
			&hasFill, &f.TradeGUID, &f.TradeID, &f.OrderID, &f.AlgoID, &f.InstID, &f.PosSide, &f.Side, &f.Role, &f.FillPrice, &f.FillSize, &f.Fee, &f.RealizedPnL, &f.FilledAt, &checkedAt); err != nil {
			return nil, checkedAt, fmt.Errorf("scan trade report: %w", err)
		}
		i, found := index[t.GUID]
		if !found {
			if t.Payload, err = models.UnmarshalTradePayload(payload); err != nil {
				return nil, checkedAt, fmt.Errorf("decode trade report payload: %w", err)
			}
			i = len(result)
			index[t.GUID] = i
			result = append(result, s)
		}
		// Scan typed PG floats directly so NaN/Infinity reach per-trade validation
		// instead of becoming JSON strings that abort the whole report.
		if hasFill {
			result[i].Fills = append(result[i].Fills, f)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, checkedAt, fmt.Errorf("read trade report rows: %w", err)
	}
	return result, checkedAt, nil
}
