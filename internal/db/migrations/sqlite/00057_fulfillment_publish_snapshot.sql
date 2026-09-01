-- +goose Up
ALTER TABLE order_fulfillments ADD COLUMN publish_record_id INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE order_fulfillments DROP COLUMN publish_record_id;
