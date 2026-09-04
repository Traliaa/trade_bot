-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS public.trade_fills (
    id           bigserial PRIMARY KEY,
    trade_guid   uuid             NOT NULL REFERENCES public.trade_history(guid) ON DELETE CASCADE,
    trade_id     text             NOT NULL,
    order_id     text             NOT NULL DEFAULT '',
    algo_id      text             NOT NULL DEFAULT '',
    inst_id      text             NOT NULL,
    pos_side     text             NOT NULL,
    side         text             NOT NULL,
    role         text             NOT NULL CHECK (role IN ('entry', 'exit')),
    fill_price   double precision NOT NULL,
    fill_size    double precision NOT NULL,
    fee          double precision NOT NULL DEFAULT 0,
    realized_pnl double precision NOT NULL DEFAULT 0,
    filled_at    timestamptz      NOT NULL,
    created_at   timestamptz      NOT NULL DEFAULT now(),
    UNIQUE (trade_guid, trade_id)
);

CREATE INDEX IF NOT EXISTS trade_fills_trade_guid_filled_at_idx
    ON public.trade_fills (trade_guid, filled_at);

CREATE INDEX IF NOT EXISTS trade_history_user_id_status_exit_at_idx
    ON public.trade_history (user_id, status, exit_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS public.trade_history_user_id_status_exit_at_idx;
DROP TABLE IF EXISTS public.trade_fills;

-- +goose StatementEnd
