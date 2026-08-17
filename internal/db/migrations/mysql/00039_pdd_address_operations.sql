-- +goose Up
CREATE TABLE pdd_address_operations (id VARCHAR(64) PRIMARY KEY,user_id BIGINT NOT NULL,order_id VARCHAR(191) NOT NULL,idempotency_key VARCHAR(128) NOT NULL,status VARCHAR(32) NOT NULL,pdd_address_id VARCHAR(191) NOT NULL DEFAULT '',target_json TEXT NOT NULL,http_status INTEGER NOT NULL DEFAULT 0,response_json TEXT NOT NULL,error_message TEXT NOT NULL,created_at BIGINT NOT NULL,finished_at BIGINT NOT NULL DEFAULT 0,UNIQUE KEY uq_pdd_address_operation_key(user_id,idempotency_key),KEY idx_pdd_address_operations_order(user_id,order_id,created_at),CONSTRAINT fk_pdd_address_operations_user FOREIGN KEY(user_id) REFERENCES users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose Down
DROP TABLE pdd_address_operations;
