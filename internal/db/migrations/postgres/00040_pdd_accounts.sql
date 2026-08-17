-- +goose Up
CREATE TABLE pdd_accounts (id TEXT PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),name TEXT NOT NULL DEFAULT '拼多多主账号',cookie_encrypted TEXT NOT NULL DEFAULT '',pdd_uid TEXT NOT NULL DEFAULT '',default_address_id TEXT NOT NULL DEFAULT '',user_agent TEXT NOT NULL DEFAULT '',enabled INTEGER NOT NULL DEFAULT 1,is_default INTEGER NOT NULL DEFAULT 1,credential_status TEXT NOT NULL DEFAULT 'unchecked',last_verified_at BIGINT NOT NULL DEFAULT 0,last_error TEXT NOT NULL DEFAULT '',created_at BIGINT NOT NULL,updated_at BIGINT NOT NULL);
CREATE INDEX idx_pdd_accounts_user_default ON pdd_accounts(user_id,is_default);
ALTER TABLE order_fulfillments ADD COLUMN pdd_account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pdd_address_operations ADD COLUMN pdd_account_id TEXT NOT NULL DEFAULT '';
CREATE TABLE pdd_account_locks (pdd_account_id TEXT PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),order_id TEXT NOT NULL,operation_id TEXT NOT NULL,locked_at BIGINT NOT NULL,expires_at BIGINT NOT NULL);
-- +goose Down
DROP TABLE pdd_account_locks;
ALTER TABLE pdd_address_operations DROP COLUMN pdd_account_id;
ALTER TABLE order_fulfillments DROP COLUMN pdd_account_id;
DROP TABLE pdd_accounts;
