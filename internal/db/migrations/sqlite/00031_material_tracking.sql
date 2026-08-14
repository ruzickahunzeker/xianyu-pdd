-- +goose Up
ALTER TABLE product_materials ADD COLUMN image_property_name TEXT NOT NULL DEFAULT '';
CREATE TABLE material_publish_records (id INTEGER PRIMARY KEY AUTOINCREMENT,request_id TEXT NOT NULL UNIQUE,material_id INTEGER NOT NULL REFERENCES product_materials(id),user_id INTEGER NOT NULL REFERENCES users(id),source_type TEXT NOT NULL DEFAULT '',source_id TEXT NOT NULL DEFAULT '',cookie_id TEXT NOT NULL,published_item_id TEXT NOT NULL DEFAULT '',status TEXT NOT NULL,error_code TEXT NOT NULL DEFAULT '',error_message TEXT NOT NULL DEFAULT '',sku_snapshot_json TEXT NOT NULL DEFAULT '[]',created_at INTEGER NOT NULL,finished_at INTEGER NOT NULL DEFAULT 0);
CREATE INDEX idx_material_publish_records_material ON material_publish_records(material_id,created_at);
CREATE TABLE material_publish_sku_mappings (id INTEGER PRIMARY KEY AUTOINCREMENT,publish_record_id INTEGER NOT NULL REFERENCES material_publish_records(id) ON DELETE CASCADE,material_sku_id TEXT NOT NULL,source_sku_id TEXT NOT NULL DEFAULT '',xianyu_sku_id TEXT NOT NULL DEFAULT '',published_properties_json TEXT NOT NULL DEFAULT '[]',published_price_cent INTEGER NOT NULL,published_quantity INTEGER NOT NULL,mapping_status TEXT NOT NULL DEFAULT 'pending',UNIQUE(publish_record_id,material_sku_id));
-- +goose Down
DROP TABLE IF EXISTS material_publish_sku_mappings;
DROP TABLE IF EXISTS material_publish_records;
