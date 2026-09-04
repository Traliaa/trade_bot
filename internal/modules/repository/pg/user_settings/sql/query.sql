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
-- name: CreateTradeHistory :exec
INSERT INTO public.trade_history (
    guid,
    user_id,
    inst_id,
    strategy,
    timeframe,
    status,
    close_reason,
    entry_at,
    exit_at,
    payload,
    created_at,
    updated_at
) VALUES (
             @guid,
             @user_id,
             @inst_id,
             @strategy,
             @timeframe,
             @status,
             @close_reason,
             @entry_at,
             @exit_at,
             @payload,
             @created_at,
             @updated_at
         );

-- name: UpdateTradeHistoryPayload :exec
UPDATE public.trade_history
SET
    payload = @payload,
    updated_at = @updated_at
WHERE guid = @guid;

-- name: CloseTradeHistory :exec
UPDATE public.trade_history
SET
    status = @status,
    close_reason = @close_reason,
    exit_at = @exit_at,
    payload = @payload,
    updated_at = @updated_at
WHERE guid = @guid;

-- name: GetTradeHistoryByGUID :one
SELECT
    guid,
    user_id,
    inst_id,
    strategy,
    timeframe,
    status,
    close_reason,
    entry_at,
    exit_at,
    payload,
    created_at,
    updated_at
FROM public.trade_history
WHERE guid = @guid;

-- name: GetOpenTradeByUserAndInst :one
SELECT
    guid,
    user_id,
    inst_id,
    strategy,
    timeframe,
    status,
    close_reason,
    entry_at,
    exit_at,
    payload,
    created_at,
    updated_at
FROM public.trade_history
WHERE user_id = @user_id
  AND inst_id = @inst_id
  AND status = 'open'
ORDER BY entry_at DESC
LIMIT 1;

-- name: GetOpenTradeByUserAndInstSide :one
SELECT
    guid,
    user_id,
    inst_id,
    strategy,
    timeframe,
    status,
    close_reason,
    entry_at,
    exit_at,
    payload,
    created_at,
    updated_at
FROM public.trade_history
WHERE user_id = @user_id
  AND inst_id = @inst_id
  AND payload->>'pos_side' = @payload
  AND status = 'open'
ORDER BY entry_at DESC
LIMIT 1;

-- name: ListRecentTradesByUser :many
SELECT
    guid,
    user_id,
    inst_id,
    strategy,
    timeframe,
    status,
    close_reason,
    entry_at,
    exit_at,
    payload,
    created_at,
    updated_at
FROM public.trade_history
WHERE user_id = @user_id
ORDER BY created_at DESC
LIMIT $1;

-- name: ListClosedTradesByUser :many
SELECT
    guid,
    user_id,
    inst_id,
    strategy,
    timeframe,
    status,
    close_reason,
    entry_at,
    exit_at,
    payload,
    created_at,
    updated_at
FROM public.trade_history
WHERE user_id = @user_id
  AND status = 'closed'
ORDER BY exit_at DESC NULLS LAST
LIMIT $1;

-- name: ListAllClosedTradesByUser :many
SELECT
    guid,
    user_id,
    inst_id,
    strategy,
    timeframe,
    status,
    close_reason,
    entry_at,
    exit_at,
    payload,
    created_at,
    updated_at
FROM public.trade_history
WHERE user_id = @user_id
  AND status = 'closed'
ORDER BY exit_at DESC NULLS LAST;

-- name: ListOpenTradesByUser :many
SELECT
    guid,
    user_id,
    inst_id,
    strategy,
    timeframe,
    status,
    close_reason,
    entry_at,
    exit_at,
    payload,
    created_at,
    updated_at
FROM public.trade_history
WHERE user_id = @user_id
  AND status = 'open'
ORDER BY entry_at DESC;

-- name: ListTradesByUserForPeriod :many
SELECT
    guid,
    user_id,
    inst_id,
    strategy,
    timeframe,
    status,
    close_reason,
    entry_at,
    exit_at,
    payload,
    created_at,
    updated_at
FROM public.trade_history
WHERE user_id = @user_id
  AND created_at >= @date_from
  AND created_at < @date_to
ORDER BY created_at DESC;

-- name: UpsertTradeFill :exec
INSERT INTO public.trade_fills (
    trade_guid,
    trade_id,
    order_id,
    algo_id,
    inst_id,
    pos_side,
    side,
    role,
    fill_price,
    fill_size,
    fee,
    realized_pnl,
    filled_at
) VALUES (
    @trade_guid,
    @trade_id,
    @order_id,
    @algo_id,
    @inst_id,
    @pos_side,
    @side,
    @role,
    @fill_price,
    @fill_size,
    @fee,
    @realized_pnl,
    @filled_at
)
ON CONFLICT (trade_guid, trade_id) DO UPDATE SET
    order_id = EXCLUDED.order_id,
    algo_id = EXCLUDED.algo_id,
    fee = EXCLUDED.fee,
    realized_pnl = EXCLUDED.realized_pnl;

-- name: ListTradeFills :many
SELECT
    trade_guid,
    trade_id,
    order_id,
    algo_id,
    inst_id,
    pos_side,
    side,
    role,
    fill_price,
    fill_size,
    fee,
    realized_pnl,
    filled_at
FROM public.trade_fills
WHERE trade_guid = @trade_guid
ORDER BY filled_at ASC;
