-- +goose Up
CREATE TABLE xianyu_shipping_addresses (cookie_id TEXT NOT NULL,user_id BIGINT NOT NULL REFERENCES users(id),contact_id BIGINT NOT NULL,area_id BIGINT NOT NULL DEFAULT 0,contact_name TEXT NOT NULL DEFAULT '',mobile_phone TEXT NOT NULL DEFAULT '',province_name TEXT NOT NULL DEFAULT '',city_name TEXT NOT NULL DEFAULT '',district_name TEXT NOT NULL DEFAULT '',detail_address TEXT NOT NULL DEFAULT '',platform_default INTEGER NOT NULL DEFAULT 0,last_synced_at BIGINT NOT NULL,PRIMARY KEY(cookie_id,contact_id));
-- +goose Down
DROP TABLE xianyu_shipping_addresses;
