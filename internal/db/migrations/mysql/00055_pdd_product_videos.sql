-- +goose Up
ALTER TABLE pdd_products ADD COLUMN videos_json LONGTEXT NULL;
UPDATE pdd_products SET videos_json='[]' WHERE videos_json IS NULL OR videos_json='';
ALTER TABLE pdd_products MODIFY COLUMN videos_json LONGTEXT NOT NULL;
-- +goose Down
ALTER TABLE pdd_products DROP COLUMN videos_json;
