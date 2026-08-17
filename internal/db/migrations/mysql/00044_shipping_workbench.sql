-- +goose Up
ALTER TABLE order_fulfillments ADD COLUMN shipping_status VARCHAR(64) NOT NULL DEFAULT 'waiting_pdd_shipping',ADD COLUMN logistics_company_code VARCHAR(64) NOT NULL DEFAULT '',ADD COLUMN logistics_synced_at BIGINT NOT NULL DEFAULT 0,ADD COLUMN shipping_attempts INT NOT NULL DEFAULT 0,ADD COLUMN shipping_last_error TEXT NOT NULL;
CREATE TABLE fulfillment_ship_operations (id VARCHAR(64) PRIMARY KEY,user_id BIGINT NOT NULL,order_id VARCHAR(255) NOT NULL,idempotency_key VARCHAR(255) NOT NULL,status VARCHAR(64) NOT NULL,request_json LONGTEXT NOT NULL,response_json LONGTEXT NOT NULL,error_message TEXT NOT NULL,created_at BIGINT NOT NULL,finished_at BIGINT NOT NULL DEFAULT 0,UNIQUE KEY uq_ship_idempotency(user_id,idempotency_key),CONSTRAINT fk_ship_user FOREIGN KEY(user_id) REFERENCES users(id));
CREATE INDEX idx_fulfillment_shipping ON order_fulfillments(user_id,shipping_status,updated_at);
-- +goose Down
DROP TABLE fulfillment_ship_operations;
DROP INDEX idx_fulfillment_shipping ON order_fulfillments;
ALTER TABLE order_fulfillments DROP COLUMN shipping_last_error,DROP COLUMN shipping_attempts,DROP COLUMN logistics_synced_at,DROP COLUMN logistics_company_code,DROP COLUMN shipping_status;
