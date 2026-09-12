-- +goose Up
CREATE TABLE public.service_status_events (
    id bigserial PRIMARY KEY,
    state text NOT NULL,
    occurred_at timestamptz NOT NULL,
    instruments integer NOT NULL DEFAULT 0,
    progress integer NOT NULL DEFAULT 0
);
CREATE INDEX service_status_events_recent ON public.service_status_events(occurred_at DESC, id DESC);

-- +goose Down
DROP TABLE public.service_status_events;
