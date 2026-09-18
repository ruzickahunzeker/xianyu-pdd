-- +goose Up
CREATE TABLE pdd_logistics_sync_tasks (id TEXT PRIMARY KEY,user_id BIGINT NOT NULL,order_id TEXT NOT NULL,pdd_order_id TEXT NOT NULL,pdd_account_id TEXT NOT NULL,source TEXT NOT NULL DEFAULT 'single',status TEXT NOT NULL DEFAULT 'queued',scheduled_at BIGINT NOT NULL,worker_id TEXT NOT NULL DEFAULT '',lease_token TEXT NOT NULL DEFAULT '',lease_expires_at BIGINT NOT NULL DEFAULT 0,result_json TEXT NOT NULL DEFAULT '{}',last_error TEXT NOT NULL DEFAULT '',active_marker BIGINT DEFAULT 1,created_at BIGINT NOT NULL,started_at BIGINT NOT NULL DEFAULT 0,finished_at BIGINT NOT NULL DEFAULT 0,updated_at BIGINT NOT NULL,UNIQUE(user_id,order_id,active_marker));
CREATE INDEX idx_pdd_logistics_sync_due ON pdd_logistics_sync_tasks(user_id,status,scheduled_at);
CREATE INDEX idx_pdd_logistics_sync_order ON pdd_logistics_sync_tasks(user_id,order_id,created_at);
-- +goose Down
DROP TABLE IF EXISTS pdd_logistics_sync_tasks;
