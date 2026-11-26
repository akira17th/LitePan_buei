CREATE TABLE IF NOT EXISTS buei_douban_scores (
    strm_task_id INTEGER NOT NULL,
    item_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    year INTEGER,
    media_type TEXT NOT NULL DEFAULT '',
    query_title TEXT NOT NULL DEFAULT '',
    query_year INTEGER,
    douban_id TEXT NOT NULL DEFAULT '',
    douban_title TEXT NOT NULL DEFAULT '',
    douban_year INTEGER,
    rating REAL,
    votes INTEGER NOT NULL DEFAULT 0,
    is_tv INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    ended_at TEXT,
    checked_at TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (strm_task_id, item_id)
);

CREATE INDEX IF NOT EXISTS idx_buei_douban_scores_task ON buei_douban_scores(strm_task_id);
