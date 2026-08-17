-- +goose Up
CREATE TABLE pdd_accounts (id VARCHAR(64) PRIMARY KEY,user_id BIGINT NOT NULL,name VARCHAR(191) NOT NULL DEFAULT '拼多多主账号',cookie_encrypted TEXT NOT NULL,pdd_uid VARCHAR(191) NOT NULL DEFAULT '',default_address_id VARCHAR(191) NOT NULL DEFAULT '',user_agent TEXT NOT NULL,enabled TINYINT NOT NULL DEFAULT 1,is_default TINYINT NOT NULL DEFAULT 1,credential_status VARCHAR(32) NOT NULL DEFAULT 'unchecked',last_verified_at BIGINT NOT NULL DEFAULT 0,last_error TEXT NOT NULL,created_at BIGINT NOT NULL,updated_at BIGINT NOT NULL,KEY idx_pdd_accounts_user_default(user_id,is_default),CONSTRAINT fk_pdd_accounts_user FOREIGN KEY(user_id) REFERENCES users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
ALTER TABLE order_fulfillments ADD COLUMN pdd_account_id VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE pdd_address_operations ADD COLUMN pdd_account_id VARCHAR(64) NOT NULL DEFAULT '';
CREATE TABLE pdd_account_locks (pdd_account_id VARCHAR(64) PRIMARY KEY,user_id BIGINT NOT NULL,order_id VARCHAR(191) NOT NULL,operation_id VARCHAR(64) NOT NULL,locked_at BIGINT NOT NULL,expires_at BIGINT NOT NULL,CONSTRAINT fk_pdd_account_locks_user FOREIGN KEY(user_id) REFERENCES users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose Down
DROP TABLE pdd_account_locks;
ALTER TABLE pdd_address_operations DROP COLUMN pdd_account_id;
ALTER TABLE order_fulfillments DROP COLUMN pdd_account_id;
DROP TABLE pdd_accounts;
