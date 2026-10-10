package collect

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sort"
)

const orderLinksSelect = `SELECT row_id,
 COALESCE(CASE WHEN octet_length(order_id)<=128 THEN order_id END,''),
 COALESCE(CASE WHEN octet_length(role)<=8 THEN role END,'')
 FROM research_collect.order_links_v1 WHERE row_id = ANY($1)
 ORDER BY row_id, order_id, role LIMIT 10001`

// Called inside the entry transaction, never with the trading service pool.
func loadEntryLinks(ctx context.Context, tx pgx.Tx, b *EntryBatch) error {
	if len(b.entries) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(b.entries))
	index := map[uuid.UUID]int{}
	owners := map[string]int{}
	invalid := map[int]bool{}
	exits := map[int]map[string]bool{}
	for i, e := range b.entries {
		ids = append(ids, e.rowID)
		index[e.rowID] = i
		exits[i] = map[string]bool{}
		if e.orderID == "" {
			invalid[i] = true
			continue
		}
		if prior, ok := owners[e.orderID]; ok {
			invalid[i] = true
			invalid[prior] = true
		}
		owners[e.orderID] = i
	}
	rows, err := tx.Query(ctx, orderLinksSelect, ids)
	if err != nil {
		return errDB
	}
	defer rows.Close()
	count := 0
	overflow := false
	for rows.Next() {
		count++
		if count > 10000 {
			overflow = true
			break
		}
		var id uuid.UUID
		var order, role string
		if rows.Scan(&id, &order, &role) != nil {
			return errDBRows
		}
		i, ok := index[id]
		if !ok {
			return errDBRows
		}
		if order == "" || len(order) > 128 || (role != "entry" && role != "exit") {
			invalid[i] = true
			continue
		}
		if prior, ok := owners[order]; ok && prior != i {
			invalid[i] = true
			invalid[prior] = true
		}
		owners[order] = i
		if role == "entry" {
			if order != b.entries[i].orderID {
				invalid[i] = true
			}
		} else {
			if order == b.entries[i].orderID {
				invalid[i] = true
			}
			exits[i][order] = true
		}
	}
	if rows.Err() != nil || ctx.Err() != nil {
		return errDB
	}
	for i := range b.entries {
		b.entries[i].linksComplete = !overflow && !invalid[i] && !b.Coverage.Truncated
		for order := range exits[i] {
			b.entries[i].exitOrders = append(b.entries[i].exitOrders, order)
		}
		sort.Strings(b.entries[i].exitOrders)
	}
	return nil
}
