package domain

import (
	"context"
	"time"
)

// Emby 媒体库最近同步时间（buei 定制）：用于“超过 12 小时自动同步”和按钮悬停提示。

// BueiEmbyLibrarySync 记录某个 Emby 媒体库最近一次「Emby 同步」成功的时间。
type BueiEmbyLibrarySync struct {
	LibraryID    string
	LibraryName  string
	LastSyncedAt time.Time
}

// BueiEmbyLibrarySyncRepository 是最近同步时间的仓储。
type BueiEmbyLibrarySyncRepository interface {
	List(ctx context.Context) ([]*BueiEmbyLibrarySync, error)
	Get(ctx context.Context, libraryID string) (*BueiEmbyLibrarySync, error)
	Save(ctx context.Context, item *BueiEmbyLibrarySync) error
}
