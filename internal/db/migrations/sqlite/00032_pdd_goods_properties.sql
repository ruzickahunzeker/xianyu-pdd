-- +goose Up
ALTER TABLE pdd_products ADD COLUMN properties_json TEXT NOT NULL DEFAULT '[]';
-- +goose Down
ALTER TABLE pdd_products DROP COLUMN properties_json;
