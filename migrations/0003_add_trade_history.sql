
-- +goose Up
-- +goose StatementBegin
CREATE TABLE trade_history (
                               guid                uuid PRIMARY KEY,
                               user_id           bigint        NOT NULL,
                               inst_id           text          NOT NULL,
                               pos_side          text          NOT NULL,
                               side              text          NOT NULL,
                               timeframe         text          NOT NULL,
                               strategy          text          NOT NULL,

                               entry_price       double precision NOT NULL,
                               entry_size        double precision NOT NULL,
                               entry_at          timestamptz   NOT NULL,

                               stop_loss         double precision NOT NULL,
                               take_profit       double precision NOT NULL,
                               leverage          integer       NOT NULL,

                               open_order_id     text          NOT NULL DEFAULT '',
                               algo_id           text          NOT NULL DEFAULT '',

                               exit_price        double precision NOT NULL DEFAULT 0,
                               exit_size         double precision NOT NULL DEFAULT 0,
                               exit_at           timestamptz,
                               realized_pnl      double precision NOT NULL DEFAULT 0,
                               realized_pnl_pct  double precision NOT NULL DEFAULT 0,
                               close_reason      text          NOT NULL DEFAULT 'unknown',

                               status            text          NOT NULL,
                               created_at        timestamptz   NOT NULL DEFAULT now(),
                               updated_at        timestamptz   NOT NULL DEFAULT now()
);

CREATE INDEX trade_history_user_status_idx
    ON trade_history (user_id, status);

CREATE INDEX trade_history_user_inst_pos_idx
    ON trade_history (user_id, inst_id, pos_side);

CREATE INDEX trade_history_entry_at_idx
    ON trade_history (entry_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- +goose StatementEnd