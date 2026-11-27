-- buei:douban-score —— STRM 任务与 Emby 媒体库的关联：
-- 查询到豆瓣评分后触发对应媒体库刷库（POST /Items/{LibraryId}/Refresh）。
CREATE TABLE IF NOT EXISTS buei_douban_task_libraries (
    strm_task_id INTEGER PRIMARY KEY,
    library_id   TEXT NOT NULL,
    library_name TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL
);
