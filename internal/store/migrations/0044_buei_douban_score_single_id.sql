UPDATE buei_douban_scores SET douban_id = manual_douban_id WHERE manual_douban_id <> '';
ALTER TABLE buei_douban_scores DROP COLUMN manual_douban_id;
