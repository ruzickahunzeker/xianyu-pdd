-- +goose Up
CREATE TABLE pdd_purchase_tasks (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  order_id TEXT NOT NULL,
  pdd_account_id TEXT NOT NULL DEFAULT '',
  attempt INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'pending',
  worker_id TEXT NOT NULL DEFAULT '',
  lease_token TEXT NOT NULL DEFAULT '',
  lease_expires_at INTEGER NOT NULL DEFAULT 0,
  expected_goods_id TEXT NOT NULL DEFAULT '',
  expected_sku_id TEXT NOT NULL DEFAULT '',
  expected_quantity INTEGER NOT NULL DEFAULT 1,
  expected_amount_cent INTEGER NOT NULL DEFAULT 0,
  expected_receiver_name TEXT NOT NULL DEFAULT '',
  expected_province TEXT NOT NULL DEFAULT '',
  expected_city TEXT NOT NULL DEFAULT '',
  expected_district TEXT NOT NULL DEFAULT '',
  expected_detail_address TEXT NOT NULL DEFAULT '',
  before_order_sns TEXT NOT NULL DEFAULT '[]',
  pdd_order_id TEXT,
  pdd_order_json TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  started_at INTEGER NOT NULL DEFAULT 0,
  submitted_at INTEGER NOT NULL DEFAULT 0,
  finished_at INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  UNIQUE(user_id, order_id, attempt),
  FOREIGN KEY(user_id) REFERENCES users(id)
);
CREATE UNIQUE INDEX uq_pdd_purchase_order_id ON pdd_purchase_tasks(pdd_order_id) WHERE pdd_order_id IS NOT NULL;
CREATE INDEX idx_pdd_purchase_claim ON pdd_purchase_tasks(user_id,status,lease_expires_at,created_at);

CREATE TABLE fulfillment_exception_events (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  order_id TEXT NOT NULL,
  task_id TEXT NOT NULL DEFAULT '',
  event_type TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  detail_json TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open',
  notification_status TEXT NOT NULL DEFAULT 'pending',
  created_at INTEGER NOT NULL,
  resolved_at INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY(user_id) REFERENCES users(id)
);
CREATE INDEX idx_fulfillment_exception_open ON fulfillment_exception_events(user_id,status,created_at);

-- +goose Down
DROP TABLE IF EXISTS fulfillment_exception_events;
DROP TABLE IF EXISTS pdd_purchase_tasks;
