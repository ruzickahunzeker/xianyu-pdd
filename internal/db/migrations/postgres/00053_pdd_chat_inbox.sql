-- +goose Up
CREATE TABLE pdd_chat_conversations (id TEXT PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),pdd_account_id TEXT NOT NULL,mall_sn TEXT NOT NULL,mall_id TEXT NOT NULL DEFAULT '',goods_id TEXT NOT NULL DEFAULT '',pdd_order_id TEXT NOT NULL DEFAULT '',page_url TEXT NOT NULL DEFAULT '',last_message_id TEXT NOT NULL DEFAULT '',last_sync_at BIGINT NOT NULL DEFAULT 0,titan_wakeup_at BIGINT NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'active',created_at BIGINT NOT NULL,updated_at BIGINT NOT NULL,UNIQUE(user_id,pdd_account_id,mall_sn));
CREATE INDEX idx_pdd_chat_conversations_sync ON pdd_chat_conversations(user_id,status,last_sync_at);
CREATE TABLE pdd_chat_messages (id TEXT PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),conversation_id TEXT NOT NULL REFERENCES pdd_chat_conversations(id),platform_message_id TEXT NOT NULL,direction TEXT NOT NULL DEFAULT 'unknown',message_type TEXT NOT NULL DEFAULT 'unknown',content TEXT NOT NULL DEFAULT '',platform_created_at BIGINT NOT NULL DEFAULT 0,raw_json TEXT NOT NULL DEFAULT '{}',created_at BIGINT NOT NULL,UNIQUE(conversation_id,platform_message_id));
CREATE INDEX idx_pdd_chat_messages_inbox ON pdd_chat_messages(user_id,created_at DESC);
CREATE TABLE pdd_chat_captures (id TEXT PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),conversation_id TEXT NOT NULL REFERENCES pdd_chat_conversations(id),endpoint TEXT NOT NULL,response_json TEXT NOT NULL DEFAULT '{}',captured_at BIGINT NOT NULL);
CREATE INDEX idx_pdd_chat_captures_conversation ON pdd_chat_captures(conversation_id,captured_at DESC);
-- +goose Down
DROP TABLE IF EXISTS pdd_chat_captures;
DROP TABLE IF EXISTS pdd_chat_messages;
DROP TABLE IF EXISTS pdd_chat_conversations;
