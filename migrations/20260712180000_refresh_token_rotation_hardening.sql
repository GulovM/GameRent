-- +goose Up
CREATE UNIQUE INDEX ux_refresh_tokens_token_hash ON refresh_tokens(token_hash);

-- +goose Down
DROP INDEX IF EXISTS ux_refresh_tokens_token_hash;
