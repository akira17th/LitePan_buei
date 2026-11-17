-- buei: 光鸭分享转存后自动刮削（重建刮削索引 + 开始刮削）及其 STRM 任务
ALTER TABLE buei_guangya_share_links ADD COLUMN auto_scrape INTEGER NOT NULL DEFAULT 1;
ALTER TABLE buei_guangya_share_links ADD COLUMN auto_scrape_task_id INTEGER NOT NULL DEFAULT 0;
