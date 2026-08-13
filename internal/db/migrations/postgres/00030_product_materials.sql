-- +goose Up
CREATE TABLE product_materials (id BIGSERIAL PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),source_type TEXT NOT NULL DEFAULT '',source_id TEXT NOT NULL DEFAULT '',title TEXT NOT NULL,description TEXT NOT NULL DEFAULT '',images_json TEXT NOT NULL DEFAULT '[]',category_json TEXT NOT NULL DEFAULT '{}',skus_json TEXT NOT NULL DEFAULT '[]',postage_mode TEXT NOT NULL DEFAULT 'free',postage_cent BIGINT NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'draft',created_at BIGINT NOT NULL,updated_at BIGINT NOT NULL,deleted_at BIGINT);
CREATE INDEX idx_product_materials_user ON product_materials(user_id,deleted_at,updated_at);
-- +goose Down
DROP TABLE IF EXISTS product_materials;
