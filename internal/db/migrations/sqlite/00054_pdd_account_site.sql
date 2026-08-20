-- +goose Up
ALTER TABLE pdd_accounts ADD COLUMN site TEXT NOT NULL DEFAULT 'pinduoduo';

-- +goose Down
ALTER TABLE pdd_accounts DROP COLUMN site;
