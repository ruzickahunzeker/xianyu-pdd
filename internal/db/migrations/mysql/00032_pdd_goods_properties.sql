-- +goose Up
ALTER TABLE pdd_products ADD COLUMN properties_json LONGTEXT NULL;
UPDATE pdd_products SET properties_json='[]' WHERE properties_json IS NULL;
ALTER TABLE pdd_products MODIFY COLUMN properties_json LONGTEXT NOT NULL;
-- +goose Down
ALTER TABLE pdd_products DROP COLUMN properties_json;
