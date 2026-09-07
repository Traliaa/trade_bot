-- +goose Up
CREATE TABLE public.manual_close_requests (
 request_id uuid PRIMARY KEY,
 trade_guid uuid NOT NULL REFERENCES public.trade_history(guid),
 fraction double precision NOT NULL CHECK (fraction > 0 AND fraction <= 1),
 size double precision NOT NULL DEFAULT 0,
 order_id text NOT NULL DEFAULT '',
 status text NOT NULL CHECK (status IN ('pending','accepted','unknown','filled','canceled','rejected')),
 message text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX manual_close_one_active ON public.manual_close_requests(trade_guid)
 WHERE status IN ('pending','accepted','unknown');

-- +goose Down
DROP TABLE public.manual_close_requests;
