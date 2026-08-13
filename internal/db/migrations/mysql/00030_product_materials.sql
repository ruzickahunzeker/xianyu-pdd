-- +goose Up
CREATE TABLE product_materials (id BIGINT AUTO_INCREMENT PRIMARY KEY,user_id BIGINT NOT NULL,source_type VARCHAR(32) NOT NULL DEFAULT '',source_id VARCHAR(191) NOT NULL DEFAULT '',title TEXT NOT NULL,description TEXT NOT NULL,images_json LONGTEXT NOT NULL,category_json LONGTEXT NOT NULL,skus_json LONGTEXT NOT NULL,postage_mode VARCHAR(32) NOT NULL DEFAULT 'free',postage_cent BIGINT NOT NULL DEFAULT 0,status VARCHAR(32) NOT NULL DEFAULT 'draft',created_at BIGINT NOT NULL,updated_at BIGINT NOT NULL,deleted_at BIGINT,KEY idx_product_materials_user(user_id,deleted_at,updated_at),CONSTRAINT fk_product_materials_user FOREIGN KEY(user_id) REFERENCES users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose Down
DROP TABLE IF EXISTS product_materials;
