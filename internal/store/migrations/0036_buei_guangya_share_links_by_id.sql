-- buei: 光鸭收藏链接允许重复，转存历史改按收藏 ID 关联
CREATE TABLE buei_guangya_share_links_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  share_id TEXT NOT NULL,
  url TEXT NOT NULL DEFAULT '',
  password TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  owner TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_used_at TEXT NOT NULL,
  last_account_id INTEGER NOT NULL DEFAULT 0,
  last_parent_id TEXT NOT NULL DEFAULT '',
  last_base_path TEXT NOT NULL DEFAULT '',
  last_base_dirs TEXT NOT NULL DEFAULT '[]',
  last_title TEXT NOT NULL DEFAULT '',
  last_season INTEGER NOT NULL DEFAULT 1,
  last_is_series INTEGER NOT NULL DEFAULT 1,
  last_browse_path TEXT NOT NULL DEFAULT '',
  rename_replace_enabled INTEGER NOT NULL DEFAULT 0,
  rename_replace_find TEXT NOT NULL DEFAULT '',
  rename_replace_with TEXT NOT NULL DEFAULT '',
  auto_strm INTEGER NOT NULL DEFAULT 1,
  auto_scrape INTEGER NOT NULL DEFAULT 1,
  auto_scrape_task_id INTEGER NOT NULL DEFAULT 0,
  auto_emby INTEGER NOT NULL DEFAULT 1,
  emby_config_id TEXT NOT NULL DEFAULT '',
  emby_mode TEXT NOT NULL DEFAULT 'global',
  emby_library_id TEXT NOT NULL DEFAULT '',
  rename_replace_rules TEXT NOT NULL DEFAULT '[]'
);

INSERT INTO buei_guangya_share_links_new (
  id, share_id, url, password, title, owner, created_at, updated_at, last_used_at,
  last_account_id, last_parent_id, last_base_path, last_base_dirs, last_title,
  last_season, last_is_series, last_browse_path, rename_replace_enabled,
  rename_replace_find, rename_replace_with, auto_strm, auto_scrape,
  auto_scrape_task_id, auto_emby, emby_config_id, emby_mode, emby_library_id,
  rename_replace_rules
)
SELECT
  id, share_id, url, password, title, owner, created_at, updated_at, last_used_at,
  last_account_id, last_parent_id, last_base_path, last_base_dirs, last_title,
  last_season, last_is_series, last_browse_path, rename_replace_enabled,
  rename_replace_find, rename_replace_with, auto_strm, auto_scrape,
  auto_scrape_task_id, auto_emby, emby_config_id, emby_mode, emby_library_id,
  rename_replace_rules
FROM buei_guangya_share_links;

CREATE TABLE buei_guangya_share_transferred_files_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  link_id INTEGER NOT NULL,
  file_id TEXT NOT NULL,
  account_id INTEGER NOT NULL DEFAULT 0,
  original_name TEXT NOT NULL DEFAULT '',
  new_name TEXT NOT NULL DEFAULT '',
  target_parent_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'transferred',
  UNIQUE(link_id, file_id),
  FOREIGN KEY (link_id) REFERENCES buei_guangya_share_links_new(id) ON DELETE CASCADE
);

INSERT INTO buei_guangya_share_transferred_files_new (
  id, link_id, file_id, account_id, original_name, new_name, target_parent_id,
  created_at, updated_at, status
)
SELECT
  t.id, l.id, t.file_id, t.account_id, t.original_name, t.new_name,
  t.target_parent_id, t.created_at, t.updated_at, t.status
FROM buei_guangya_share_transferred_files t
JOIN buei_guangya_share_links_new l ON l.share_id = t.share_id;

DROP TABLE buei_guangya_share_transferred_files;

DROP TABLE buei_guangya_share_links;

ALTER TABLE buei_guangya_share_links_new RENAME TO buei_guangya_share_links;

ALTER TABLE buei_guangya_share_transferred_files_new RENAME TO buei_guangya_share_transferred_files;

CREATE INDEX idx_buei_guangya_share_links_last_used
  ON buei_guangya_share_links(last_used_at DESC);

CREATE INDEX idx_buei_guangya_share_links_share
  ON buei_guangya_share_links(share_id);

CREATE INDEX idx_buei_guangya_share_transferred_link
  ON buei_guangya_share_transferred_files(link_id, updated_at DESC);
