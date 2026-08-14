-- +goose Up
ALTER TABLE material_publish_sku_mappings ADD COLUMN source_goods_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_material_publish_sku_source ON material_publish_sku_mappings(source_goods_id,source_sku_id);
-- +goose Down
DROP INDEX IF EXISTS idx_material_publish_sku_source;
ALTER TABLE material_publish_sku_mappings DROP COLUMN source_goods_id;
