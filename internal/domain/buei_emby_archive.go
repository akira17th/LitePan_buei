package domain

import (
	"context"
	"time"
)

// Emby 自动归档（buei 定制）：为归档媒体库配置来源媒体库，按固定规则定时把项目移入该媒体库。

const (
	BueiArchiveTriggerAuto   = "auto"
	BueiArchiveTriggerManual = "manual"

	BueiArchiveRunRunning = "running"
	BueiArchiveRunSuccess = "success"
	BueiArchiveRunPartial = "partial"
	BueiArchiveRunFailed  = "failed"

	BueiArchiveRunItemArchived = "archived"
	BueiArchiveRunItemFailed   = "failed"
	BueiArchiveRunItemSkipped  = "skipped"
)

// BueiEmbyArchiveSourceLibrary 是自动归档配置里选中的来源 Emby 媒体库。
type BueiEmbyArchiveSourceLibrary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// BueiEmbyArchiveRule 是一个归档 Emby 媒体库的自动归档配置。
// 同一个来源媒体库只能出现在一个归档媒体库的配置中。
type BueiEmbyArchiveRule struct {
	ID                 int64
	ArchiveLibraryID   string
	ArchiveLibraryName string
	ConfigID           string
	SourceLibraries    []BueiEmbyArchiveSourceLibrary
	IntervalHours      int
	Enabled            bool
	LastRunAt          time.Time
	NextRunAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// BueiEmbyArchiveRunItem 是一次执行日志中的单个项目结果。
type BueiEmbyArchiveRunItem struct {
	Name          string `json:"name"`
	MediaType     string `json:"media_type,omitempty"`
	SourceLibrary string `json:"source_library,omitempty"`
	SourceTask    string `json:"source_task,omitempty"`
	TargetPath    string `json:"target_path,omitempty"`
	Result        string `json:"result"`
	Message       string `json:"message,omitempty"`
}

// BueiEmbyArchiveRun 是一次自动或手动归档的执行记录。
type BueiEmbyArchiveRun struct {
	ID                 int64
	ArchiveLibraryID   string
	ArchiveLibraryName string
	Trigger            string
	Status             string
	StartedAt          time.Time
	FinishedAt         time.Time
	Scanned            int
	Archived           int
	Skipped            int
	Failed             int
	Message            string
	Items              []BueiEmbyArchiveRunItem
}

// BueiEmbyArchiveRuleRepository 保存自动归档配置。
type BueiEmbyArchiveRuleRepository interface {
	List(ctx context.Context) ([]*BueiEmbyArchiveRule, error)
	Get(ctx context.Context, archiveLibraryID string) (*BueiEmbyArchiveRule, error)
	Save(ctx context.Context, rule *BueiEmbyArchiveRule) (*BueiEmbyArchiveRule, error)
	UpdateSchedule(ctx context.Context, archiveLibraryID string, lastRunAt, nextRunAt time.Time) error
}

// BueiEmbyArchiveRunRepository 保存自动归档执行日志。
type BueiEmbyArchiveRunRepository interface {
	Create(ctx context.Context, run *BueiEmbyArchiveRun) (*BueiEmbyArchiveRun, error)
	Finish(ctx context.Context, run *BueiEmbyArchiveRun) error
	ListByLibrary(ctx context.Context, archiveLibraryID string, limit int) ([]*BueiEmbyArchiveRun, error)
	// PruneByTrigger 只保留同一触发方式（手动/自动）的最新一条记录，keepID 为刚创建的那条。
	PruneByTrigger(ctx context.Context, archiveLibraryID, trigger string, keepID int64) error
	// FailInterrupted 把进程退出时仍为 running 的记录标记为失败，避免日志一直显示执行中。
	FailInterrupted(ctx context.Context, archiveLibraryID string) error
}
