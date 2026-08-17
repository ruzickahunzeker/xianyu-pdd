-- +goose Up
CREATE TABLE xianyu_shipping_accounts (cookie_id VARCHAR(255) PRIMARY KEY,user_id BIGINT NOT NULL,address_id BIGINT NOT NULL DEFAULT 0,address_summary TEXT NOT NULL,verified_at BIGINT NOT NULL DEFAULT 0,created_at BIGINT NOT NULL,updated_at BIGINT NOT NULL,CONSTRAINT fk_shipping_account_user FOREIGN KEY(user_id) REFERENCES users(id));
-- +goose Down
DROP TABLE xianyu_shipping_accounts;
