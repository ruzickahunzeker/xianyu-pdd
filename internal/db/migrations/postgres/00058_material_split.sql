-- +goose Up
ALTER TABLE product_materials ADD COLUMN parent_material_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE product_materials ADD COLUMN split_batch_id TEXT NOT NULL DEFAULT '';
ALTER TABLE product_materials ADD COLUMN split_group_name TEXT NOT NULL DEFAULT '';
ALTER TABLE product_materials ADD COLUMN is_split_source INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_product_materials_parent ON product_materials(user_id,parent_material_id,deleted_at);

-- +goose Down
DROP INDEX idx_product_materials_parent;
ALTER TABLE product_materials DROP COLUMN is_split_source;
ALTER TABLE product_materials DROP COLUMN split_group_name;
ALTER TABLE product_materials DROP COLUMN split_batch_id;
ALTER TABLE product_materials DROP COLUMN parent_material_id;
