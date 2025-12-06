
-- +goose Up
-- +goose StatementBegin
CREATE TABLE user_settings (
                               id bigserial PRIMARY KEY,
                               chatID bigint NOT NULL,
                               name text NOT NULL default '',
                               settings jsonb default '{}',
                               step text  default ''

);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- +goose StatementEnd