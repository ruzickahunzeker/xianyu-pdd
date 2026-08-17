-- +goose Up
ALTER TABLE fulfillment_exception_events ADD COLUMN read_at BIGINT NOT NULL DEFAULT 0;
CREATE INDEX idx_fulfillment_exception_unread ON fulfillment_exception_events(user_id,read_at,created_at);

-- +goose Down
DROP INDEX idx_fulfillment_exception_unread ON fulfillment_exception_events;
ALTER TABLE fulfillment_exception_events DROP COLUMN read_at;
