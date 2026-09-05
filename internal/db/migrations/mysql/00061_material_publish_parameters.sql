-- +goose Up
ALTER TABLE product_materials ADD COLUMN publish_parameters_json LONGTEXT NOT NULL DEFAULT ('{}');
ALTER TABLE product_materials ADD COLUMN revision BIGINT NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE product_materials DROP COLUMN revision;
ALTER TABLE product_materials DROP COLUMN publish_parameters_json;
