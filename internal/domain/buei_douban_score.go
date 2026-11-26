package domain

import (
	"context"
	"time"
)

// buei:douban-score —— 豆瓣评分（后台辅助工具「豆瓣评分」）。

const (
	// BueiDoubanScoreMatched 表示匹配到豆瓣条目（评分可能为 0，即豆瓣暂无评分）。
	BueiDoubanScoreMatched = "matched"
	// BueiDoubanScoreNotFound 表示没有匹配的豆瓣条目。
	BueiDoubanScoreNotFound = "not_found"
	// BueiDoubanScoreError 表示查询失败（网络、风控、解析）。
	BueiDoubanScoreError = "error"
)

// BueiDoubanScore 是一条 STRM 索引项目的豆瓣评分。
// STRM 索引本身是可重建缓存（重建会 DELETE FROM items），因此评分独立存放在主库，
// 由 (strm_task_id, item_id) 关联回索引项目。
type BueiDoubanScore struct {
	StrmTaskID int64
	ItemID     string
	Title      string
	Year       *int
	MediaType  string

	// QueryTitle / QueryYear 是实际拿去查询的片名与年份（片名已清洗）。
	QueryTitle string
	QueryYear  *int

	DoubanID    string
	DoubanTitle string
	DoubanYear  *int
	Rating      *float64
	Votes       int
	// IsTV 由豆瓣接口的「剧集」标记 / episode 字段判定，用于二次校验媒体类型。
	IsTV bool

	Status  string
	Message string

	// CheckedAt 是最近一次向豆瓣查询的时间。
	CheckedAt time.Time
	UpdatedAt time.Time
}

// BueiDoubanScoreCounts 是按任务聚合的评分数量。
type BueiDoubanScoreCounts struct {
	Total  int // 已有评分记录的项目数（含未匹配）
	Scored int // 有豆瓣评分的项目数
}

// BueiDoubanScoreRepository 豆瓣评分仓储。
type BueiDoubanScoreRepository interface {
	ListByTask(ctx context.Context, strmTaskID int64) ([]*BueiDoubanScore, error)
	Upsert(ctx context.Context, scores []*BueiDoubanScore) error
	Counts(ctx context.Context) (map[int64]BueiDoubanScoreCounts, error)
	DeleteByTask(ctx context.Context, strmTaskID int64) error
}
