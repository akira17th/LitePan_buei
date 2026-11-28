package domain

import (
	"context"
	"strings"
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

// GuangyaShareRenameRule 是一条“替换文本”规则。
type GuangyaShareRenameRule struct {
	Find        string `json:"find"`
	ReplaceWith string `json:"replace_with"`
}

// GuangyaShareRenameReplace 是保存的“替换文本”重命名规则。
// Rules 支持按顺序执行多条替换；Find/ReplaceWith 保留为第一条规则的镜像，
// 兼容早期只有一个输入框的版本。
type GuangyaShareRenameReplace struct {
	Enabled     bool                     `json:"enabled"`
	Find        string                   `json:"find"`
	ReplaceWith string                   `json:"replace_with"`
	Rules       []GuangyaShareRenameRule `json:"rules,omitempty"`
}

// NormalizeRules 返回有效的规则列表：优先 Rules，否则回退到单条 Find/ReplaceWith。
func (r GuangyaShareRenameReplace) NormalizeRules() []GuangyaShareRenameRule {
	out := make([]GuangyaShareRenameRule, 0, len(r.Rules)+1)
	for _, rule := range r.Rules {
		if strings.TrimSpace(rule.Find) == "" {
			continue
		}
		out = append(out, rule)
	}
	if len(out) == 0 && strings.TrimSpace(r.Find) != "" {
		out = append(out, GuangyaShareRenameRule{Find: r.Find, ReplaceWith: r.ReplaceWith})
	}
	return out
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
	// UpdateSchedule 是用户在收藏弹窗里设置的更新时间（仅用于展示更新节奏）。
	UpdateSchedule IngestUpdateSchedule
	// buei: MagnetAccountID 是磁力收藏离线使用的光鸭账号 ID；仅列表返回时在内存中填充，不落库。
	MagnetAccountID int64
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

// GuangyaShareTransferredRepository 按收藏 ID 持久化分享转存历史。
// 链接可以重复收藏，因此不能再用 share_id 区分不同收藏的历史。
type GuangyaShareTransferredRepository interface {
	ListByLink(ctx context.Context, linkID int64) ([]*GuangyaShareTransferredFile, error)
	UpsertMany(ctx context.Context, linkID int64, files []*GuangyaShareTransferredFile) error
	DeleteByLink(ctx context.Context, linkID int64) error
}

// GuangyaShareLinkRepository 负责收藏链接的持久化。
type GuangyaShareLinkRepository interface {
	List(ctx context.Context) ([]*GuangyaShareLink, error)
	Get(ctx context.Context, id int64) (*GuangyaShareLink, error)
	// Create 新建一条收藏；相同 share_id 可以重复收藏。
	Create(ctx context.Context, link *GuangyaShareLink) (*GuangyaShareLink, error)
	// Touch 只更新最近使用时间，按收藏 ID 定位，不修改链接内容。
	Touch(ctx context.Context, id int64) error
	// UpdateDestination 更新该收藏上次成功转存的目标预设。
	UpdateDestination(ctx context.Context, id int64, dest *GuangyaShareDestination) error
	// UpdateBrowsePath 更新该收藏上次转存时浏览的分享目录。
	UpdateBrowsePath(ctx context.Context, id int64, crumbs []GuangyaShareBrowseCrumb) error
	// UpdateRenameReplace 保存该收藏的“替换文本”重命名规则。
	UpdateRenameReplace(ctx context.Context, id int64, rule GuangyaShareRenameReplace) error
	// UpdateSchedule 保存该收藏的更新时间设置（未设置时清空）。
	UpdateSchedule(ctx context.Context, id int64, schedule IngestUpdateSchedule) error
	// UpdateIdentity 按收藏 ID 更新分享身份（链接、分享 ID、标题、提取码），保留目标预设与重命名规则。
	// clearBrowsePath 用于分享 ID 变化时清理已失效的浏览路径和转存历史。
	UpdateIdentity(ctx context.Context, id int64, shareID, url, password, title, owner string, clearBrowsePath bool) (*GuangyaShareLink, error)
	// Delete 按收藏 ID 删除收藏，关联转存历史由外键级联清理。
	Delete(ctx context.Context, id int64) error
}
