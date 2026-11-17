-- buei: 光鸭分享转存后自动生成 STRM 开关（跟随分享的目标预设保存）
ALTER TABLE buei_guangya_share_links ADD COLUMN auto_strm INTEGER NOT NULL DEFAULT 1;
