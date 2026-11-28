ALTER TABLE buei_guangya_share_links ADD COLUMN update_weekday INTEGER NOT NULL DEFAULT -1;
ALTER TABLE buei_guangya_share_links ADD COLUMN update_hour INTEGER NOT NULL DEFAULT 0;
ALTER TABLE buei_guangya_share_links ADD COLUMN update_minute INTEGER NOT NULL DEFAULT 0;
ALTER TABLE buei_quark_ingest_favorites ADD COLUMN update_weekday INTEGER NOT NULL DEFAULT -1;
ALTER TABLE buei_quark_ingest_favorites ADD COLUMN update_hour INTEGER NOT NULL DEFAULT 0;
ALTER TABLE buei_quark_ingest_favorites ADD COLUMN update_minute INTEGER NOT NULL DEFAULT 0;
