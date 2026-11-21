CREATE TABLE IF NOT EXISTS buei_emby_library_syncs (
    library_id TEXT PRIMARY KEY,
    library_name TEXT NOT NULL DEFAULT '',
    last_synced_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
