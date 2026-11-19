CREATE TABLE IF NOT EXISTS buei_quark_ingest_favorites (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  link TEXT NOT NULL DEFAULT '',
  folder_id TEXT NOT NULL,
  parent_folder_id TEXT NOT NULL DEFAULT '',
  folder_name TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_buei_quark_ingest_favorites_account
  ON buei_quark_ingest_favorites(account_id, id ASC);
