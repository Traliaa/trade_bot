-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS public.trade_history (
                                                    guid         uuid PRIMARY KEY,
                                                    user_id      bigint                      NOT NULL,
                                                    inst_id      text                        NOT NULL,
                                                    strategy     text                        NOT NULL,
                                                    timeframe    text                        NOT NULL,
                                                    status       text                        NOT NULL,
                                                    close_reason text                        NOT NULL DEFAULT 'unknown',
                                                    entry_at     timestamptz                 NOT NULL,
                                                    exit_at      timestamptz,
                                                    payload      jsonb                       NOT NULL DEFAULT '{}'::jsonb,
                                                    created_at   timestamptz                 NOT NULL DEFAULT now(),
                                                    updated_at   timestamptz                 NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS trade_history_user_id_created_at_idx
    ON public.trade_history (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS trade_history_user_id_status_created_at_idx
    ON public.trade_history (user_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS trade_history_user_id_entry_at_idx
    ON public.trade_history (user_id, entry_at DESC);

CREATE INDEX IF NOT EXISTS trade_history_user_id_close_reason_idx
    ON public.trade_history (user_id, close_reason);

CREATE INDEX IF NOT EXISTS trade_history_user_id_inst_id_entry_at_idx
    ON public.trade_history (user_id, inst_id, entry_at DESC);

-- если позже понадобится искать по payload:
-- CREATE INDEX IF NOT EXISTS trade_history_payload_gin_idx
--     ON public.trade_history USING gin (payload);

-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS public.trade_history;

-- +goose StatementEnd