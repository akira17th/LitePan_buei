package domain

import (
	"context"
	"time"
)

// QuarkIngestFavorite 是夸克入库保存的一条收藏：分享链接 + 「来自：分享」下的文件夹。
type QuarkIngestFavorite struct {
	ID             int64
	AccountID      int64
	Link           string
	FolderID       string
	ParentFolderID string
	FolderName     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// UpdateSchedule 是用户在收藏弹窗里设置的更新时间（仅用于展示更新节奏）。
	UpdateSchedule IngestUpdateSchedule
}

// QuarkIngestFavoriteRepository 负责夸克入库收藏的持久化。
type QuarkIngestFavoriteRepository interface {
	ListByAccount(ctx context.Context, accountID int64) ([]*QuarkIngestFavorite, error)
	Create(ctx context.Context, favorite *QuarkIngestFavorite) (*QuarkIngestFavorite, error)
	Update(ctx context.Context, favorite *QuarkIngestFavorite) (*QuarkIngestFavorite, error)
	Delete(ctx context.Context, id, accountID int64) error
	UpdateFolderName(ctx context.Context, id int64, name string) error
}
