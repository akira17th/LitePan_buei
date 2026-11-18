package domain

import (
	"context"
	"time"
)

// GuangyaShareDestinationBase 是最近一次成功转存使用的基础目录。
type GuangyaShareDestinationBase struct {
	AccountID int64    `json:"account_id"`
	ParentID  string   `json:"parent_id"`
	Path      string   `json:"path"`
	Dirs      []string `json:"dirs,omitempty"`
}

// GuangyaShareDestination 记录某个分享上次转存时使用的完整目标预设。
type GuangyaShareDestination struct {
	AccountID int64    `json:"account_id"`
	ParentID  string   `json:"parent_id"`
	BasePath  string   `json:"base_path"`
	Title     string   `json:"title"`
	Season    int      `json:"season"`
	IsSeries  bool     `json:"is_series"`
	BaseDirs  []string `json:"base_dirs,omitempty"`
	// AutoStrm 表示转存成功后自动在目标文件夹生成 STRM。
	AutoStrm bool `json:"auto_strm"`
	// AutoScrape 表示转存成功后自动重建刮削索引并开始刮削（依赖 AutoStrm）。
	AutoScrape bool `json:"auto_scrape"`
	// ScrapeTaskID 是自动刮削使用的 STRM 任务（海报墙下拉选择）。
	ScrapeTaskID int64 `json:"auto_scrape_task_id"`
	// RefreshEmby 表示转存成功后触发 Emby/Jellyfin 刷库。
	RefreshEmby bool `json:"refresh_emby"`
	// EmbyConfigID / EmbyMode / EmbyLibraryID 是刷库使用的配置、范围与媒体库。
	EmbyConfigID  string `json:"emby_config_id"`
	EmbyMode      string `json:"emby_mode"`
	EmbyLibraryID string `json:"emby_library_id"`
}

// GuangyaShareRenameReplace 是保存的“替换文本”重命名规则。
type GuangyaShareRenameReplace struct {
	Enabled     bool   `json:"enabled"`
	Find        string `json:"find"`
	ReplaceWith string `json:"replace_with"`
}

// GuangyaShareBrowseCrumb 是分享内浏览路径的一级目录。
type GuangyaShareBrowseCrumb struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

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
	// LastDestination 是该分享上次成功转存的位置；没有转存过时为 nil。
	LastDestination *GuangyaShareDestination
	// LastBrowsePath 是上次转存时浏览到的分享目录（含根节点）。
	LastBrowsePath []GuangyaShareBrowseCrumb
	// RenameReplace 是上次使用的替换文本重命名规则。
	RenameReplace GuangyaShareRenameReplace
}

const (
	// GuangyaShareFileTransferred 表示文件已经转存。
	GuangyaShareFileTransferred = "transferred"
	// GuangyaShareFileDiscarded 表示用户主动丢弃了该文件。
	GuangyaShareFileDiscarded = "discarded"
)

// GuangyaShareTransferredFile 记录某个分享里已经处理过（转存或丢弃）的文件及其新名称。
type GuangyaShareTransferredFile struct {
	FileID         string
	OriginalName   string
	NewName        string
	AccountID      int64
	TargetParentID string
	Status         string
	UpdatedAt      time.Time
}

// GuangyaShareTransferredRepository 负责分享转存历史的持久化。
type GuangyaShareTransferredRepository interface {
	ListByShare(ctx context.Context, shareID string) ([]*GuangyaShareTransferredFile, error)
	UpsertMany(ctx context.Context, shareID string, files []*GuangyaShareTransferredFile) error
	DeleteByShare(ctx context.Context, shareID string) error
}

// GuangyaShareLinkRepository 负责收藏链接的持久化。
type GuangyaShareLinkRepository interface {
	List(ctx context.Context) ([]*GuangyaShareLink, error)
	// Upsert 按 share_id 去重；重复解析同一分享时更新标题、链接与最近使用时间。
	Upsert(ctx context.Context, link *GuangyaShareLink) (*GuangyaShareLink, error)
	// UpdateDestination 更新分享上次成功转存的目标预设，不影响收藏链接本身。
	UpdateDestination(ctx context.Context, shareID string, dest *GuangyaShareDestination) error
	// UpdateBrowsePath 更新上次转存时浏览的分享目录。
	UpdateBrowsePath(ctx context.Context, shareID string, crumbs []GuangyaShareBrowseCrumb) error
	// UpdateRenameReplace 保存“替换文本”重命名规则。
	UpdateRenameReplace(ctx context.Context, shareID string, rule GuangyaShareRenameReplace) error
	// Delete 删除收藏并返回 share_id，供调用方清理关联数据。
	Delete(ctx context.Context, id int64) (string, error)
}
