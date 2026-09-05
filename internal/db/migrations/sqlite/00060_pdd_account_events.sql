-- +goose Up
CREATE TABLE pdd_account_events (id INTEGER PRIMARY KEY AUTOINCREMENT,user_id INTEGER NOT NULL,pdd_account_id TEXT NOT NULL,site TEXT NOT NULL DEFAULT 'pinduoduo',operation TEXT NOT NULL,status TEXT NOT NULL,error_type TEXT NOT NULL DEFAULT '',message TEXT NOT NULL DEFAULT '',task_id TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL,FOREIGN KEY(user_id) REFERENCES users(id),FOREIGN KEY(pdd_account_id) REFERENCES pdd_accounts(id) ON DELETE CASCADE);
CREATE INDEX idx_pdd_account_events_account_created ON pdd_account_events(pdd_account_id,created_at DESC);
CREATE INDEX idx_pdd_account_events_created ON pdd_account_events(created_at);
-- +goose Down
DROP TABLE pdd_account_events;
