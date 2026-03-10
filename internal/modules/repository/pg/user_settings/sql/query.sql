-- name: Insert :one
INSERT INTO user_settings (
    chatid, name, settings, step, status,premium
) VALUES (
             @chatid, @name, @settings, @step::text, @status, @premium
         ) returning id;


-- name: Update :exec
UPDATE user_settings
SET  name = @name, settings = @settings, step = @step::text, status = @status,premium = @premium
WHERE chatid = @chatid;

-- name: ListEnabled :many
SELECT *
FROM user_settings
WHERE status = true;




-- name: Delete :exec
DELETE FROM user_settings
WHERE chatid = @chatid and  id = @id;


-- name: GetById :one
SELECT id, name, settings, step::text,status,premium FROM user_settings WHERE chatid = @chatid;


-- name: GetAll :many
SELECT id, chatid, name, settings, step::text, status,premium FROM user_settings;

-- name: CreateTrade :exec
INSERT INTO trade_history (
    guid, user_id, inst_id, pos_side, side, timeframe, strategy,
    entry_price, entry_size, entry_at,
    stop_loss, take_profit, leverage,
    open_order_id, algo_id,
    status, created_at, updated_at
) VALUES (
             $1,$2,$3,$4,$5,$6,$7,
             $8,$9,$10,
             $11,$12,$13,
             $14,$15,
             $16,now(),now()
         );

-- name: ListOpenTrades :many
SELECT
    guid, user_id, inst_id, pos_side, side, timeframe, strategy,
    entry_price, entry_size, entry_at,
    stop_loss, take_profit, leverage,
    open_order_id, algo_id,
    status, created_at, updated_at
FROM trade_history
WHERE user_id = $1 AND status = 'open'
ORDER BY entry_at ASC;

-- name: CloseTrade :exec
UPDATE trade_history
SET
    exit_price = $2,
    exit_size = $3,
    exit_at = $4,
    realized_pnl = $5,
    realized_pnl_pct = $6,
    close_reason = $7,
    status = 'closed',
    updated_at = now()
WHERE guid = $1
  AND status = 'open';