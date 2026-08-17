-- +goose Up
CREATE TABLE xianyu_shipping_addresses (cookie_id VARCHAR(191) NOT NULL,user_id BIGINT NOT NULL,contact_id BIGINT NOT NULL,area_id BIGINT NOT NULL DEFAULT 0,contact_name VARCHAR(191) NOT NULL DEFAULT '',mobile_phone VARCHAR(64) NOT NULL DEFAULT '',province_name VARCHAR(191) NOT NULL DEFAULT '',city_name VARCHAR(191) NOT NULL DEFAULT '',district_name VARCHAR(191) NOT NULL DEFAULT '',detail_address TEXT NOT NULL,platform_default INTEGER NOT NULL DEFAULT 0,last_synced_at BIGINT NOT NULL,PRIMARY KEY(cookie_id,contact_id),CONSTRAINT fk_xianyu_shipping_address_user FOREIGN KEY(user_id) REFERENCES users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose Down
DROP TABLE xianyu_shipping_addresses;
