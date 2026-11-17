ALTER TABLE buei_guangya_share_links ADD COLUMN rename_replace_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE buei_guangya_share_links ADD COLUMN rename_replace_find TEXT NOT NULL DEFAULT '';
ALTER TABLE buei_guangya_share_links ADD COLUMN rename_replace_with TEXT NOT NULL DEFAULT '';
