package embyarchive

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/pkg/safego"
)

const (
	schedulerTick    = time.Minute
	manualRunTimeout = 90 * time.Minute
	maxRunItems      = 200
	stopWaitBudget   = 15 * time.Second
)

// Options 构造自动归档服务。
type Options struct {
	Files   FileOps
	Strm    StrmOps
	Scrape  ScrapeOps
	Emby    EmbyOps
	StrmDir string
	Rules   domain.BueiEmbyArchiveRuleRepository
	Runs    domain.BueiEmbyArchiveRunRepository
	Log     *slog.Logger
	Now     func() time.Time
}

// Service 负责归档/移回的执行，以及「归档媒体库」的自动归档配置与定时任务。
type Service struct {
	files   FileOps
	strm    StrmOps
	scrape  ScrapeOps
	emby    EmbyOps
	strmDir string
	rules   domain.BueiEmbyArchiveRuleRepository
	runs    domain.BueiEmbyArchiveRunRepository
	log     *slog.Logger
	now     func() time.Time

	mu      sync.Mutex
	running map[string]struct{}
	appCtx  context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New 构造自动归档服务。
func New(opts Options) *Service {
	return &Service{
		files:   opts.Files,
		strm:    opts.Strm,
		scrape:  opts.Scrape,
		emby:    opts.Emby,
		strmDir: opts.StrmDir,
		rules:   opts.Rules,
		runs:    opts.Runs,
		log:     newLogger(opts.Log),
		now:     opts.Now,
		running: make(map[string]struct{}, 2),
	}
}

// SourceLibraryOption 是配置弹窗里可选的来源媒体库。
type SourceLibraryOption struct {
	ID                   string
	Name                 string
	CollectionType       string
	ClaimedByLibraryID   string
	ClaimedByLibraryName string
}

// RuleView 是配置弹窗所需的配置与可选来源。
type RuleView struct {
	Rule            *domain.BueiEmbyArchiveRule
	SourceLibraries []SourceLibraryOption
	NextRunAt       time.Time
}

// SaveRuleInput 是保存自动归档配置的入参。
type SaveRuleInput struct {
	ConfigID         string
	ArchiveLibraryID string
	SourceLibraryIDs []string
	IntervalHours    int
}

// NormalizeInterval 把执行频率限定在允许的 1/6/24 小时。
func NormalizeInterval(hours int) int {
	switch hours {
	case 6:
		return 6
	case 24:
		return 24
	default:
		return 1
	}
}

// RuleView 读取某个归档媒体库的自动归档配置与可选来源媒体库。
func (s *Service) RuleView(ctx context.Context, configID, archiveLibraryID string) (RuleView, error) {
	libraries, err := s.emby.ListLibraries(ctx, configID)
	if err != nil {
		return RuleView{}, err
	}
	archiveLibrary, ok := libraryByID(libraries, archiveLibraryID)
	if !ok {
		return RuleView{}, domain.Errorf(domain.CodeNotFound, "所选 Emby 媒体库不存在")
	}
	if !strings.Contains(archiveLibrary.Name, archiveWord) {
		return RuleView{}, domain.Errorf(domain.CodeValidation, "只有名称包含“归档”的媒体库支持自动归档")
	}

	rule, err := s.rules.Get(ctx, archiveLibrary.ID)
	if err != nil {
		if !isNotFound(err) {
			return RuleView{}, err
		}
		rule = &domain.BueiEmbyArchiveRule{
			ArchiveLibraryID:   archiveLibrary.ID,
			ArchiveLibraryName: archiveLibrary.Name,
			ConfigID:           strings.TrimSpace(configID),
			SourceLibraries:    []domain.BueiEmbyArchiveSourceLibrary{},
			IntervalHours:      1,
		}
	}
	if rule.ConfigID == "" {
		rule.ConfigID = strings.TrimSpace(configID)
	}
	if name := strings.TrimSpace(archiveLibrary.Name); name != "" {
		rule.ArchiveLibraryName = name
	}

	claimed := s.claimedSources(ctx, archiveLibrary.ID)
	options := make([]SourceLibraryOption, 0, len(libraries))
	for _, library := range libraries {
		if strings.Contains(library.Name, archiveWord) {
			continue
		}
		option := SourceLibraryOption{ID: library.ID, Name: library.Name, CollectionType: library.CollectionType}
		if other, ok := claimed[library.ID]; ok {
			option.ClaimedByLibraryID = other.ArchiveLibraryID
			option.ClaimedByLibraryName = other.ArchiveLibraryName
		}
		options = append(options, option)
	}
	return RuleView{Rule: rule, SourceLibraries: options, NextRunAt: rule.NextRunAt}, nil
}

// SaveRule 校验并保存自动归档配置；同一来源媒体库不能被多个归档媒体库占用。
func (s *Service) SaveRule(ctx context.Context, input SaveRuleInput) (*domain.BueiEmbyArchiveRule, error) {
	configID := strings.TrimSpace(input.ConfigID)
	archiveLibraryID := strings.TrimSpace(input.ArchiveLibraryID)
	if archiveLibraryID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "请选择归档媒体库")
	}
	libraries, err := s.emby.ListLibraries(ctx, configID)
	if err != nil {
		return nil, err
	}
	archiveLibrary, ok := libraryByID(libraries, archiveLibraryID)
	if !ok {
		return nil, domain.Errorf(domain.CodeNotFound, "所选 Emby 媒体库不存在")
	}
	if !strings.Contains(archiveLibrary.Name, archiveWord) {
		return nil, domain.Errorf(domain.CodeValidation, "只有名称包含“归档”的媒体库支持自动归档")
	}

	ids := make([]string, 0, len(input.SourceLibraryIDs))
	seen := make(map[string]struct{}, len(input.SourceLibraryIDs))
	for _, raw := range input.SourceLibraryIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请至少选择一个来源媒体库")
	}
	sources := make([]domain.BueiEmbyArchiveSourceLibrary, 0, len(ids))
	for _, id := range ids {
		library, ok := libraryByID(libraries, id)
		if !ok {
			return nil, domain.Errorf(domain.CodeValidation, "来源媒体库不存在：%s", id)
		}
		if strings.Contains(library.Name, archiveWord) {
			return nil, domain.Errorf(domain.CodeValidation, "来源媒体库不能是归档媒体库：%s", library.Name)
		}
		sources = append(sources, domain.BueiEmbyArchiveSourceLibrary{ID: library.ID, Name: library.Name})
	}

	// 其他归档媒体库已经占用的来源不允许重复选择。
	claimed := s.claimedSources(ctx, archiveLibrary.ID)
	for _, source := range sources {
		other, ok := claimed[source.ID]
		if !ok {
			continue
		}
		return nil, domain.Errorf(domain.CodeValidation, "来源媒体库「%s」已被「%s」的自动归档使用", source.Name, other.ArchiveLibraryName)
	}

	previous, err := s.rules.Get(ctx, archiveLibrary.ID)
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	var lastRunAt, createdAt time.Time
	if previous != nil {
		lastRunAt = previous.LastRunAt
		createdAt = previous.CreatedAt
	}
	interval := NormalizeInterval(input.IntervalHours)
	nextRunAt := nowUTC(s.now).Add(time.Duration(interval) * time.Hour)
	saved, err := s.rules.Save(ctx, &domain.BueiEmbyArchiveRule{
		ArchiveLibraryID:   archiveLibrary.ID,
		ArchiveLibraryName: archiveLibrary.Name,
		ConfigID:           configID,
		SourceLibraries:    sources,
		IntervalHours:      interval,
		LastRunAt:          lastRunAt,
		NextRunAt:          nextRunAt,
		CreatedAt:          createdAt,
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("Emby 自动归档配置已保存",
		"archive_library", archiveLibrary.Name,
		"sources", len(sources),
		"interval_hours", interval,
		"next_run_at", nextRunAt.Format(time.RFC3339),
	)
	return saved, nil
}

// Logs 读取某个归档媒体库最近的执行日志。
func (s *Service) Logs(ctx context.Context, archiveLibraryID string, limit int) ([]*domain.BueiEmbyArchiveRun, error) {
	return s.runs.ListByLibrary(ctx, archiveLibraryID, limit)
}

// StartRun 立即执行一次自动归档（异步），返回刚创建的日志记录。
func (s *Service) StartRun(ctx context.Context, archiveLibraryID, trigger string) (*domain.BueiEmbyArchiveRun, error) {
	rule, err := s.rules.Get(ctx, archiveLibraryID)
	if err != nil {
		if isNotFound(err) {
			return nil, domain.Errorf(domain.CodeValidation, "该归档媒体库还没有配置来源媒体库，请先打开“配置”")
		}
		return nil, err
	}
	run, err := s.launch(ctx, rule, trigger)
	if err != nil {
		return nil, err
	}
	baseCtx := s.baseContext()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		runCtx, cancel := context.WithTimeout(baseCtx, manualRunTimeout)
		defer cancel()
		safego.Guard(s.log, "buei.embyarchive", func() {
			s.execute(runCtx, rule, run)
		})
	}()
	return run, nil
}

// Start 启动定时归档任务。
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.appCtx = loopCtx
	s.cancel = cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop(loopCtx)
	}()
}

// Stop 停止定时任务并等待正在执行的归档结束（最多 stopWaitBudget）。
func (s *Service) Stop(ctx context.Context) {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
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
		s.log.Warn("Emby 自动归档停止超时，仍有执行未结束")
	}
}

func (s *Service) loop(ctx context.Context) {
	ticker := time.NewTicker(schedulerTick)
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
	rules, err := s.rules.List(ctx)
	if err != nil {
		s.log.Warn("读取 Emby 自动归档配置失败", "err", err)
		return
	}
	now := nowUTC(s.now)
	for _, rule := range rules {
		if rule == nil || rule.NextRunAt.IsZero() || now.Before(rule.NextRunAt) {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		run, err := s.launch(ctx, rule, domain.BueiArchiveTriggerAuto)
		if err != nil {
			s.log.Warn("Emby 自动归档未能启动", "archive_library", rule.ArchiveLibraryName, "err", err)
			continue
		}
		safego.Guard(s.log, "buei.embyarchive", func() {
			s.execute(ctx, rule, run)
		})
	}
}

func (s *Service) baseContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appCtx != nil {
		return s.appCtx
	}
	return context.Background()
}

func (s *Service) launch(ctx context.Context, rule *domain.BueiEmbyArchiveRule, trigger string) (*domain.BueiEmbyArchiveRun, error) {
	if rule == nil {
		return nil, domain.Errorf(domain.CodeValidation, "自动归档配置不存在")
	}
	if len(rule.SourceLibraries) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请先配置来源媒体库")
	}
	if !s.tryBegin(rule.ArchiveLibraryID) {
		return nil, domain.Errorf(domain.CodeValidation, "自动归档正在执行，请稍后再试")
	}
	// 上一次进程退出时留下的 running 记录先标记为中断，避免日志一直显示执行中。
	if err := s.runs.FailInterrupted(ctx, rule.ArchiveLibraryID); err != nil {
		s.log.Warn("清理中断的自动归档日志失败", "err", err)
	}
	created, err := s.runs.Create(ctx, &domain.BueiEmbyArchiveRun{
		ArchiveLibraryID:   rule.ArchiveLibraryID,
		ArchiveLibraryName: rule.ArchiveLibraryName,
		Trigger:            trigger,
		Status:             domain.BueiArchiveRunRunning,
		StartedAt:          nowUTC(s.now),
		Items:              []domain.BueiEmbyArchiveRunItem{},
	})
	if err != nil {
		s.endRun(rule.ArchiveLibraryID)
		return nil, err
	}
	return created, nil
}

func (s *Service) execute(ctx context.Context, rule *domain.BueiEmbyArchiveRule, run *domain.BueiEmbyArchiveRun) {
	if rule == nil || run == nil {
		return
	}
	defer s.endRun(rule.ArchiveLibraryID)

	err := s.runRule(ctx, rule, run)
	run.FinishedAt = nowUTC(s.now)
	switch {
	case err != nil && run.Archived == 0:
		run.Status = domain.BueiArchiveRunFailed
	case err != nil || run.Failed > 0:
		run.Status = domain.BueiArchiveRunPartial
	default:
		run.Status = domain.BueiArchiveRunSuccess
	}
	if err != nil {
		run.Message = joinMessage(run.Message, err.Error())
	}

	finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if finishErr := s.runs.Finish(finishCtx, run); finishErr != nil {
		s.log.Warn("保存 Emby 自动归档日志失败", "err", finishErr)
	}
	nextRunAt := run.FinishedAt.Add(time.Duration(NormalizeInterval(rule.IntervalHours)) * time.Hour)
	if updateErr := s.rules.UpdateSchedule(finishCtx, rule.ArchiveLibraryID, run.StartedAt, nextRunAt); updateErr != nil {
		s.log.Warn("更新 Emby 自动归档排期失败", "err", updateErr)
	}
	s.log.Info("Emby 自动归档执行结束",
		"archive_library", run.ArchiveLibraryName,
		"trigger", run.Trigger,
		"archived", run.Archived,
		"failed", run.Failed,
		"skipped", run.Skipped,
		"status", run.Status,
	)
}

func (s *Service) tryBegin(archiveLibraryID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running == nil {
		s.running = make(map[string]struct{}, 2)
	}
	key := strings.TrimSpace(archiveLibraryID)
	if _, ok := s.running[key]; ok {
		return false
	}
	s.running[key] = struct{}{}
	return true
}

func (s *Service) endRun(archiveLibraryID string) {
	s.mu.Lock()
	delete(s.running, strings.TrimSpace(archiveLibraryID))
	s.mu.Unlock()
}

func (s *Service) claimedSources(ctx context.Context, archiveLibraryID string) map[string]*domain.BueiEmbyArchiveRule {
	claimed := make(map[string]*domain.BueiEmbyArchiveRule)
	rules, err := s.rules.List(ctx)
	if err != nil {
		s.log.Warn("读取 Emby 自动归档配置失败", "err", err)
		return claimed
	}
	for _, rule := range rules {
		if rule == nil || strings.TrimSpace(rule.ArchiveLibraryID) == strings.TrimSpace(archiveLibraryID) {
			continue
		}
		for _, source := range rule.SourceLibraries {
			claimed[strings.TrimSpace(source.ID)] = rule
		}
	}
	return claimed
}

func libraryByID(libraries []embyproxy.Library, id string) (embyproxy.Library, bool) {
	id = strings.TrimSpace(id)
	for _, library := range libraries {
		if library.ID == id {
			return library, true
		}
	}
	return embyproxy.Library{}, false
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	appErr, ok := domain.AsAppError(err)
	return ok && appErr.Code == domain.CodeNotFound
}

func joinMessage(current, extra string) string {
	parts := make([]string, 0, 2)
	for _, part := range []string{current, extra} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	joined := strings.Join(parts, "；")
	if runes := []rune(joined); len(runes) > 400 {
		return string(runes[:400]) + "…"
	}
	return joined
}

func appendRunItem(run *domain.BueiEmbyArchiveRun, item domain.BueiEmbyArchiveRunItem) {
	if run == nil {
		return
	}
	if len(run.Items) >= maxRunItems {
		return
	}
	run.Items = append(run.Items, item)
}
