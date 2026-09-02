-- +goose Up
CREATE TABLE material_split_batches (id VARCHAR(64) PRIMARY KEY,user_id BIGINT NOT NULL,source_material_id BIGINT NOT NULL,idempotency_key VARCHAR(191) NOT NULL,response_json LONGTEXT NOT NULL,created_at BIGINT NOT NULL,UNIQUE KEY uq_material_split_batch(user_id,source_material_id,idempotency_key),CONSTRAINT fk_material_split_batch_user FOREIGN KEY(user_id) REFERENCES users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE material_split_assignments (id BIGINT AUTO_INCREMENT PRIMARY KEY,user_id BIGINT NOT NULL,root_material_id BIGINT NOT NULL,child_material_id BIGINT NOT NULL,material_sku_id VARCHAR(191) NOT NULL,split_batch_id VARCHAR(64) NOT NULL,created_at BIGINT NOT NULL,UNIQUE KEY uq_material_split_assignment(root_material_id,material_sku_id),KEY idx_material_split_assignment_child(user_id,child_material_id),CONSTRAINT fk_material_split_assignment_user FOREIGN KEY(user_id) REFERENCES users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE material_split_assignments;
DROP TABLE material_split_batches;
