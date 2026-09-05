-- +goose Up
CREATE TABLE pdd_account_events (id BIGSERIAL PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),pdd_account_id TEXT NOT NULL REFERENCES pdd_accounts(id) ON DELETE CASCADE,site TEXT NOT NULL DEFAULT 'pinduoduo',operation TEXT NOT NULL,status TEXT NOT NULL,error_type TEXT NOT NULL DEFAULT '',message TEXT NOT NULL DEFAULT '',task_id TEXT NOT NULL DEFAULT '',created_at BIGINT NOT NULL);
CREATE INDEX idx_pdd_account_events_account_created ON pdd_account_events(pdd_account_id,created_at DESC);
CREATE INDEX idx_pdd_account_events_created ON pdd_account_events(created_at);
-- +goose Down
DROP TABLE pdd_account_events;
