-- +goose Up
ALTER TABLE pdd_products ADD COLUMN gallery_images_json LONGTEXT NOT NULL DEFAULT ('[]');
ALTER TABLE pdd_products ADD COLUMN detail_images_json LONGTEXT NOT NULL DEFAULT ('[]');

-- +goose Down
ALTER TABLE pdd_products DROP COLUMN detail_images_json;
ALTER TABLE pdd_products DROP COLUMN gallery_images_json;
