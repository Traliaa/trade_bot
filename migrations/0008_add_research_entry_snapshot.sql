-- +goose Up
ALTER TABLE public.trade_history
    ADD COLUMN IF NOT EXISTS research_entry_snapshot jsonb NULL;

-- +goose Down
-- DESTRUCTIVE: manual-only data removal. Application rollback must keep this
-- nullable column; old binaries can INSERT without it and preserve existing data.
ALTER TABLE public.trade_history DROP COLUMN research_entry_snapshot;
