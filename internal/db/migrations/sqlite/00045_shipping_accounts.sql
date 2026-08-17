-- +goose Up
CREATE TABLE xianyu_shipping_accounts (cookie_id TEXT PRIMARY KEY,user_id INTEGER NOT NULL REFERENCES users(id),address_id INTEGER NOT NULL DEFAULT 0,address_summary TEXT NOT NULL DEFAULT '',verified_at INTEGER NOT NULL DEFAULT 0,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
-- +goose Down
DROP TABLE xianyu_shipping_accounts;
