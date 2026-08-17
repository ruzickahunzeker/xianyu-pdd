-- +goose Up
ALTER TABLE order_fulfillments ADD COLUMN shipping_status TEXT NOT NULL DEFAULT 'waiting_pdd_shipping';
ALTER TABLE order_fulfillments ADD COLUMN logistics_company_code TEXT NOT NULL DEFAULT '';
ALTER TABLE order_fulfillments ADD COLUMN logistics_synced_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE order_fulfillments ADD COLUMN shipping_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE order_fulfillments ADD COLUMN shipping_last_error TEXT NOT NULL DEFAULT '';
CREATE TABLE fulfillment_ship_operations (id TEXT PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),order_id TEXT NOT NULL,idempotency_key TEXT NOT NULL,status TEXT NOT NULL,request_json TEXT NOT NULL DEFAULT '',response_json TEXT NOT NULL DEFAULT '',error_message TEXT NOT NULL DEFAULT '',created_at BIGINT NOT NULL,finished_at BIGINT NOT NULL DEFAULT 0,UNIQUE(user_id,idempotency_key));
CREATE INDEX idx_fulfillment_shipping ON order_fulfillments(user_id,shipping_status,updated_at);
-- +goose Down
DROP TABLE fulfillment_ship_operations;
DROP INDEX idx_fulfillment_shipping;
ALTER TABLE order_fulfillments DROP COLUMN shipping_last_error;
ALTER TABLE order_fulfillments DROP COLUMN shipping_attempts;
ALTER TABLE order_fulfillments DROP COLUMN logistics_synced_at;
ALTER TABLE order_fulfillments DROP COLUMN logistics_company_code;
ALTER TABLE order_fulfillments DROP COLUMN shipping_status;
