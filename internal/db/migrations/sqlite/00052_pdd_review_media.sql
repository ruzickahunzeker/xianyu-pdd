-- +goose Up
CREATE TABLE pdd_review_media (id INTEGER PRIMARY KEY AUTOINCREMENT,goods_id TEXT NOT NULL,review_id TEXT NOT NULL,sku_id TEXT NOT NULL DEFAULT '',media_key TEXT NOT NULL,media_type TEXT NOT NULL,source_type TEXT NOT NULL DEFAULT 'initial',remote_url TEXT NOT NULL,cover_url TEXT NOT NULL DEFAULT '',media_md5 TEXT NOT NULL DEFAULT '',width INTEGER NOT NULL DEFAULT 0,height INTEGER NOT NULL DEFAULT 0,duration_ms INTEGER NOT NULL DEFAULT 0,is_live_photo_image INTEGER NOT NULL DEFAULT 0,collected_at INTEGER NOT NULL,last_seen_at INTEGER NOT NULL,UNIQUE(goods_id,media_key));
CREATE INDEX idx_pdd_review_media_goods_type ON pdd_review_media(goods_id,media_type,last_seen_at);
ALTER TABLE product_materials ADD COLUMN video_enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE product_materials ADD COLUMN videos_json TEXT NOT NULL DEFAULT '[]';
-- +goose Down
ALTER TABLE product_materials DROP COLUMN videos_json;
ALTER TABLE product_materials DROP COLUMN video_enabled;
DROP TABLE IF EXISTS pdd_review_media;
