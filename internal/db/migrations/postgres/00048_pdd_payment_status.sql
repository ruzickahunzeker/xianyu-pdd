-- +goose Up
ALTER TABLE order_fulfillments ADD COLUMN pdd_paid INTEGER NOT NULL DEFAULT 0;
ALTER TABLE order_fulfillments ADD COLUMN pdd_paid_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE order_fulfillments ADD COLUMN pdd_paid_source TEXT NOT NULL DEFAULT '';
UPDATE order_fulfillments SET pdd_paid=CASE WHEN pdd_shipped=1 THEN 1 ELSE 0 END,pdd_paid_at=CASE WHEN pdd_shipped=1 THEN COALESCE(NULLIF(pdd_shipped_at,0),updated_at) ELSE 0 END,pdd_paid_source=CASE WHEN pdd_shipped=1 THEN 'inferred_shipping' ELSE '' END;
UPDATE order_fulfillments SET pdd_ordered=1,pdd_ordered_at=COALESCE(NULLIF(pdd_ordered_at,0),updated_at) WHERE pdd_order_id<>'';
CREATE INDEX idx_order_fulfillments_paid ON order_fulfillments(user_id,pdd_paid,pdd_shipped);
-- +goose Down
DROP INDEX idx_order_fulfillments_paid;
ALTER TABLE order_fulfillments DROP COLUMN pdd_paid_source;
ALTER TABLE order_fulfillments DROP COLUMN pdd_paid_at;
ALTER TABLE order_fulfillments DROP COLUMN pdd_paid;
