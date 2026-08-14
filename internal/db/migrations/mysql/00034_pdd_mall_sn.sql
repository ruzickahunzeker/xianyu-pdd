-- +goose Up
ALTER TABLE pdd_products ADD COLUMN mall_sn VARCHAR(2048) NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE pdd_products DROP COLUMN mall_sn;
