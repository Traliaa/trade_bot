
-- +goose Up
-- +goose StatementBegin
alter table user_settings
    add status bool default false not null;
alter table user_settings
    add premium bool default false not null;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- +goose StatementEnd