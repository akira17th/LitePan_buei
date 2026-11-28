package doubanscore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
	"litepan/pkg/safego"
)

const (
	// ScheduleHour 是每天自动查询的小时（服务器本地时区）。
	ScheduleHour = 8

	scheduleTick     = time.Minute
	manualRunTimeout = 6 * time.Hour
	stopWaitBudget   = 10 * time.Second

	itemsPageSize = 200 // strmscrape 单页上限
	maxTaskItems  = 20000
	upsertBatch   = 20

	stateConfigKey = "buei_douban_score_state"
)

// TaskSource 提供 STRM 任务列表（*strm.Service）。
type TaskSource interface {
	ListTasks(ctx context.Context) ([]*domain.StrmTask, error)
}

// ItemSource 提供某个 STRM 索引里的作品列表（*strmscrape.Service）。
type ItemSource interface {
	ListItems(ctx context.Context, strmTaskID int64, query strmscrape.ItemListQuery) (strmscrape.ItemListResult, error)
}

// RatingWriter 把评分写进作品 NFO 的社区评分字段（*strmscrape.Service）。
type RatingWriter interface {
	UpdateItemRatings(ctx context.Context, strmTaskID int64, ratings []strmscrape.ItemRating) error
}

// Options 构造豆瓣评分服务。
type Options struct {
	Tasks   TaskSource
	Items   ItemSource
	Scores  domain.BueiDoubanScoreRepository
	Configs domain.ConfigRepository
	Log     *slog.Logger
	Now     func() time.Time
	// Ratings 可选：非空时把查到的评分一并写进作品 NFO。
	Ratings RatingWriter
	// Libraries 可选：任务 ↔ Emby 媒体库关联仓储（查到评分后刷对应媒体库）。
	Libraries domain.BueiDoubanTaskLibraryRepository
	// Emby 可选：Emby 刷库接口（RefreshLibraryDirect）；与 Libraries 同时配置才启用联动。
	Emby EmbyRefresher

	// Interval 是两次豆瓣请求之间的最小间隔，留空用默认 900ms。
	Interval time.Duration
	// Lookup 可替换查询实现（测试用），留空走真实豆瓣接口。
	Lookup func(ctx context.Context, title string, year *int, mediaType string) (*LookupResult, error)
	// LookupByID 可替换按豆瓣 ID 取条目的实现（测试用），留空走真实豆瓣接口。
	LookupByID func(ctx context.Context, doubanID string) (*LookupResult, error)

	// SchedulerDisabled 为 true 时不启动每日自动查询（本地开发用），手动查询仍然可用。
	SchedulerDisabled bool
	// DevVerbose 为 true 时把豆瓣查询过程（入参 / 候选打分 / 命中 / 失败原因）打到控制台，
	// 由 LITEPAN_DEV=1 打开；生产环境保持 false，不产生额外日志。
	DevVerbose bool
}

// Service 负责豆瓣评分的查询、保存与每日自动刷新。
type Service struct {
	tasks    TaskSource
	items    ItemSource
	scores   domain.BueiDoubanScoreRepository
	ratings  RatingWriter
	libs     domain.BueiDoubanTaskLibraryRepository
	emby     EmbyRefresher
	configs  domain.ConfigRepository
	log      *slog.Logger
	now      func() time.Time
	douban   *doubanClient
	lookup   func(ctx context.Context, title string, year *int, mediaType string) (*LookupResult, error)
	lookupID func(ctx context.Context, doubanID string) (*LookupResult, error)
	noSched  bool
	// devVerbose 打开开发环境的查询日志（见 devlog.go）。
	devVerbose bool

	mu        sync.Mutex
	running   bool
	progress  Progress
	runCancel context.CancelFunc
	appCtx    context.Context
	loopStop  context.CancelFunc
	wg        sync.WaitGroup
}

// New 构造豆瓣评分服务。
func New(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	s := &Service{
		tasks:      opts.Tasks,
		items:      opts.Items,
		scores:     opts.Scores,
		ratings:    opts.Ratings,
		libs:       opts.Libraries,
		emby:       opts.Emby,
		configs:    opts.Configs,
		log:        log,
		now:        now,
		douban:     newDoubanClient(opts.Interval),
		lookup:     opts.Lookup,
		lookupID:   opts.LookupByID,
		noSched:    opts.SchedulerDisabled,
		devVerbose: opts.DevVerbose,
	}
	if s.lookup == nil {
		s.lookup = s.lookupDouban
	}
	if s.lookupID == nil {
		s.lookupID = s.douban.subjectDetail
	}
	if s.devVerbose {
		// 开发环境：连底层 HTTP 请求（URL / 状态码 / 耗时）一起打到控制台。
		s.douban.debugf = s.devf
	}
	return s
}

// ItemRunStatus 是单个作品在一次查询中的状态与结果说明。
type ItemRunStatus struct {
	Title  string `json:"title"`
	State  string `json:"state"` // queried | skipped | failed
	Detail string `json:"detail,omitempty"`
}

// Progress 是一次查询的执行进度。
type Progress struct {
	Running      bool   `json:"running"`
	Scope        string `json:"scope"` // task | items | all
	StrmTaskID   int64  `json:"strm_task_id,omitempty"`
	Total        int    `json:"total"`
	Done         int    `json:"done"`
	Queried      int    `json:"queried"`
	Skipped      int    `json:"skipped"`
	Failed       int    `json:"failed"`
	CurrentTitle string `json:"current_title,omitempty"`
	// CurrentItemID 是正在查询的项目 ID（弹窗逐项状态用）。
	CurrentItemID string `json:"current_item_id,omitempty"`
	Message       string `json:"message,omitempty"`
	Error         string `json:"error,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
	// Items 按项目记录本轮状态与真实原因（跳过原因 / 失败原因 / 匹配结果）。
	Items map[string]*ItemRunStatus `json:"items,omitempty"`
}

// Summary 是最近一次自动/手动查询的统计。
type Summary struct {
	Total      int    `json:"total"`
	Queried    int    `json:"queried"`
	Skipped    int    `json:"skipped"`
	Failed     int    `json:"failed"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// StateView 是面板顶部的调度状态。
type StateView struct {
	Progress    Progress `json:"progress"`
	RunAt       string   `json:"run_at"`
	LastRunAt   string   `json:"last_run_at,omitempty"`
	NextRunAt   string   `json:"next_run_at,omitempty"`
	LastSummary *Summary `json:"last_summary,omitempty"`
	// RunLog 是最近一次每日自动查询的执行日志（各任务待查/成功/失败统计）。
	RunLog *RunLog `json:"run_log,omitempty"`
}

type persistedState struct {
	LastRunAt   string   `json:"last_run_at,omitempty"`
	LastSummary *Summary `json:"last_summary,omitempty"`
}

// TaskWall 是左侧任务索引卡片。
type TaskWall struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Recorded int    `json:"recorded"`
	Scored   int    `json:"scored"`
}

// ScoreView 是单个作品的豆瓣评分视图。
type ScoreView struct {
	Rating      *float64 `json:"rating,omitempty"`
	Votes       int      `json:"votes"`
	DoubanID    string   `json:"douban_id,omitempty"`
	DoubanTitle string   `json:"douban_title,omitempty"`
	DoubanYear  *int     `json:"douban_year,omitempty"`
	IsTV        bool     `json:"is_tv"`
	Status      string   `json:"status"`
	Message     string   `json:"message,omitempty"`
	QueryTitle  string   `json:"query_title,omitempty"`
	CheckedAt   string   `json:"checked_at,omitempty"`
	NeedsQuery  bool     `json:"needs_query"`
	// 手动编辑项：编辑弹窗据此回填（豆瓣 ID 只有 DoubanID 一个字段，不另存）。
	ManualRating *float64 `json:"manual_rating,omitempty"`
	NoAutoQuery  bool     `json:"no_auto_query"`
}

// WallItem 是海报墙上的一部作品（STRM 索引项 + 豆瓣评分）。
type WallItem struct {
	ID         string `json:"id"`
	StrmTaskID int64  `json:"strm_task_id"`
	TaskName   string `json:"task_name"`
	Title      string `json:"title"`
	Year       *int   `json:"year,omitempty"`
	MediaType  string `json:"media_type"`
	TVState    string `json:"tv_state,omitempty"`
	PosterURL  string `json:"poster_url,omitempty"`
	Status     string `json:"status"`
	AddedAt    string `json:"added_at,omitempty"`
	// ReleaseDate / EndDate 来自 STRM 索引（刮削写入 NFO 的发行日期与剧集完结日期）。
	ReleaseDate string `json:"release_date,omitempty"`
	EndDate     string `json:"end_date,omitempty"`
	// TMDBID 来自 STRM 索引（NFO / 目录名），海报墙据此提供 TMDB 跳转。
	TMDBID string     `json:"tmdb_id,omitempty"`
	Douban *ScoreView `json:"douban,omitempty"`
}

// ItemStats 是当前任务海报墙的统计。
type ItemStats struct {
	Total    int `json:"total"`
	Scored   int `json:"scored"`
	Unscored int `json:"unscored"`
	Pending  int `json:"pending"`
	TV       int `json:"tv"`
	Movie    int `json:"movie"`
}

// ItemPage 是海报墙的一页数据。
type ItemPage struct {
	Items   []WallItem `json:"items"`
	Total   int        `json:"total"`
	Offset  int        `json:"offset"`
	Limit   int        `json:"limit"`
	HasMore bool       `json:"has_more"`
	Stats   ItemStats  `json:"stats"`
}

// ItemQuery 是海报墙查询条件。
type ItemQuery struct {
	StrmTaskID int64
	Offset     int
	Limit      int
	Keyword    string
	Filter     string // all | scored | unscored | pending
	MediaType  string // all | movie | tv
	Sort       string // default | title_asc | year_desc | added_desc | rating_asc
}

// PlanItem 是查询计划里的一行：要不要查，以及原因。
type PlanItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Year   *int   `json:"year,omitempty"`
	Reason string `json:"reason"`
}

// Plan 是「立即查询」弹窗的数据：将查询 / 无需查询两组清单。
type Plan struct {
	StrmTaskID int64      `json:"strm_task_id"`
	TaskName   string     `json:"task_name"`
	Run        []PlanItem `json:"run"`
	Skip       []PlanItem `json:"skip"`
}

// QueryPlan 按当前调度规则（无 TMDB 不查 / 无评分要查 / 日期规则）拆分任务作品。
func (s *Service) QueryPlan(ctx context.Context, strmTaskID int64) (Plan, error) {
	if strmTaskID <= 0 {
		return Plan{}, domain.Errorf(domain.CodeValidation, "strm_task_id 无效")
	}
	taskName, err := s.taskName(ctx, strmTaskID)
	if err != nil {
		return Plan{}, err
	}
	items, err := s.loadAllItems(ctx, strmTaskID)
	if err != nil {
		return Plan{}, err
	}
	scores, err := s.scores.ListByTask(ctx, strmTaskID)
	if err != nil {
		return Plan{}, err
	}
	byID := make(map[string]*domain.BueiDoubanScore, len(scores))
	for _, sc := range scores {
		byID[sc.ItemID] = sc
	}
	now := s.now()
	plan := Plan{StrmTaskID: strmTaskID, TaskName: taskName, Run: []PlanItem{}, Skip: []PlanItem{}}
	for _, item := range items {
		d := decideQuery(byID[item.ID], item, now)
		entry := PlanItem{ID: item.ID, Title: item.Title, Year: item.Year, Reason: d.Reason}
		if d.Query {
			plan.Run = append(plan.Run, entry)
		} else {
			plan.Skip = append(plan.Skip, entry)
		}
	}
	return plan, nil
}

// RunRequest 指定一次查询的范围。
type RunRequest struct {
	StrmTaskID int64
	ItemIDs    []string
	AllTasks   bool
	// Force 为 true 时跳过全部调度规则直接查（卡片「查询 / 重查」用）。
	Force bool
}

// Items 返回海报墙数据（STRM 索引项目 + 豆瓣评分，服务端过滤/排序/分页）。
func (s *Service) Items(ctx context.Context, q ItemQuery) (ItemPage, error) {
	if q.StrmTaskID <= 0 {
		return ItemPage{}, domain.Errorf(domain.CodeValidation, "strm_task_id 无效")
	}
	taskName, err := s.taskName(ctx, q.StrmTaskID)
	if err != nil {
		return ItemPage{}, err
	}
	items, err := s.loadAllItems(ctx, q.StrmTaskID)
	if err != nil {
		return ItemPage{}, err
	}
	scores, err := s.scores.ListByTask(ctx, q.StrmTaskID)
	if err != nil {
		return ItemPage{}, err
	}
	byID := make(map[string]*domain.BueiDoubanScore, len(scores))
	for _, sc := range scores {
		byID[sc.ItemID] = sc
	}

	now := s.now()
	keyword := strings.ToLower(strings.TrimSpace(q.Keyword))
	views := make([]WallItem, 0, len(items))
	stats := ItemStats{Total: len(items)}
	for _, item := range items {
		score := byID[item.ID]
		view := buildWallItem(q.StrmTaskID, taskName, item, score, now)
		if view.Douban != nil && view.Douban.Rating != nil {
			stats.Scored++
		} else {
			stats.Unscored++
		}
		// 「待更新」只统计已有评分但明天还会重新查的作品（日期未知 / 上映或完结不满 1 个月）。
		if score != nil && score.Rating != nil && view.Douban != nil && view.Douban.NeedsQuery {
			stats.Pending++
		}
		switch item.MediaType {
		case strmscrape.MediaTypeTV:
			stats.TV++
		case strmscrape.MediaTypeMovie:
			stats.Movie++
		}
		views = append(views, view)
	}

	// 另起一片，避免和 views 共用底层数组造成不易察觉的别名问题。
	filtered := make([]WallItem, 0, len(views))
	for _, view := range views {
		if !matchFilter(view, q.Filter) || !matchMediaType(view, q.MediaType) || !matchKeyword(view, keyword) {
			continue
		}
		filtered = append(filtered, view)
	}
	sortWallItems(filtered, q.Sort)

	total := len(filtered)
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 60
	}
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return ItemPage{
		Items:   filtered[offset:end],
		Total:   total,
		Offset:  offset,
		Limit:   limit,
		HasMore: end < total,
		Stats:   stats,
	}, nil
}

func (s *Service) loadAllItems(ctx context.Context, strmTaskID int64) ([]strmscrape.Item, error) {
	out := make([]strmscrape.Item, 0, itemsPageSize)
	query := strmscrape.ItemListQuery{
		Limit: itemsPageSize,
		Sort:  strmscrape.ItemListSortTitleAsc,
	}
	for len(out) < maxTaskItems {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		query.Offset = len(out)
		page, err := s.items.ListItems(ctx, strmTaskID, query)
		if err != nil {
			// 输出目录还没生成、被删掉或改名：按空处理，不当成查询失败。
			if isStrmDirMissing(err) {
				return nil, nil
			}
			return nil, err
		}
		out = append(out, page.Items...)
		if !page.HasMore || len(page.Items) == 0 {
			break
		}
	}
	return out, nil
}

func buildWallItem(taskID int64, taskName string, item strmscrape.Item, score *domain.BueiDoubanScore, now time.Time) WallItem {
	view := WallItem{
		ID:          item.ID,
		StrmTaskID:  taskID,
		TaskName:    taskName,
		Title:       item.Title,
		Year:        item.Year,
		MediaType:   item.MediaType,
		TVState:     item.TVState,
		PosterURL:   item.PosterURL,
		Status:      item.Status,
		AddedAt:     item.AddedAt,
		ReleaseDate: item.ReleaseDate,
		EndDate:     item.EndDate,
		TMDBID:      item.TMDBID,
	}
	douban := &ScoreView{NeedsQuery: needsQuery(score, item, now)}
	if score != nil {
		douban.Rating = score.Rating
		douban.Votes = score.Votes
		douban.DoubanID = score.DoubanID
		douban.DoubanTitle = score.DoubanTitle
		douban.DoubanYear = score.DoubanYear
		douban.IsTV = score.IsTV
		douban.Status = score.Status
		douban.Message = score.Message
		douban.QueryTitle = score.QueryTitle
		douban.CheckedAt = formatTS(score.CheckedAt)
		douban.ManualRating = score.ManualRating
		douban.NoAutoQuery = score.NoAutoQuery
	}
	applyScoreOverride(douban, score)
	view.Douban = douban
	return view
}

// applyScoreOverride 把手动评分合并到视图上（豆瓣 ID 只有 DoubanID 一个字段，无需覆盖）。
func applyScoreOverride(view *ScoreView, score *domain.BueiDoubanScore) {
	if view == nil || score == nil || score.ManualRating == nil {
		return
	}
	view.Rating = score.ManualRating
}

func matchFilter(view WallItem, filter string) bool {
	switch strings.TrimSpace(filter) {
	case "scored":
		return view.Douban != nil && view.Douban.Rating != nil
	case "unscored":
		return view.Douban == nil || view.Douban.Rating == nil
	case "pending":
		return view.Douban != nil && view.Douban.Rating != nil && view.Douban.NeedsQuery
	default:
		return true
	}
}

func matchMediaType(view WallItem, mediaType string) bool {
	switch strings.TrimSpace(mediaType) {
	case "movie", "tv":
		return view.MediaType == mediaType
	default:
		return true
	}
}

func matchKeyword(view WallItem, keyword string) bool {
	if keyword == "" {
		return true
	}
	return strings.Contains(strings.ToLower(view.Title), keyword)
}

func sortWallItems(items []WallItem, sortKey string) {
	switch strings.TrimSpace(sortKey) {
	case "title_asc":
		sortSlice(items, func(a, b WallItem) bool { return strings.ToLower(a.Title) < strings.ToLower(b.Title) })
	case "year_desc":
		sortSlice(items, func(a, b WallItem) bool {
			return yearValue(a.Year) > yearValue(b.Year)
		})
	case "added_desc":
		sortSlice(items, func(a, b WallItem) bool { return a.AddedAt > b.AddedAt })
	case "rating_asc":
		sortSlice(items, func(a, b WallItem) bool {
			av, bv := ratingValue(a), ratingValue(b)
			return av < bv
		})
	default:
		// 默认按评分从高到低，未评分的排到最后（同分按标题）。
		sortSlice(items, func(a, b WallItem) bool {
			av, bv := ratingValue(a), ratingValue(b)
			if av != bv {
				return av > bv
			}
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		})
	}
}

func ratingValue(item WallItem) float64 {
	if item.Douban == nil || item.Douban.Rating == nil {
		return -1
	}
	return *item.Douban.Rating
}

func sortSlice(items []WallItem, less func(a, b WallItem) bool) {
	sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j]) })
}

func yearValue(year *int) int {
	if year == nil {
		return 0
	}
	return *year
}

// Tasks 返回左侧任务索引（含已评分数量，来自主库聚合，不访问索引库）。
func (s *Service) Tasks(ctx context.Context) ([]TaskWall, error) {
	tasks, err := s.tasks.ListTasks(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.scores.Counts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TaskWall, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		c := counts[t.ID]
		out = append(out, TaskWall{
			ID:       t.ID,
			Name:     t.Name,
			Status:   t.Status,
			Recorded: c.Total,
			Scored:   c.Scored,
		})
	}
	return out, nil
}

// TaskLibraries 返回 strm_task_id → 关联媒体库映射。
func (s *Service) TaskLibraries(ctx context.Context) (map[int64]domain.BueiDoubanTaskLibrary, error) {
	if s.libs == nil {
		return map[int64]domain.BueiDoubanTaskLibrary{}, nil
	}
	return s.libs.Map(ctx)
}

// SetTaskLibrary 保存任务的关联媒体库；libraryID 为空表示解除关联。
func (s *Service) SetTaskLibrary(ctx context.Context, strmTaskID int64, libraryID, libraryName string) error {
	if s.libs == nil {
		return domain.Errorf(domain.CodeNotImplement, "未启用媒体库关联存储")
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return s.libs.Delete(ctx, strmTaskID)
	}
	return s.libs.Set(ctx, strmTaskID, libraryID, strings.TrimSpace(libraryName))
}

func (s *Service) taskName(ctx context.Context, taskID int64) (string, error) {
	tasks, err := s.tasks.ListTasks(ctx)
	if err != nil {
		return "", err
	}
	for _, t := range tasks {
		if t != nil && t.ID == taskID {
			return t.Name, nil
		}
	}
	return "", domain.Errorf(domain.CodeNotFound, "STRM 任务不存在")
}

// State 返回查询进度与每日调度状态。
func (s *Service) State(ctx context.Context) (StateView, error) {
	st, err := s.loadState(ctx)
	if err != nil {
		return StateView{}, err
	}
	s.mu.Lock()
	progress := s.progress
	s.mu.Unlock()
	runLog, _ := loadRunLog(ctx, s.configs)
	now := s.now()
	return StateView{
		Progress:    progress,
		RunAt:       formatHourMinute(ScheduleHour),
		LastRunAt:   st.LastRunAt,
		NextRunAt:   nextRunAt(now, parseTS(st.LastRunAt)).Format(time.RFC3339),
		LastSummary: st.LastSummary,
		RunLog:      runLog,
	}, nil
}

func (s *Service) loadState(ctx context.Context) (persistedState, error) {
	var st persistedState
	raw, ok, err := s.configs.Get(ctx, stateConfigKey)
	if err != nil || !ok || strings.TrimSpace(raw) == "" {
		return st, err
	}
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		// 状态损坏不影响主流程，按「从未执行」处理。
		return persistedState{}, nil
	}
	return st, nil
}

func (s *Service) saveState(ctx context.Context, st persistedState) {
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	if err := s.configs.Set(ctx, stateConfigKey, string(raw)); err != nil {
		s.log.Warn("保存豆瓣评分调度状态失败", "err", err)
	}
}

func parseTS(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func formatTS(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatHourMinute(hour int) string {
	return time.Date(2000, 1, 1, hour, 0, 0, 0, time.UTC).Format("15:04")
}

// StartQuery 异步启动一次查询，返回初始进度。
func (s *Service) StartQuery(req RunRequest) (Progress, error) {
	req, scope, err := normalizeRunRequest(req)
	if err != nil {
		return Progress{}, err
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return Progress{}, domain.Errorf(domain.CodeValidation, "豆瓣评分查询正在进行中")
	}
	base := s.appCtx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	s.running = true
	s.runCancel = cancel
	s.progress = Progress{
		Running:    true,
		Scope:      scope,
		StrmTaskID: req.StrmTaskID,
		Message:    "正在准备查询",
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
		Items:      make(map[string]*ItemRunStatus),
	}
	progress := s.progress
	s.mu.Unlock()

	s.devf("开始查询（scope=%s，task=%d，指定项目 %d 个，force=%v）", scope, req.StrmTaskID, len(req.ItemIDs), req.Force)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		startedAt := time.Now().UTC()
		runCtx, runCancel := context.WithTimeout(ctx, manualRunTimeout)
		safego.Guard(s.log, "buei.doubanscore", func() {
			s.execute(runCtx, req, scope)
		})
		runCancel()
		cancel()

		s.mu.Lock()
		s.running = false
		s.runCancel = nil
		s.progress.Running = false
		s.progress.CurrentTitle = ""
		s.progress.CurrentItemID = ""
		s.progress.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		if s.progress.Message == "正在准备查询" {
			s.progress.Message = ""
		}
		progress := s.progress
		s.mu.Unlock()

		// buei:devlog —— 开发环境把本轮汇总打到控制台。
		s.devf("本轮结束：待查 %d · 已查 %d · 跳过 %d · 失败 %d · 耗时 %s",
			progress.Total, progress.Queried, progress.Skipped, progress.Failed,
			time.Since(startedAt).Round(time.Second))
		if progress.Error != "" {
			s.devf("本轮错误：%s", progress.Error)
		}

		// 无论成功、失败还是被停止，都记下这次执行时间：
		// 否则失败后每分钟的调度检查都会立刻重试，形成对豆瓣的持续压力。
		// 单条「查询 / 重查」不写调度状态，避免打乱每日计划。
		if scope == "items" {
			return
		}
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer saveCancel()
		s.saveState(saveCtx, persistedState{
			LastRunAt: startedAt.Format(time.RFC3339),
			LastSummary: &Summary{
				Total:      progress.Total,
				Queried:    progress.Queried,
				Skipped:    progress.Skipped,
				Failed:     progress.Failed,
				StartedAt:  startedAt.Format(time.RFC3339),
				FinishedAt: progress.FinishedAt,
			},
		})
	}()
	return progress, nil
}

// StopQuery 停止当前查询（已查到的评分会保留）。
func (s *Service) StopQuery() {
	s.mu.Lock()
	cancel := s.runCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func normalizeRunRequest(req RunRequest) (RunRequest, string, error) {
	req.ItemIDs = sanitizeItemIDs(req.ItemIDs)
	switch {
	case len(req.ItemIDs) > 0:
		if req.StrmTaskID <= 0 {
			return req, "", domain.Errorf(domain.CodeValidation, "按项目查询需要 strm_task_id")
		}
		return req, "items", nil
	case req.AllTasks:
		req.StrmTaskID = 0
		return req, "all", nil
	case req.StrmTaskID > 0:
		return req, "task", nil
	default:
		return req, "", domain.Errorf(domain.CodeValidation, "请指定要查询的任务")
	}
}

func sanitizeItemIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *Service) execute(ctx context.Context, req RunRequest, scope string) {
	runLog := s.beginRunLog(scope)
	tasks, err := s.tasks.ListTasks(ctx)
	if err != nil {
		s.finishWithError(err)
		s.finishRunLog(ctx, runLog)
		return
	}
	targets := make([]*domain.StrmTask, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		if scope != "all" && t.ID != req.StrmTaskID {
			continue
		}
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		s.finishWithError(domain.Errorf(domain.CodeNotFound, "STRM 任务不存在"))
		s.finishRunLog(ctx, runLog)
		return
	}

	// 先装载所有目标作品，便于展示准确的总数。
	type taskItems struct {
		task  *domain.StrmTask
		items []strmscrape.Item
	}
	loaded := make([]taskItems, 0, len(targets))
	itemFilter := make(map[string]struct{}, len(req.ItemIDs))
	for _, id := range req.ItemIDs {
		itemFilter[id] = struct{}{}
	}
	// buei:emby-refresh —— 查到评分的任务收尾登记其媒体库，整轮结束统一刷一次。
	refresher := newLibraryRefresher(s.emby, s.libs, s.log)
	refresher.load(ctx)

	total := 0
	skipReasons := make(map[string]string, len(req.ItemIDs))
	for _, task := range targets {
		items, err := s.loadAllItems(ctx, task.ID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Warn("读取 STRM 索引失败，跳过该任务", "task_id", task.ID, "err", err)
			continue
		}
		if len(itemFilter) > 0 {
			kept := items[:0]
			for _, item := range items {
				if _, ok := itemFilter[item.ID]; ok {
					kept = append(kept, item)
				}
			}
			items = kept
		}
		// 记下跳过原因，弹窗里要给用户看「为什么不用查」。
		now := s.now()
		existing, _ := s.scores.ListByTask(ctx, task.ID)
		scores := make(map[string]*domain.BueiDoubanScore, len(existing))
		for _, sc := range existing {
			scores[sc.ItemID] = sc
		}
		// buei: —— 「n/m」总数只统计真正要查的条目；跳过的排除在外，
		// 并记录原因供弹窗展示（runTask 里按 skipReason 直接跳过）。
		kept := items[:0:0]
		skipCounts := make(map[string]int, 8)
		for _, item := range items {
			// buei:force —— 手动点卡片「查询 / 重查」时（force）不应用任何调度规则：
			// 用户点查询就是要查这一项，不能被「未刮削 TMDB / 已有评分 / 冷静期 /
			// 手动评分 / 不再查询」这类规则拦下。
			if !req.Force {
				if d := decideQuery(scores[item.ID], item, now); !d.Query {
					skipReasons[item.ID] = d.Reason
					skipCounts[d.Reason]++
					continue
				}
			}
			kept = append(kept, item)
		}
		if s.devVerbose {
			if req.Force {
				s.devf("任务「%s」：索引 %d 项，手动查询（force）跳过全部调度规则，待查 %d 项", task.Name, len(items), len(kept))
			} else {
				s.devf("任务「%s」：索引 %d 项，待查 %d 项，跳过 %d 项", task.Name, len(items), len(kept), len(items)-len(kept))
			}
			for reason, count := range skipCounts {
				s.devf("  跳过 %d 项：%s", count, reason)
			}
		}
		loaded = append(loaded, taskItems{task: task, items: kept})
		total += len(kept)
	}

	s.setProgress(func(p *Progress) {
		p.Total = total
		p.Message = "正在查询豆瓣评分"
	})

	for _, entry := range loaded {
		if ctx.Err() != nil {
			break
		}
		if runLog != nil {
			runLog.beginTask(entry.task.ID, entry.task.Name)
		}
		if err := s.runTask(ctx, entry.task.ID, entry.items, skipReasons, req.Force, refresher, runLog); err != nil {
			if ctx.Err() != nil {
				break
			}
			s.log.Warn("豆瓣评分查询中断", "task_id", entry.task.ID, "err", err)
			s.finishWithError(err)
			s.finishRunLog(ctx, runLog)
			return
		}
	}

	if scope == "all" {
		s.pruneMissingTasks(ctx, targets)
	}

	// 无论是否被停止，已写入的评分都触发对应媒体库刷新（用独立上下文，避免取消中断刷库）。
	refresher.flush(context.WithoutCancel(ctx))

	// buei:run-log —— 被停止也落盘（记录已查部分）；用独立上下文避免取消中断保存。
	if runLog != nil {
		saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		runLog.finish(saveCtx, s.configs, s.log)
		saveCancel()
	}

	if ctx.Err() != nil {
		s.setProgress(func(p *Progress) {
			p.Message = "已停止查询"
		})
		return
	}
	s.setProgress(func(p *Progress) {
		p.Message = "查询完成"
	})
}

func (s *Service) runTask(ctx context.Context, taskID int64, items []strmscrape.Item, skipReason map[string]string, force bool, refresher *libraryRefresher, runLog *runLogRecorder) error {
	existingList, err := s.scores.ListByTask(ctx, taskID)
	if err != nil {
		return err
	}
	existing := make(map[string]*domain.BueiDoubanScore, len(existingList))
	for _, sc := range existingList {
		existing[sc.ItemID] = sc
	}

	now := s.now()
	batch := make([]*domain.BueiDoubanScore, 0, upsertBatch)
	// buei:rating —— 本轮查到的评分，结束后统一写进作品 NFO（每任务只扫一次目录）
	nfoRatings := make([]strmscrape.ItemRating, 0, len(items))
	defer func() {
		s.writeNFORatings(ctx, taskID, nfoRatings)
		// buei:emby-refresh —— 确实给这个任务写过评分 NFO 才登记刷它的媒体库。
		if len(nfoRatings) > 0 {
			refresher.collectTask(taskID)
		}
	}()
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.scores.Upsert(ctx, batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	for _, item := range items {
		if err := ctx.Err(); err != nil {
			break
		}
		prev := existing[item.ID]
		if !force && !needsQuery(prev, item, now) {
			s.setProgress(func(p *Progress) {
				p.Done++
				p.Skipped++
				p.Items[item.ID] = &ItemRunStatus{
					Title:  item.Title,
					State:  "skipped",
					Detail: skipReason[item.ID],
				}
			})
			continue
		}
		s.setProgress(func(p *Progress) {
			p.CurrentTitle = item.Title
			p.CurrentItemID = item.ID
		})

		// buei:devlog —— 开发环境把每个项目的查询入参与结果打到控制台（入参日志在 lookupItem 里）。
		res, lookupErr := s.lookupItem(ctx, item, prev)
		if lookupErr != nil && ctx.Err() != nil {
			break
		}
		if lookupErr != nil {
			s.devFailure(lookupErr)
		} else {
			s.devResult(res)
		}
		score := buildScore(taskID, item, prev, res, lookupErr, s.now())
		batch = append(batch, score)
		status := &ItemRunStatus{Title: item.Title, State: "queried"}
		switch {
		case lookupErr != nil:
			status.State = "failed"
			status.Detail = DescribeLookupError(lookupErr)
		case res == nil:
			status.Detail = "豆瓣没有匹配到条目"
		case score.Rating == nil:
			status.Detail = "豆瓣暂无评分"
		default:
			status.Detail = fmt.Sprintf("豆瓣 %.1f 分 · %d 人评价", *score.Rating, score.Votes)
		}
		// buei:run-log —— 每日自动任务记录每个被查项目的名称与结果（失败附具体原因）。
		if runLog != nil {
			runLog.recordItem(item.Title, lookupErr != nil, status.Detail)
		}
		s.setProgress(func(p *Progress) {
			p.Items[item.ID] = status
		})
		if score.Rating != nil {
			// 查询失败/未匹配时会沿用上一次的评分，这里也照写，保证 NFO 与库一致。
			nfoRatings = append(nfoRatings, strmscrape.ItemRating{
				ItemID:   score.ItemID,
				Rating:   *score.Rating,
				Votes:    score.Votes,
				DoubanID: score.DoubanID,
			})
		}
		failed := lookupErr != nil
		s.setProgress(func(p *Progress) {
			p.Done++
			if failed {
				p.Failed++
			} else {
				p.Queried++
			}
		})
		if len(batch) >= upsertBatch {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// writeNFORatings 把本轮评分写进 NFO；查询被中断时也尽量写完已查到的部分。
func (s *Service) writeNFORatings(ctx context.Context, taskID int64, ratings []strmscrape.ItemRating) {
	if s.ratings == nil || len(ratings) == 0 {
		return
	}
	s.devf("任务 %d：写入 NFO 评分 %d 项", taskID, len(ratings))
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if err := s.ratings.UpdateItemRatings(writeCtx, taskID, ratings); err != nil {
		s.log.Warn("写入 NFO 社区评分失败", "task_id", taskID, "err", err)
	}
}

func buildScore(taskID int64, item strmscrape.Item, prev *domain.BueiDoubanScore, res *LookupResult, lookupErr error, now time.Time) *domain.BueiDoubanScore {
	score := &domain.BueiDoubanScore{
		StrmTaskID: taskID,
		ItemID:     item.ID,
		Title:      item.Title,
		Year:       item.Year,
		MediaType:  item.MediaType,
		QueryTitle: cleanQueryTitle(item.Title),
		QueryYear:  item.Year,
		CheckedAt:  now.UTC(),
		UpdatedAt:  now.UTC(),
	}
	// 查询失败或没匹配时保留上一次的成功评分，避免一次风控把已有数据抹掉。
	keepPrev := func() {
		if prev == nil {
			return
		}
		score.Rating = prev.Rating
		score.Votes = prev.Votes
		score.DoubanID = prev.DoubanID
		score.DoubanTitle = prev.DoubanTitle
		score.DoubanYear = prev.DoubanYear
		score.IsTV = prev.IsTV
	}
	switch {
	case lookupErr != nil:
		keepPrev()
		score.Status = domain.BueiDoubanScoreError
		score.Message = truncateMessage(lookupErr.Error())
	case res == nil:
		keepPrev()
		score.Status = domain.BueiDoubanScoreNotFound
		score.Message = "豆瓣没有匹配到条目"
	default:
		score.Status = domain.BueiDoubanScoreMatched
		score.DoubanID = res.DoubanID
		score.DoubanTitle = res.DoubanTitle
		score.DoubanYear = res.DoubanYear
		score.Rating = res.Rating
		score.Votes = res.Votes
		score.IsTV = res.IsTV
		if res.Rating == nil {
			score.Message = "豆瓣暂无评分"
		}
	}
	// 手动覆盖项始终跟随旧记录：手动评分优先展示，不再查询标记也保留。
	if prev != nil {
		score.ManualRating = prev.ManualRating
		score.NoAutoQuery = prev.NoAutoQuery
		if score.ManualRating != nil {
			score.Rating = score.ManualRating
		}
	}
	return score
}

// lookupItem 查询单个作品的豆瓣条目。
// 作品只有一个豆瓣 ID 字段（名称匹配得到的与编辑弹窗填写的都是同一个可信 ID）：
// 只要它非空就直接读该条目的评分，不再按片名搜索；ID 为空时才按片名 + 年份搜索。
// 按 ID 查不到时如实返回错误，由用户自行决定是否清掉 ID 回退到名称匹配。
func (s *Service) lookupItem(ctx context.Context, item strmscrape.Item, prev *domain.BueiDoubanScore) (*LookupResult, error) {
	if prev != nil {
		if id := strings.TrimSpace(prev.DoubanID); id != "" {
			s.devLookupInput(item, id)
			return s.lookupID(ctx, id)
		}
	}
	s.devLookupInput(item, "")
	return s.lookup(ctx, item.Title, item.Year, item.MediaType)
}

func truncateMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	runes := []rune(msg)
	if len(runes) > 200 {
		return string(runes[:200]) + "…"
	}
	return msg
}

func (s *Service) pruneMissingTasks(ctx context.Context, tasks []*domain.StrmTask) {
	alive := make(map[int64]struct{}, len(tasks))
	for _, t := range tasks {
		if t != nil {
			alive[t.ID] = struct{}{}
		}
	}
	counts, err := s.scores.Counts(ctx)
	if err != nil {
		return
	}
	for taskID := range counts {
		if _, ok := alive[taskID]; ok {
			continue
		}
		if err := s.scores.DeleteByTask(ctx, taskID); err != nil {
			s.log.Warn("清理已删除任务的豆瓣评分失败", "task_id", taskID, "err", err)
		}
	}
}

// beginRunLog 每日自动任务（scope=all）创建本轮日志记录器；手动查询不记录。
func (s *Service) beginRunLog(scope string) *runLogRecorder {
	if scope != "all" {
		return nil
	}
	return &runLogRecorder{started: s.now()}
}

// finishRunLog 落盘本轮日志（自动任务）。
func (s *Service) finishRunLog(ctx context.Context, runLog *runLogRecorder) {
	if runLog == nil {
		return
	}
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer saveCancel()
	runLog.finish(saveCtx, s.configs, s.log)
}

func (s *Service) finishWithError(err error) {
	s.setProgress(func(p *Progress) {
		p.Error = truncateMessage(err.Error())
		p.Message = "查询失败"
	})
}

func (s *Service) setProgress(fn func(*Progress)) {
	s.mu.Lock()
	fn(&s.progress)
	s.mu.Unlock()
}

// Start 启动每日 08:00 自动查询。开发环境（SchedulerDisabled）只记录日志，不启动循环。
func (s *Service) Start(ctx context.Context) {
	if s.noSched {
		s.log.Info("开发环境：已跳过豆瓣评分每日自动查询")
		return
	}
	s.mu.Lock()
	if s.loopStop != nil {
		s.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.appCtx = loopCtx
	s.loopStop = cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop(loopCtx)
	}()
}

// Stop 停止调度并等待正在执行的查询结束（最多 stopWaitBudget）。
func (s *Service) Stop(ctx context.Context) {
	s.mu.Lock()
	cancel := s.loopStop
	s.loopStop = nil
	runCancel := s.runCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if runCancel != nil {
		runCancel()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	budget := stopWaitBudget
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < budget {
			budget = remaining
		}
	}
	select {
	case <-done:
	case <-time.After(budget):
		s.log.Warn("豆瓣评分查询停止超时，仍有执行未结束")
	}
}

func (s *Service) loop(ctx context.Context) {
	ticker := time.NewTicker(scheduleTick)
	defer ticker.Stop()
	s.runDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDue(ctx)
		}
	}
}

func (s *Service) runDue(ctx context.Context) {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if running {
		return
	}
	st, err := s.loadState(ctx)
	if err != nil {
		s.log.Warn("读取豆瓣评分调度状态失败", "err", err)
		return
	}
	if !scheduleDue(s.now(), parseTS(st.LastRunAt)) {
		return
	}
	if _, err := s.StartQuery(RunRequest{AllTasks: true}); err != nil {
		s.log.Warn("豆瓣评分自动查询未能启动", "err", err)
		return
	}
	s.log.Info("豆瓣评分自动查询已启动")
}

// isStrmDirMissing 识别刮削索引读取时的“输出目录不存在 / 无效”错误。
// 这类任务的索引必然为空，按「没有作品」处理即可，不必打扰用户。
func isStrmDirMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "输出目录不存在") || strings.Contains(msg, "输出目录无效")
}
