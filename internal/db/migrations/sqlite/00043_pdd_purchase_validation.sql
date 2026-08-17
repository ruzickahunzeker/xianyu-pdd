-- +goose Up
CREATE TABLE pdd_purchase_goods_snapshots (id TEXT PRIMARY KEY,user_id INTEGER NOT NULL,task_id TEXT NOT NULL,pdd_account_id TEXT NOT NULL,goods_id TEXT NOT NULL,source TEXT NOT NULL,cache_hit INTEGER NOT NULL DEFAULT 0,snapshot_json TEXT NOT NULL,blocking_errors_json TEXT NOT NULL DEFAULT '[]',warnings_json TEXT NOT NULL DEFAULT '[]',captured_at INTEGER NOT NULL,FOREIGN KEY(user_id) REFERENCES users(id));
CREATE INDEX idx_pdd_goods_snapshot_cache ON pdd_purchase_goods_snapshots(user_id,pdd_account_id,goods_id,captured_at);
INSERT OR IGNORE INTO system_settings(key,value,description) VALUES('pdd_product_refresh_interval_hours','72','拼多多采购商品信息刷新间隔（小时）');
-- +goose Down
DELETE FROM system_settings WHERE key='pdd_product_refresh_interval_hours';
DROP TABLE IF EXISTS pdd_purchase_goods_snapshots;
