package domain

import (
	"context"
	"time"
)

// GuangyaShareLink 是 buei 定制「光鸭分享收藏」保存的一条分享链接。
type GuangyaShareLink struct {
	ID         int64
	ShareID    string
	URL        string
	Password   string
	Title      string
	Owner      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	LastUsedAt time.Time
}

// GuangyaShareLinkRepository 负责收藏链接的持久化。
type GuangyaShareLinkRepository interface {
	List(ctx context.Context) ([]*GuangyaShareLink, error)
	// Upsert 按 share_id 去重；重复解析同一分享时更新标题、链接与最近使用时间。
	Upsert(ctx context.Context, link *GuangyaShareLink) (*GuangyaShareLink, error)
	Delete(ctx context.Context, id int64) error
}
