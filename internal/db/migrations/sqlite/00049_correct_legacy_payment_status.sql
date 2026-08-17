-- +goose Up
UPDATE order_fulfillments SET pdd_paid=0,pdd_paid_at=0,pdd_paid_source='' WHERE pdd_paid_source='legacy' AND pdd_shipped=0;
UPDATE order_fulfillments SET pdd_paid=1,pdd_paid_at=COALESCE(NULLIF(pdd_shipped_at,0),updated_at),pdd_paid_source='inferred_shipping' WHERE pdd_paid_source='legacy' AND pdd_shipped=1;

-- +goose Down
UPDATE order_fulfillments SET pdd_paid_source='legacy' WHERE pdd_paid_source='inferred_shipping';
