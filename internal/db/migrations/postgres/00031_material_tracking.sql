-- +goose Up
ALTER TABLE product_materials ADD COLUMN image_property_name TEXT NOT NULL DEFAULT '';
CREATE TABLE material_publish_records (id BIGSERIAL PRIMARY KEY,request_id TEXT NOT NULL UNIQUE,material_id BIGINT NOT NULL REFERENCES product_materials(id),user_id BIGINT NOT NULL REFERENCES users(id),source_type TEXT NOT NULL DEFAULT '',source_id TEXT NOT NULL DEFAULT '',cookie_id TEXT NOT NULL,published_item_id TEXT NOT NULL DEFAULT '',status TEXT NOT NULL,error_code TEXT NOT NULL DEFAULT '',error_message TEXT NOT NULL DEFAULT '',sku_snapshot_json TEXT NOT NULL DEFAULT '[]',created_at BIGINT NOT NULL,finished_at BIGINT NOT NULL DEFAULT 0);
CREATE INDEX idx_material_publish_records_material ON material_publish_records(material_id,created_at);
CREATE TABLE material_publish_sku_mappings (id BIGSERIAL PRIMARY KEY,publish_record_id BIGINT NOT NULL REFERENCES material_publish_records(id) ON DELETE CASCADE,material_sku_id TEXT NOT NULL,source_sku_id TEXT NOT NULL DEFAULT '',xianyu_sku_id TEXT NOT NULL DEFAULT '',published_properties_json TEXT NOT NULL DEFAULT '[]',published_price_cent BIGINT NOT NULL,published_quantity BIGINT NOT NULL,mapping_status TEXT NOT NULL DEFAULT 'pending',UNIQUE(publish_record_id,material_sku_id));
-- +goose Down
DROP TABLE IF EXISTS material_publish_sku_mappings;
DROP TABLE IF EXISTS material_publish_records;
