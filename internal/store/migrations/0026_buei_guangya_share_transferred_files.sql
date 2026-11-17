CREATE TABLE IF NOT EXISTS buei_guangya_share_transferred_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  share_id TEXT NOT NULL,
  file_id TEXT NOT NULL,
  account_id INTEGER NOT NULL DEFAULT 0,
  original_name TEXT NOT NULL DEFAULT '',
  new_name TEXT NOT NULL DEFAULT '',
  target_parent_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(share_id, file_id),
  FOREIGN KEY (share_id) REFERENCES buei_guangya_share_links(share_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_buei_guangya_share_transferred_share
  ON buei_guangya_share_transferred_files(share_id, updated_at DESC);
