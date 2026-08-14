-- +goose Up
ALTER TABLE pdd_products ADD COLUMN mall_sn TEXT NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE pdd_products DROP COLUMN mall_sn;
