-- +goose Up
CREATE TABLE xianyu_shipping_accounts (cookie_id TEXT PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),address_id BIGINT NOT NULL DEFAULT 0,address_summary TEXT NOT NULL DEFAULT '',verified_at BIGINT NOT NULL DEFAULT 0,created_at BIGINT NOT NULL,updated_at BIGINT NOT NULL);
-- +goose Down
DROP TABLE xianyu_shipping_accounts;
