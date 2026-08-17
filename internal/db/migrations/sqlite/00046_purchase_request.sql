-- +goose Up
ALTER TABLE order_fulfillments ADD COLUMN purchase_requested_at INTEGER NOT NULL DEFAULT 0;
-- +goose Down
ALTER TABLE order_fulfillments DROP COLUMN purchase_requested_at;
