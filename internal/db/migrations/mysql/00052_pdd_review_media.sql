-- +goose Up
CREATE TABLE pdd_review_media (id BIGINT AUTO_INCREMENT PRIMARY KEY,goods_id VARCHAR(191) NOT NULL,review_id VARCHAR(191) NOT NULL,sku_id VARCHAR(191) NOT NULL DEFAULT '',media_key VARCHAR(255) NOT NULL,media_type VARCHAR(16) NOT NULL,source_type VARCHAR(32) NOT NULL DEFAULT 'initial',remote_url TEXT NOT NULL,cover_url TEXT NOT NULL,media_md5 VARCHAR(191) NOT NULL DEFAULT '',width INTEGER NOT NULL DEFAULT 0,height INTEGER NOT NULL DEFAULT 0,duration_ms BIGINT NOT NULL DEFAULT 0,is_live_photo_image TINYINT NOT NULL DEFAULT 0,collected_at BIGINT NOT NULL,last_seen_at BIGINT NOT NULL,UNIQUE KEY uq_pdd_review_media(goods_id,media_key),KEY idx_pdd_review_media_goods_type(goods_id,media_type,last_seen_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
ALTER TABLE product_materials ADD COLUMN video_enabled TINYINT NOT NULL DEFAULT 1;
ALTER TABLE product_materials ADD COLUMN videos_json LONGTEXT NOT NULL DEFAULT ('[]');
-- +goose Down
ALTER TABLE product_materials DROP COLUMN videos_json;
ALTER TABLE product_materials DROP COLUMN video_enabled;
DROP TABLE pdd_review_media;
