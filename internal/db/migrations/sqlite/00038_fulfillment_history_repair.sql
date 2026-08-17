-- +goose Up
ALTER TABLE order_fulfillments ADD COLUMN fulfillment_exempt INTEGER NOT NULL DEFAULT 0;
ALTER TABLE order_fulfillments ADD COLUMN reminder_exempt INTEGER NOT NULL DEFAULT 0;
ALTER TABLE order_fulfillments ADD COLUMN manual_modified_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE order_fulfillments ADD COLUMN manual_modified_fields TEXT NOT NULL DEFAULT '';
ALTER TABLE order_fulfillments ADD COLUMN history_repaired_at INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE order_fulfillments DROP COLUMN history_repaired_at;
ALTER TABLE order_fulfillments DROP COLUMN manual_modified_fields;
ALTER TABLE order_fulfillments DROP COLUMN manual_modified_at;
ALTER TABLE order_fulfillments DROP COLUMN reminder_exempt;
ALTER TABLE order_fulfillments DROP COLUMN fulfillment_exempt;
