-- +goose Up
CREATE TABLE pdd_account_events (id BIGINT AUTO_INCREMENT PRIMARY KEY,user_id BIGINT NOT NULL,pdd_account_id VARCHAR(64) NOT NULL,site VARCHAR(32) NOT NULL DEFAULT 'pinduoduo',operation VARCHAR(64) NOT NULL,status VARCHAR(32) NOT NULL,error_type VARCHAR(64) NOT NULL DEFAULT '',message TEXT NOT NULL,task_id VARCHAR(191) NOT NULL DEFAULT '',created_at BIGINT NOT NULL,KEY idx_pdd_account_events_account_created(pdd_account_id,created_at),KEY idx_pdd_account_events_created(created_at),CONSTRAINT fk_pdd_events_user FOREIGN KEY(user_id) REFERENCES users(id),CONSTRAINT fk_pdd_events_account FOREIGN KEY(pdd_account_id) REFERENCES pdd_accounts(id) ON DELETE CASCADE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose Down
DROP TABLE pdd_account_events;
