CREATE TABLE IF NOT EXISTS buei_emby_archive_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  archive_library_id TEXT NOT NULL,
  archive_library_name TEXT NOT NULL DEFAULT '',
  config_id TEXT NOT NULL DEFAULT '',
  source_libraries TEXT NOT NULL DEFAULT '[]',
  interval_hours INTEGER NOT NULL DEFAULT 1,
  last_run_at TEXT,
  next_run_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_buei_emby_archive_rules_library
  ON buei_emby_archive_rules(archive_library_id);

CREATE TABLE IF NOT EXISTS buei_emby_archive_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  archive_library_id TEXT NOT NULL,
  archive_library_name TEXT NOT NULL DEFAULT '',
  trigger TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  finished_at TEXT,
  scanned INTEGER NOT NULL DEFAULT 0,
  archived INTEGER NOT NULL DEFAULT 0,
  skipped INTEGER NOT NULL DEFAULT 0,
  failed INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  items TEXT NOT NULL DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS idx_buei_emby_archive_runs_library
  ON buei_emby_archive_runs(archive_library_id, id DESC);
