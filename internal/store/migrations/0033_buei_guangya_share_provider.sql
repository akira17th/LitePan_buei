ALTER TABLE buei_guangya_share_links ADD COLUMN provider TEXT NOT NULL DEFAULT 'guangya';

CREATE INDEX IF NOT EXISTS idx_buei_guangya_share_links_provider
  ON buei_guangya_share_links(provider, last_used_at DESC);
