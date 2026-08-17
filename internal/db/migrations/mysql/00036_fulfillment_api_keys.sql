-- +goose Up
CREATE TABLE fulfillment_api_keys (id VARCHAR(64) PRIMARY KEY,user_id BIGINT NOT NULL,name VARCHAR(191) NOT NULL,token_hash CHAR(64) NOT NULL UNIQUE,enabled TINYINT NOT NULL DEFAULT 1,last_used_at BIGINT NOT NULL DEFAULT 0,created_at BIGINT NOT NULL,KEY idx_fulfillment_api_keys_user(user_id,created_at),CONSTRAINT fk_fulfillment_api_keys_user FOREIGN KEY(user_id) REFERENCES users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose Down
DROP TABLE fulfillment_api_keys;
