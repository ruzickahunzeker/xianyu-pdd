-- +goose Up
CREATE TABLE xianyu_shipping_addresses (cookie_id TEXT NOT NULL,user_id INTEGER NOT NULL,contact_id INTEGER NOT NULL,area_id INTEGER NOT NULL DEFAULT 0,contact_name TEXT NOT NULL DEFAULT '',mobile_phone TEXT NOT NULL DEFAULT '',province_name TEXT NOT NULL DEFAULT '',city_name TEXT NOT NULL DEFAULT '',district_name TEXT NOT NULL DEFAULT '',detail_address TEXT NOT NULL DEFAULT '',platform_default INTEGER NOT NULL DEFAULT 0,last_synced_at INTEGER NOT NULL,PRIMARY KEY(cookie_id,contact_id),FOREIGN KEY(user_id) REFERENCES users(id));
-- +goose Down
DROP TABLE xianyu_shipping_addresses;
