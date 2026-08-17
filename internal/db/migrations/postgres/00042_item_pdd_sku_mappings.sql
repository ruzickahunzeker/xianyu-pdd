-- +goose Up
CREATE TABLE item_pdd_sku_mappings (id BIGSERIAL PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),cookie_id TEXT NOT NULL,item_id TEXT NOT NULL,xianyu_sku_id TEXT NOT NULL DEFAULT '',source_goods_id TEXT NOT NULL,source_sku_id TEXT NOT NULL,xianyu_properties_json TEXT NOT NULL DEFAULT '[]',mapping_source TEXT NOT NULL DEFAULT 'manual',created_at BIGINT NOT NULL,updated_at BIGINT NOT NULL,UNIQUE(user_id,cookie_id,item_id,xianyu_sku_id));
CREATE INDEX idx_item_pdd_mapping_item ON item_pdd_sku_mappings(user_id,cookie_id,item_id);
CREATE TABLE item_pdd_sku_mapping_audits (id TEXT PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),cookie_id TEXT NOT NULL,item_id TEXT NOT NULL,xianyu_sku_id TEXT NOT NULL DEFAULT '',action TEXT NOT NULL,old_mapping_json TEXT NOT NULL DEFAULT '',new_mapping_json TEXT NOT NULL DEFAULT '',created_at BIGINT NOT NULL);
CREATE INDEX idx_item_pdd_mapping_audit_item ON item_pdd_sku_mapping_audits(user_id,cookie_id,item_id,created_at);
-- +goose Down
DROP TABLE IF EXISTS item_pdd_sku_mapping_audits;
DROP TABLE IF EXISTS item_pdd_sku_mappings;
