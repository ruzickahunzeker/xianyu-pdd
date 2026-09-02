-- +goose Up
CREATE TABLE material_split_batches (id TEXT PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),source_material_id BIGINT NOT NULL,idempotency_key TEXT NOT NULL,response_json TEXT NOT NULL DEFAULT '',created_at BIGINT NOT NULL,UNIQUE(user_id,source_material_id,idempotency_key));
CREATE TABLE material_split_assignments (id BIGSERIAL PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),root_material_id BIGINT NOT NULL,child_material_id BIGINT NOT NULL,material_sku_id TEXT NOT NULL,split_batch_id TEXT NOT NULL,created_at BIGINT NOT NULL,UNIQUE(root_material_id,material_sku_id));
CREATE INDEX idx_material_split_assignment_child ON material_split_assignments(user_id,child_material_id);

-- +goose Down
DROP TABLE material_split_assignments;
DROP TABLE material_split_batches;
