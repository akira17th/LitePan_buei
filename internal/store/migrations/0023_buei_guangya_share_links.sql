CREATE TABLE IF NOT EXISTS buei_guangya_share_links (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  share_id TEXT NOT NULL UNIQUE,
  url TEXT NOT NULL DEFAULT '',
  password TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  owner TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_used_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_buei_guangya_share_links_last_used
  ON buei_guangya_share_links(last_used_at DESC);
