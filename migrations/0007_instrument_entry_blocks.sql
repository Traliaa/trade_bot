-- +goose Up
CREATE TABLE public.instrument_entry_blocks (
    user_id bigint NOT NULL,
    inst_id text NOT NULL CHECK (inst_id <> ''),
    code text NOT NULL CHECK (code = '51155'),
    blocked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, inst_id)
);

-- No automatic expiry: a restart or timer must not re-enable a prohibited pair.
-- +goose Down
DROP TABLE public.instrument_entry_blocks;
