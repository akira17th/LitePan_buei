ALTER TABLE buei_guangya_share_links ADD COLUMN last_account_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE buei_guangya_share_links ADD COLUMN last_parent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE buei_guangya_share_links ADD COLUMN last_base_path TEXT NOT NULL DEFAULT '';
ALTER TABLE buei_guangya_share_links ADD COLUMN last_title TEXT NOT NULL DEFAULT '';
ALTER TABLE buei_guangya_share_links ADD COLUMN last_season INTEGER NOT NULL DEFAULT 1;
ALTER TABLE buei_guangya_share_links ADD COLUMN last_is_series INTEGER NOT NULL DEFAULT 1;
