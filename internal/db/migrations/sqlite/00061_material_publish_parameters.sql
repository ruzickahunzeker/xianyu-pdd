-- +goose Up
ALTER TABLE product_materials ADD COLUMN publish_parameters_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE product_materials ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE product_materials DROP COLUMN revision;
ALTER TABLE product_materials DROP COLUMN publish_parameters_json;
