-- +goose Up
CREATE TABLE pdd_purchase_goods_snapshots (id VARCHAR(64) PRIMARY KEY,user_id BIGINT NOT NULL,task_id VARCHAR(64) NOT NULL,pdd_account_id VARCHAR(64) NOT NULL,goods_id VARCHAR(191) NOT NULL,source VARCHAR(32) NOT NULL,cache_hit TINYINT(1) NOT NULL DEFAULT 0,snapshot_json LONGTEXT NOT NULL,blocking_errors_json LONGTEXT NOT NULL,warnings_json LONGTEXT NOT NULL,captured_at BIGINT NOT NULL,KEY idx_pdd_goods_snapshot_cache(user_id,pdd_account_id,goods_id,captured_at),CONSTRAINT fk_pdd_goods_snapshot_user FOREIGN KEY(user_id) REFERENCES users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT IGNORE INTO system_settings(`key`,value,description) VALUES('pdd_product_refresh_interval_hours','72','拼多多采购商品信息刷新间隔（小时）');
-- +goose Down
DELETE FROM system_settings WHERE `key`='pdd_product_refresh_interval_hours';
DROP TABLE IF EXISTS pdd_purchase_goods_snapshots;
