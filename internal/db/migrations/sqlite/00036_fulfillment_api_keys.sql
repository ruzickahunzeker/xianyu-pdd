-- +goose Up
CREATE TABLE fulfillment_api_keys (id TEXT PRIMARY KEY,user_id INTEGER NOT NULL,name TEXT NOT NULL,token_hash TEXT NOT NULL UNIQUE,enabled INTEGER NOT NULL DEFAULT 1,last_used_at INTEGER NOT NULL DEFAULT 0,created_at INTEGER NOT NULL,FOREIGN KEY(user_id) REFERENCES users(id));
CREATE INDEX idx_fulfillment_api_keys_user ON fulfillment_api_keys(user_id,created_at);
-- +goose Down
DROP TABLE fulfillment_api_keys;
