-- +goose Up
CREATE TABLE pdd_address_operations (id TEXT PRIMARY KEY,user_id INTEGER NOT NULL,order_id TEXT NOT NULL,idempotency_key TEXT NOT NULL,status TEXT NOT NULL,pdd_address_id TEXT NOT NULL DEFAULT '',target_json TEXT NOT NULL DEFAULT '',http_status INTEGER NOT NULL DEFAULT 0,response_json TEXT NOT NULL DEFAULT '',error_message TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL,finished_at INTEGER NOT NULL DEFAULT 0,UNIQUE(user_id,idempotency_key),FOREIGN KEY(user_id) REFERENCES users(id));
CREATE INDEX idx_pdd_address_operations_order ON pdd_address_operations(user_id,order_id,created_at);
-- +goose Down
DROP TABLE pdd_address_operations;
