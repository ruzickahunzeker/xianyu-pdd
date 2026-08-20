-- +goose Up
ALTER TABLE pdd_accounts ADD COLUMN site VARCHAR(32) NOT NULL DEFAULT 'pinduoduo' AFTER name;

-- +goose Down
ALTER TABLE pdd_accounts DROP COLUMN site;
