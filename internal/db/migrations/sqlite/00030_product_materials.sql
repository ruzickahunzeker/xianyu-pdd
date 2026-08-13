-- +goose Up
CREATE TABLE product_materials (id INTEGER PRIMARY KEY AUTOINCREMENT,user_id INTEGER NOT NULL,source_type TEXT NOT NULL DEFAULT '',source_id TEXT NOT NULL DEFAULT '',title TEXT NOT NULL,description TEXT NOT NULL DEFAULT '',images_json TEXT NOT NULL DEFAULT '[]',category_json TEXT NOT NULL DEFAULT '{}',skus_json TEXT NOT NULL DEFAULT '[]',postage_mode TEXT NOT NULL DEFAULT 'free',postage_cent INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'draft',created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,deleted_at INTEGER,FOREIGN KEY(user_id) REFERENCES users(id));
CREATE INDEX idx_product_materials_user ON product_materials(user_id,deleted_at,updated_at);
-- +goose Down
DROP TABLE IF EXISTS product_materials;
