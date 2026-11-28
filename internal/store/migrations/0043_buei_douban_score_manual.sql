ALTER TABLE buei_douban_scores ADD COLUMN manual_douban_id TEXT NOT NULL DEFAULT '';
ALTER TABLE buei_douban_scores ADD COLUMN manual_rating REAL;
ALTER TABLE buei_douban_scores ADD COLUMN no_auto INTEGER NOT NULL DEFAULT 0;
