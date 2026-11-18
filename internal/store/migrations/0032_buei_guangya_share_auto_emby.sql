-- buei: 光鸭分享转存后 Emby 刷库（开关 / 配置 / 范围 / 媒体库）跟随目标预设保存
ALTER TABLE buei_guangya_share_links ADD COLUMN auto_emby INTEGER NOT NULL DEFAULT 1;
ALTER TABLE buei_guangya_share_links ADD COLUMN emby_config_id TEXT NOT NULL DEFAULT '';
ALTER TABLE buei_guangya_share_links ADD COLUMN emby_mode TEXT NOT NULL DEFAULT 'global';
ALTER TABLE buei_guangya_share_links ADD COLUMN emby_library_id TEXT NOT NULL DEFAULT '';
