package domain

import (
	"context"
	"time"
)

// BueiDoubanTaskLibrary 是 STRM 任务与 Emby 媒体库的关联：
// 豆瓣评分查询到新评分后，触发对应媒体库刷库，让 Emby 尽快读到 NFO 里的新评分。
type BueiDoubanTaskLibrary struct {
	LibraryID   string
	LibraryName string
	UpdatedAt   time.Time
}

// BueiDoubanTaskLibraryRepository 任务 ↔ 媒体库关联仓储。
type BueiDoubanTaskLibraryRepository interface {
	// Map 返回 strm_task_id → 关联媒体库（只有关联过的任务才会出现）。
	Map(ctx context.Context) (map[int64]BueiDoubanTaskLibrary, error)
	// Set 保存/更新任务的关联媒体库。
	Set(ctx context.Context, strmTaskID int64, libraryID, libraryName string) error
	// Delete 删除任务的关联（媒体库不再刷库）。
	Delete(ctx context.Context, strmTaskID int64) error
}
