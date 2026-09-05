-- +goose Up
CREATE TABLE scheduled_publish_batches (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    account_id TEXT NOT NULL,
    start_at INTEGER NOT NULL,
    interval_min_seconds INTEGER NOT NULL DEFAULT 300,
    interval_max_seconds INTEGER NOT NULL DEFAULT 600,
    minimum_profit_cent INTEGER NOT NULL DEFAULT 30,
    location_json TEXT NOT NULL DEFAULT '{}',
    task_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_scheduled_publish_batches_user ON scheduled_publish_batches(user_id, status, created_at);

CREATE TABLE scheduled_publish_tasks (
    id TEXT PRIMARY KEY,
    batch_id TEXT NOT NULL REFERENCES scheduled_publish_batches(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL,
    material_id INTEGER NOT NULL REFERENCES product_materials(id),
    material_revision INTEGER NOT NULL,
    account_id TEXT NOT NULL,
    sequence_no INTEGER NOT NULL,
    planned_at INTEGER NOT NULL,
    interval_seconds INTEGER NOT NULL DEFAULT 0,
    not_before INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    idempotency_key TEXT NOT NULL UNIQUE,
    snapshot_json TEXT NOT NULL,
    minimum_profit_cent INTEGER NOT NULL DEFAULT 30,
    minimum_actual_profit_cent INTEGER NOT NULL DEFAULT 0,
    published_item_id TEXT NOT NULL DEFAULT '',
    error_stage TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    account_queue_paused INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    started_at INTEGER NOT NULL DEFAULT 0,
    finished_at INTEGER NOT NULL DEFAULT 0,
    UNIQUE(batch_id, sequence_no)
);
CREATE INDEX idx_scheduled_publish_tasks_due ON scheduled_publish_tasks(status, not_before, account_id);
CREATE INDEX idx_scheduled_publish_tasks_material ON scheduled_publish_tasks(user_id, material_id, status);

-- +goose Down
DROP TABLE IF EXISTS scheduled_publish_tasks;
DROP TABLE IF EXISTS scheduled_publish_batches;
