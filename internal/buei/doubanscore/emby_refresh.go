package doubanscore

import (
	"context"
	"log/slog"

	"litepan/internal/domain"
)

// buei:emby-refresh —— 评分联动：查到豆瓣评分后触发对应 Emby 媒体库刷库。
//
// Emby/Jellyfin 只有在扫描媒体库目录时才会重新读 NFO；把评分写进 NFO 之后
// 触发一次 `POST /Items/{LibraryId}/Refresh`，评分才能尽快出现在 Emby 页面上。
// 每个任务在同一轮里最多刷一次（collectTaskRefresh 在任务收尾时登记）。

// EmbyRefresher 是刷 Emby 媒体库的最小接口（*embyproxy.Service.RefreshLibraryDirect）。
type EmbyRefresher interface {
	RefreshLibraryDirect(ctx context.Context, configID, libraryID string) error
}

// libraryRefresher 是每轮查询的刷库收集器：任务收尾登记，全部结束后统一刷一次。
type libraryRefresher struct {
	emby   EmbyRefresher
	log    *slog.Logger
	libs   domain.BueiDoubanTaskLibraryRepository
	byTask map[int64]domain.BueiDoubanTaskLibrary
	// pending 是本轮要刷的库（去重）。
	pending map[string]string
}

func newLibraryRefresher(emby EmbyRefresher, libs domain.BueiDoubanTaskLibraryRepository, log *slog.Logger) *libraryRefresher {
	return &libraryRefresher{
		emby:    emby,
		log:     log,
		libs:    libs,
		pending: map[string]string{},
	}
}

// load 预读任务 → 媒体库映射（整轮一次）。
func (r *libraryRefresher) load(ctx context.Context) {
	if r == nil || r.libs == nil {
		return
	}
	m, err := r.libs.Map(ctx)
	if err != nil {
		r.log.Warn("读取任务媒体库关联失败，本轮不再刷库", "err", err)
		m = nil
	}
	r.byTask = m
}

// collectTask 在任务查询收尾时登记：本轮给该任务写过评分 NFO 才刷它的媒体库。
func (r *libraryRefresher) collectTask(taskID int64) {
	if r == nil || r.byTask == nil {
		return
	}
	lib, ok := r.byTask[taskID]
	if !ok || lib.LibraryID == "" {
		return
	}
	if r.pending == nil {
		r.pending = map[string]string{}
	}
	r.pending[lib.LibraryID] = lib.LibraryName
}

// flush 整轮结束后统一刷库：单个库失败只记警告，不影响其余库。
func (r *libraryRefresher) flush(ctx context.Context) {
	if r == nil || r.emby == nil || len(r.pending) == 0 {
		return
	}
	for libID, libName := range r.pending {
		if ctx.Err() != nil {
			return
		}
		if err := r.emby.RefreshLibraryDirect(ctx, "", libID); err != nil {
			r.log.Warn("触发 Emby 媒体库刷新失败", "library_id", libID, "library_name", libName, "err", err)
			continue
		}
		r.log.Info("已触发 Emby 媒体库刷新（豆瓣评分已写入）", "library_id", libID, "library_name", libName)
	}
}
