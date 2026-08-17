-- +goose Up
INSERT INTO system_settings(key,value,description) VALUES('order_sync_enabled','false','是否启用自动订单同步') ON CONFLICT (key) DO NOTHING;
INSERT INTO system_settings(key,value,description) VALUES('order_sync_interval_minutes','10','自动订单同步间隔（分钟）') ON CONFLICT (key) DO NOTHING;
CREATE TABLE order_sync_runs (id BIGSERIAL PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),trigger_type TEXT NOT NULL,status TEXT NOT NULL,started_at BIGINT NOT NULL,finished_at BIGINT NOT NULL DEFAULT 0,discovered INTEGER NOT NULL DEFAULT 0,updated INTEGER NOT NULL DEFAULT 0,soft_deleted INTEGER NOT NULL DEFAULT 0,fulfillment_updated INTEGER NOT NULL DEFAULT 0,failed INTEGER NOT NULL DEFAULT 0,error_message TEXT NOT NULL DEFAULT '');
CREATE INDEX idx_order_sync_runs_user ON order_sync_runs(user_id,started_at DESC);
-- +goose Down
DROP TABLE order_sync_runs;
DELETE FROM system_settings WHERE key IN ('order_sync_enabled','order_sync_interval_minutes');
