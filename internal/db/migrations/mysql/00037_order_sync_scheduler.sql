-- +goose Up
INSERT IGNORE INTO system_settings(`key`,value,description) VALUES('order_sync_enabled','false','是否启用自动订单同步');
INSERT IGNORE INTO system_settings(`key`,value,description) VALUES('order_sync_interval_minutes','10','自动订单同步间隔（分钟）');
CREATE TABLE order_sync_runs (id BIGINT AUTO_INCREMENT PRIMARY KEY,user_id BIGINT NOT NULL,trigger_type VARCHAR(32) NOT NULL,status VARCHAR(32) NOT NULL,started_at BIGINT NOT NULL,finished_at BIGINT NOT NULL DEFAULT 0,discovered INT NOT NULL DEFAULT 0,updated INT NOT NULL DEFAULT 0,soft_deleted INT NOT NULL DEFAULT 0,fulfillment_updated INT NOT NULL DEFAULT 0,failed INT NOT NULL DEFAULT 0,error_message TEXT NOT NULL,KEY idx_order_sync_runs_user(user_id,started_at),CONSTRAINT fk_order_sync_runs_user FOREIGN KEY(user_id) REFERENCES users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose Down
DROP TABLE order_sync_runs;
DELETE FROM system_settings WHERE `key` IN ('order_sync_enabled','order_sync_interval_minutes');
