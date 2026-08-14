-- +goose Up
ALTER TABLE material_publish_sku_mappings ADD COLUMN source_goods_id VARCHAR(191) NOT NULL DEFAULT '' AFTER material_sku_id;
CREATE INDEX idx_material_publish_sku_source ON material_publish_sku_mappings(source_goods_id,source_sku_id);
-- +goose Down
DROP INDEX idx_material_publish_sku_source ON material_publish_sku_mappings;
ALTER TABLE material_publish_sku_mappings DROP COLUMN source_goods_id;
