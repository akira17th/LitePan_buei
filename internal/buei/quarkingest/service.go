package quarkingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"litepan/internal/domain"
	"litepan/pkg/safego"
)

const (
	// presetConfigPrefix 按夸克账号保存入库预设，避免不同账号互相覆盖。
	presetConfigPrefix = "buei_quark_ingest_preset_"

	// 入库后生成 STRM 的超时；失败只记录日志，不影响入库结果。
	strmGenerateTimeout = 60 * time.Second
	// 入库后自动刮削（重建索引 + 启动刮削）的超时。
	scrapeTimeout = 2 * time.Minute
	// Emby 刷库请求本身的超时；等待刮削完成另算。
	embyRefreshTimeout = time.Minute
	// 等待刮削完成的最长时间（后台执行，不阻塞入库请求）。
	embyWaitTimeout = 6 * time.Hour

	maxTargetPathSegments = 8
	maxFolderNameRunes    = 100
	maxRenameNameRunes    = 200
	maxIngestFiles        = 20000
)

// FileOps 是入库需要的文件操作子集，便于单元测试替换。
type FileOps interface {
	List(ctx context.Context, accountID int64, parentID string, forceRefresh bool) ([]domain.FileItem, error)
	CreateFolder(ctx context.Context, accountID int64, parentID, name string) (*domain.FileItem, error)
	MoveFiles(ctx context.Context, accountID int64, fileIDs []string, targetParentID, sourceParentID string) error
	RenameFile(ctx context.Context, accountID int64, fileID, newName, parentID string) error
	DeleteFiles(ctx context.Context, accountID int64, fileIDs []string, parentID string) error
}

// AccountReader 只读取账号信息，避免依赖完整仓储接口。
type AccountReader interface {
	Get(ctx context.Context, id int64) (*domain.Account, error)
}

// ConfigStore 保存入库预设。
type ConfigStore interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	Set(ctx context.Context, key, value string) error
}

// Options 构造夸克入库服务。
type Options struct {
	Files     FileOps
	Accounts  AccountReader
	Configs   ConfigStore
	Favorites domain.QuarkIngestFavoriteRepository
	Log       *slog.Logger
}

// RenameRule 是一条替换文本规则。
type RenameRule struct {
	Find        string `json:"find"`
	ReplaceWith string `json:"replace_with"`
}

// RenameReplace 是重命名中的替换文本预设。
type RenameReplace struct {
	Enabled     bool         `json:"enabled"`
	Find        string       `json:"find"`
	ReplaceWith string       `json:"replace_with"`
	Rules       []RenameRule `json:"rules,omitempty"`
}

// NormalizeRules 过滤空规则，并兼容只有 Find/ReplaceWith 的旧结构。
func (r RenameReplace) NormalizeRules() []RenameRule {
	raw := r.Rules
	if len(raw) == 0 && strings.TrimSpace(r.Find) != "" {
		raw = []RenameRule{{Find: r.Find, ReplaceWith: r.ReplaceWith}}
	}
	out := make([]RenameRule, 0, len(raw))
	for _, rule := range raw {
		if strings.TrimSpace(rule.Find) == "" {
			continue
		}
		out = append(out, RenameRule{Find: rule.Find, ReplaceWith: rule.ReplaceWith})
	}
	return out
}

// Preset 是某个夸克账号上次入库的保存位置与联动动作预设。
type Preset struct {
	AccountID     int64         `json:"account_id"`
	ParentID      string        `json:"parent_id"`
	BasePath      string        `json:"base_path"`
	BaseDirs      []string      `json:"base_dirs,omitempty"`
	Title         string        `json:"title"`
	Season        int           `json:"season"`
	IsSeries      bool          `json:"is_series"`
	AutoStrm      bool          `json:"auto_strm"`
	AutoScrape    bool          `json:"auto_scrape"`
	ScrapeTaskID  int64         `json:"auto_scrape_task_id"`
	RefreshEmby   bool          `json:"refresh_emby"`
	EmbyConfigID  string        `json:"emby_config_id"`
	EmbyMode      string        `json:"emby_mode"`
	EmbyLibraryID string        `json:"emby_library_id"`
	RenameReplace RenameReplace `json:"rename_replace"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// IngestFile 是本次入库的一个文件或文件夹。
type IngestFile struct {
	FileID       string
	OriginalName string
	NewName      string
	IsDir        bool
}

// IngestInput 是入库入参。
type IngestInput struct {
	AccountID int64
	// FavoriteID 是本次入库对应的收藏记录；用于同步收藏文件夹改名。
	FavoriteID int64
	// SourceParentID 是收藏文件夹的 id。
	SourceParentID string
	// SourceFolderParentID 是收藏文件夹的父目录（「来自：分享」）id。
	SourceFolderParentID string
	// SourceFolderName 是收藏文件夹当前名称，用于判断是否需要改名。
	SourceFolderName string
	FileIDs          []string
	// Files 是本次入库的文件清单，用于重命名；文件夹不会入库。
	Files []IngestFile

	// ParentID 是保存位置基础目录；TargetPath 是相对它新建/进入的层级。
	ParentID   string
	TargetPath []string
	BasePath   string
	BaseDirs   []string

	Title    string
	Season   int
	IsSeries bool

	AutoStrm     bool
	AutoScrape   bool
	ScrapeTaskID int64

	RefreshEmby   bool
	EmbyConfigID  string
	EmbyMode      string
	EmbyLibraryID string

	RenameReplace *RenameReplace
	ProgressID    string
}

// Result 是入库结果。
type Result struct {
	Renamed       int    `json:"renamed"`
	RenameSkipped int    `json:"rename_skipped"`
	Moved         int    `json:"moved"`
	StrmGenerated int64  `json:"strm_generated"`
	StrmMatched   bool   `json:"strm_matched"`
	StrmError     string `json:"strm_error,omitempty"`

	ScrapeStarted bool   `json:"scrape_started"`
	ScrapeError   string `json:"scrape_error,omitempty"`

	EmbyRefreshed bool   `json:"emby_refreshed"`
	EmbyScheduled bool   `json:"emby_scheduled"`
	EmbyError     string `json:"emby_error,omitempty"`
}

// RenamePair 描述一次入库后的重命名。
type RenamePair struct {
	FileID  string
	OldName string
	NewName string
}

// Service 负责把「来自：分享」中的文件移动到目标目录并执行联动动作。
type Service struct {
	files     FileOps
	accounts  AccountReader
	configs   ConfigStore
	favorites domain.QuarkIngestFavoriteRepository
	log       *slog.Logger

	strmGen            func(ctx context.Context, accountID int64, parentID string, dirs []string) (int64, bool, error)
	rebuildScrapeIndex func(ctx context.Context, strmTaskID int64) error
	startScrape        func(ctx context.Context, strmTaskID int64, remoteDirs []string) error
	waitScrapeDone     func(ctx context.Context, strmTaskID int64) error
	refreshEmby        func(ctx context.Context, configID, mode, libraryID string) error

	progress *progressTracker
}

// New 创建服务。
func New(opts Options) *Service {
	logger := opts.Log
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		files:     opts.Files,
		accounts:  opts.Accounts,
		configs:   opts.Configs,
		favorites: opts.Favorites,
		log:       logger,
		progress:  newProgressTracker(),
	}
}

// SetFiles 注入文件操作；装配阶段 file.Service 晚于 buei 服务创建。
func (s *Service) SetFiles(files FileOps) {
	if s == nil {
		return
	}
	s.files = files
}

// SetStrmGenerator 注入入库后自动生成 STRM 的适配器。
func (s *Service) SetStrmGenerator(gen func(ctx context.Context, accountID int64, parentID string, dirs []string) (int64, bool, error)) {
	if s == nil {
		return
	}
	s.strmGen = gen
}

// SetScrapeRunner 注入自动刮削的三个步骤：重建索引、启动刮削与等待刮削结束。
func (s *Service) SetScrapeRunner(
	rebuild func(ctx context.Context, strmTaskID int64) error,
	start func(ctx context.Context, strmTaskID int64, remoteDirs []string) error,
	wait func(ctx context.Context, strmTaskID int64) error,
) {
	if s == nil {
		return
	}
	s.rebuildScrapeIndex = rebuild
	s.startScrape = start
	s.waitScrapeDone = wait
}

// SetEmbyRefresher 注入 Emby/Jellyfin 刷库调用。
func (s *Service) SetEmbyRefresher(refresh func(ctx context.Context, configID, mode, libraryID string) error) {
	if s == nil {
		return
	}
	s.refreshEmby = refresh
}

// TransferProgress 返回一次入库的阶段进度，供前端轮询展示。
func (s *Service) TransferProgress(id string) (Progress, bool) {
	if s == nil {
		return Progress{}, false
	}
	return s.progress.get(id)
}

// LoadPreset 读取指定账号上次成功入库的预设；不存在时返回 nil, nil。
func (s *Service) LoadPreset(ctx context.Context, accountID int64) (*Preset, error) {
	if s == nil || s.configs == nil || accountID <= 0 {
		return nil, nil
	}
	raw, ok, err := s.configs.Get(ctx, presetKey(accountID))
	if err != nil {
		return nil, err
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var preset Preset
	if err := json.Unmarshal([]byte(raw), &preset); err != nil {
		s.logger().Warn("解析夸克入库预设失败", "account_id", accountID, "error", err)
		return nil, nil
	}
	preset.AccountID = accountID
	preset.ParentID = strings.TrimSpace(preset.ParentID)
	preset.BasePath = normalizeDestinationBasePath(preset.BasePath)
	preset.BaseDirs = cleanDirs(preset.BaseDirs)
	preset.Season = clampSeason(preset.Season)
	preset.EmbyMode = normalizeEmbyMode(preset.EmbyMode)
	preset.RenameReplace = normalizeRenameReplace(preset.RenameReplace)
	return &preset, nil
}

// Ingest 把来源文件夹中选中的条目移动到保存位置，并按需重命名、生成 STRM、刮削与刷库。
func (s *Service) Ingest(ctx context.Context, in IngestInput) (out *Result, err error) {
	if s == nil || s.files == nil || s.accounts == nil {
		return nil, domain.Errorf(domain.CodeInternal, "夸克入库服务未初始化")
	}
	account, err := s.accounts.Get(ctx, in.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, domain.Errorf(domain.CodeNotFound, "账号不存在")
	}
	if account.DriverType != "quark" {
		return nil, domain.Errorf(domain.CodeValidation, "夸克入库只支持夸克网盘账号")
	}
	if !account.IsActive {
		return nil, domain.Errorf(domain.CodeValidation, "目标账号已停用")
	}

	sourceParentID := normalizeParentID(in.SourceParentID)
	if sourceParentID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "请在「来自：分享」文件夹内使用入库")
	}
	targetPath, err := normalizeTargetPath(in.TargetPath)
	if err != nil {
		return nil, err
	}
	files := normalizeIngestFiles(in.Files, in.FileIDs)
	// 夸克入库只移动收藏文件夹中的文件，文件夹本身留在「来自：分享」中。
	fileOnly := files[:0]
	for _, file := range files {
		if file.IsDir {
			continue
		}
		fileOnly = append(fileOnly, file)
	}
	files = fileOnly
	if len(files) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请选择要入库的文件（不支持移动文件夹）")
	}
	if len(files) > maxIngestFiles {
		return nil, domain.Errorf(domain.CodeValidation, "一次最多入库 %d 个文件", maxIngestFiles)
	}

	progressID := strings.TrimSpace(in.ProgressID)
	s.progress.start(progressID)
	// 刮削/刷库是后台异步动作：它们结束前不要把进度标记为完成。
	asyncPending := false
	defer func() {
		if asyncPending {
			return
		}
		if err != nil {
			s.progress.finish(progressID, err.Error())
			return
		}
		s.progress.finish(progressID, "")
	}()

	targetParentID, err := s.ensureTargetFolder(ctx, in.AccountID, in.ParentID, targetPath)
	if err != nil {
		return nil, err
	}
	if targetParentID == sourceParentID {
		return nil, domain.Errorf(domain.CodeValidation, "保存位置与收藏文件夹相同，请选择其他目录")
	}

	// 目标文件夹名与收藏文件夹名保持一致；用户修改标题时同步重命名收藏文件夹。
	if in.FavoriteID > 0 {
		folderName := sanitizeFolderName(in.Title)
		if folderName == "" {
			return nil, domain.Errorf(domain.CodeValidation, "目标文件夹名称不能为空")
		}
		if strings.TrimSpace(in.SourceFolderName) != folderName {
			parentID := strings.TrimSpace(in.SourceFolderParentID)
			if parentID == "" {
				return nil, domain.Errorf(domain.CodeValidation, "缺少收藏文件夹的父目录，无法重命名")
			}
			if err := s.files.RenameFile(ctx, in.AccountID, sourceParentID, folderName, parentID); err != nil {
				s.logger().Warn("夸克入库重命名收藏文件夹失败", "account_id", in.AccountID, "folder_id", sourceParentID, "new_name", folderName, "error", err)
				return nil, err
			}
			if s.favorites != nil {
				if err := s.favorites.UpdateFolderName(ctx, in.FavoriteID, folderName); err != nil {
					s.logger().Warn("更新夸克入库收藏文件夹名称失败", "favorite_id", in.FavoriteID, "name", folderName, "error", err)
				}
			}
		}
	}

	fileIDs := make([]string, 0, len(files))
	for _, file := range files {
		fileIDs = append(fileIDs, file.FileID)
	}
	if err := s.files.MoveFiles(ctx, in.AccountID, fileIDs, targetParentID, sourceParentID); err != nil {
		s.logger().Warn("夸克入库移动文件失败", "account_id", in.AccountID, "source", sourceParentID, "target", targetParentID, "count", len(fileIDs), "error", err)
		return nil, err
	}

	result := &Result{Moved: len(files)}
	plan := buildRenamePlan(files)
	if len(plan) > 0 {
		s.progress.setPhase(progressID, PhaseRename)
		result.Renamed, result.RenameSkipped = s.applyRenames(ctx, in.AccountID, targetParentID, plan)
	}
	s.savePreset(ctx, in, targetPath)

	strmDirs := targetDirectorySegments(ingestBaseDirs(in), targetPath)
	if in.AutoStrm {
		s.progress.setPhase(progressID, PhaseStrm)
		result.StrmGenerated, result.StrmMatched, result.StrmError = s.generateStrm(ctx, in.AccountID, targetParentID, strmDirs)
	}
	emby := in.embyAction()
	if scrapeEnabled(in) {
		s.progress.setPhase(progressID, PhaseScrape)
		result.ScrapeStarted, result.ScrapeError = s.scrapeAfterTransfer(ctx, in.ScrapeTaskID, strmDirs)
	}
	if result.ScrapeStarted {
		asyncPending = true
		if emby.enabled {
			result.EmbyScheduled = true
			s.scheduleEmbyAfterScrape(progressID, in.ScrapeTaskID, emby)
		} else {
			s.scheduleScrapeFinish(progressID, in.ScrapeTaskID)
		}
	} else if emby.enabled {
		s.progress.setPhase(progressID, PhaseEmby)
		result.EmbyRefreshed, result.EmbyError = s.refreshEmbyNow(ctx, emby)
	}

	if result.StrmGenerated > 0 {
		s.logger().Info("夸克入库后自动生成 STRM", "account_id", in.AccountID, "parent_id", targetParentID, "dirs", strmDirs, "count", result.StrmGenerated)
	}
	s.logger().Info("夸克入库完成", "account_id", in.AccountID, "source", sourceParentID, "target", targetParentID, "count", result.Moved, "renamed", result.Renamed)
	return result, nil
}

func presetKey(accountID int64) string {
	return fmt.Sprintf("%s%d", presetConfigPrefix, accountID)
}

func (s *Service) logger() *slog.Logger {
	if s != nil && s.log != nil {
		return s.log
	}
	return slog.Default()
}

// ensureTargetFolder 在入库账号内按层级查找目录，不存在时创建。
func (s *Service) ensureTargetFolder(ctx context.Context, accountID int64, parentID string, segments []string) (string, error) {
	parentID = strings.TrimSpace(parentID)
	for _, name := range segments {
		items, err := s.files.List(ctx, accountID, parentID, true)
		if err != nil {
			return "", err
		}
		foundID := ""
		for _, item := range items {
			if item.Name != name {
				continue
			}
			if !item.IsDir {
				return "", domain.Errorf(domain.CodeValidation, "目标目录下已存在同名文件：%s", name)
			}
			foundID = item.ID
			break
		}
		if foundID != "" {
			parentID = foundID
			continue
		}
		created, err := s.files.CreateFolder(ctx, accountID, parentID, name)
		if err != nil {
			return "", err
		}
		if created == nil || strings.TrimSpace(created.ID) == "" {
			return "", domain.Errorf(domain.CodeDriverError, "创建文件夹失败：%s", name)
		}
		parentID = created.ID
	}
	return parentID, nil
}

// applyRenames 在目标目录中按文件 id 重命名；失败的文件跳过并继续。
func (s *Service) applyRenames(ctx context.Context, accountID int64, parentID string, plan []RenamePair) (int, int) {
	renamed, skipped := 0, 0
	for _, pair := range plan {
		if err := s.files.RenameFile(ctx, accountID, pair.FileID, pair.NewName, parentID); err != nil {
			s.logger().Warn("夸克入库后重命名失败", "account_id", accountID, "parent_id", parentID, "name", pair.OldName, "new_name", pair.NewName, "error", err)
			skipped++
			continue
		}
		renamed++
	}
	return renamed, skipped
}

// savePreset 保存本次保存位置和联动动作偏好；失败只记录日志。
func (s *Service) savePreset(ctx context.Context, in IngestInput, targetPath []string) {
	if s == nil || s.configs == nil || in.AccountID <= 0 {
		return
	}
	title := sanitizeFolderName(in.Title)
	if len(targetPath) > 0 {
		title = targetPath[0]
	}
	var renameReplace RenameReplace
	if in.RenameReplace != nil {
		renameReplace = normalizeRenameReplace(*in.RenameReplace)
	}
	preset := Preset{
		AccountID:     in.AccountID,
		ParentID:      strings.TrimSpace(in.ParentID),
		BasePath:      normalizeDestinationBasePath(in.BasePath),
		BaseDirs:      cleanDirs(in.BaseDirs),
		Title:         title,
		Season:        clampSeason(in.Season),
		IsSeries:      in.IsSeries,
		AutoStrm:      in.AutoStrm,
		AutoScrape:    in.AutoScrape,
		ScrapeTaskID:  in.ScrapeTaskID,
		RefreshEmby:   in.RefreshEmby,
		EmbyConfigID:  strings.TrimSpace(in.EmbyConfigID),
		EmbyMode:      normalizeEmbyMode(in.EmbyMode),
		EmbyLibraryID: strings.TrimSpace(in.EmbyLibraryID),
		RenameReplace: renameReplace,
		UpdatedAt:     time.Now().UTC(),
	}
	data, err := json.Marshal(preset)
	if err != nil {
		s.logger().Warn("序列化夸克入库预设失败", "account_id", in.AccountID, "error", err)
		return
	}
	if err := s.configs.Set(ctx, presetKey(in.AccountID), string(data)); err != nil {
		s.logger().Warn("保存夸克入库预设失败", "account_id", in.AccountID, "error", err)
	}
}

// embyAction 是一次 Emby 刷库的参数。
type embyAction struct {
	enabled   bool
	configID  string
	mode      string
	libraryID string
}

func (in IngestInput) embyAction() embyAction {
	return embyAction{
		enabled:   in.RefreshEmby,
		configID:  strings.TrimSpace(in.EmbyConfigID),
		mode:      normalizeEmbyMode(in.EmbyMode),
		libraryID: strings.TrimSpace(in.EmbyLibraryID),
	}
}

func normalizeEmbyMode(raw string) string {
	if strings.TrimSpace(raw) == "library" {
		return "library"
	}
	return "global"
}

// refreshEmbyNow 立即触发一次 Emby 刷库；失败只记录日志并回传原因。
func (s *Service) refreshEmbyNow(ctx context.Context, action embyAction) (bool, string) {
	if s == nil || s.refreshEmby == nil || !action.enabled {
		return false, ""
	}
	runCtx, cancel := context.WithTimeout(ctx, embyRefreshTimeout)
	defer cancel()
	if err := s.refreshEmby(runCtx, action.configID, action.mode, action.libraryID); err != nil {
		s.logger().Warn("夸克入库后 Emby 刷库失败", "config_id", action.configID, "mode", action.mode, "error", err)
		return false, err.Error()
	}
	s.logger().Info("夸克入库后已触发 Emby 刷库", "config_id", action.configID, "mode", action.mode, "library_id", action.libraryID)
	return true, ""
}

// scheduleScrapeFinish 在后台等刮削结束后结束进度条目（未开启刷库时）。
func (s *Service) scheduleScrapeFinish(progressID string, scrapeTaskID int64) {
	go func() {
		safego.Guard(s.logger(), "buei.quarkingest.scrape", func() {
			s.waitScrape(scrapeTaskID)
			s.progress.finish(progressID, "")
		})
	}()
}

// scheduleEmbyAfterScrape 在后台等刮削结束后再刷库，避免阻塞入库请求。
func (s *Service) scheduleEmbyAfterScrape(progressID string, scrapeTaskID int64, action embyAction) {
	go func() {
		safego.Guard(s.logger(), "buei.quarkingest.emby", func() {
			s.waitAndRefreshEmby(progressID, scrapeTaskID, action)
		})
	}()
}

// waitScrape 等待刮削结束（最长 embyWaitTimeout）；失败只记日志，后续动作照常执行。
func (s *Service) waitScrape(scrapeTaskID int64) {
	if s.waitScrapeDone == nil || scrapeTaskID <= 0 {
		return
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), embyWaitTimeout)
	if err := s.waitScrapeDone(waitCtx, scrapeTaskID); err != nil {
		s.logger().Warn("等待刮削结束超时，仍继续后续联动动作", "strm_task_id", scrapeTaskID, "error", err)
	}
	cancel()
}

// waitAndRefreshEmby 等待刮削结束（最长 embyWaitTimeout）后刷库，并把最终结果写回进度。
func (s *Service) waitAndRefreshEmby(progressID string, scrapeTaskID int64, action embyAction) {
	s.waitScrape(scrapeTaskID)
	if !action.enabled {
		s.progress.finish(progressID, "")
		return
	}
	s.progress.setPhase(progressID, PhaseEmby)
	_, errText := s.refreshEmbyNow(context.Background(), action)
	s.progress.finish(progressID, errText)
}

// scrapeEnabled 判断本次是否需要自动刮削：必须先生成 STRM，并选中了刮削任务。
func scrapeEnabled(in IngestInput) bool {
	return in.AutoStrm && in.AutoScrape && in.ScrapeTaskID > 0
}

// scrapeAfterTransfer 先重建刮削索引，再启动刮削；remoteDirs 用于把刮削范围限定到本次入库的作品。
// 失败只记录日志并回传原因。
func (s *Service) scrapeAfterTransfer(ctx context.Context, strmTaskID int64, remoteDirs []string) (bool, string) {
	if s == nil || strmTaskID <= 0 || s.rebuildScrapeIndex == nil || s.startScrape == nil {
		return false, ""
	}
	runCtx, cancel := context.WithTimeout(ctx, scrapeTimeout)
	defer cancel()
	if err := s.rebuildScrapeIndex(runCtx, strmTaskID); err != nil {
		s.logger().Warn("夸克入库后重建刮削索引失败", "strm_task_id", strmTaskID, "error", err)
		return false, err.Error()
	}
	if err := s.startScrape(runCtx, strmTaskID, remoteDirs); err != nil {
		s.logger().Warn("夸克入库后启动刮削失败", "strm_task_id", strmTaskID, "dirs", remoteDirs, "error", err)
		return false, err.Error()
	}
	s.logger().Info("夸克入库后已启动自动刮削", "strm_task_id", strmTaskID, "dirs", remoteDirs)
	return true, ""
}

// generateStrm 在目标目录生成 STRM；失败只记录日志并回传给前端提示，不影响入库结果。
func (s *Service) generateStrm(ctx context.Context, accountID int64, parentID string, dirs []string) (int64, bool, string) {
	if s == nil || s.strmGen == nil {
		return 0, false, ""
	}
	runCtx, cancel := context.WithTimeout(ctx, strmGenerateTimeout)
	defer cancel()
	generated, matched, err := s.strmGen(runCtx, accountID, parentID, dirs)
	if err != nil {
		s.logger().Warn("夸克入库后自动生成 STRM 失败", "account_id", accountID, "parent_id", parentID, "dirs", dirs, "error", err)
		return 0, false, err.Error()
	}
	if !matched {
		s.logger().Info("夸克入库目标目录未匹配 STRM 任务，跳过自动生成", "account_id", accountID, "dirs", dirs)
	}
	return generated, matched, ""
}

// targetDirectorySegments 拼出目标目录相对账号根的目录段数组。
func targetDirectorySegments(baseDirs, targetPath []string) []string {
	dirs := make([]string, 0, len(baseDirs)+len(targetPath))
	for _, dir := range append(append([]string{}, baseDirs...), targetPath...) {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			dirs = append(dirs, trimmed)
		}
	}
	return dirs
}

// normalizeIngestFiles 清洗本次入库清单；FileIDs 是移动的权威来源。
func normalizeIngestFiles(raw []IngestFile, ids []string) []IngestFile {
	meta := make(map[string]IngestFile, len(raw))
	for _, file := range raw {
		id := strings.TrimSpace(file.FileID)
		if id == "" {
			continue
		}
		meta[id] = IngestFile{
			FileID:       id,
			OriginalName: strings.TrimSpace(file.OriginalName),
			NewName:      sanitizeRenameName(file.NewName),
			IsDir:        file.IsDir,
		}
	}
	sourceIDs := normalizeFileIDs(ids)
	if len(sourceIDs) == 0 {
		seen := make(map[string]struct{}, len(raw))
		for _, file := range raw {
			id := strings.TrimSpace(file.FileID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			sourceIDs = append(sourceIDs, id)
		}
	}
	files := make([]IngestFile, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		file, ok := meta[id]
		if !ok {
			file = IngestFile{FileID: id}
		}
		if file.OriginalName == "" {
			file.OriginalName = file.NewName
		}
		if file.NewName == "" || file.IsDir {
			file.NewName = file.OriginalName
		}
		files = append(files, file)
	}
	return files
}

// buildRenamePlan 从文件清单中提取真正发生变化的文件重命名。
func buildRenamePlan(files []IngestFile) []RenamePair {
	plan := make([]RenamePair, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.IsDir || file.FileID == "" || file.OriginalName == "" || file.NewName == "" || file.NewName == file.OriginalName {
			continue
		}
		if _, ok := seen[file.FileID]; ok {
			continue
		}
		seen[file.FileID] = struct{}{}
		plan = append(plan, RenamePair{FileID: file.FileID, OldName: file.OriginalName, NewName: file.NewName})
	}
	return plan
}

func normalizeFileIDs(fileIDs []string) []string {
	out := make([]string, 0, len(fileIDs))
	seen := make(map[string]struct{}, len(fileIDs))
	for _, id := range fileIDs {
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

func normalizeRenameReplace(rule RenameReplace) RenameReplace {
	rules := rule.NormalizeRules()
	out := RenameReplace{Enabled: rule.Enabled, Rules: rules}
	if len(rules) > 0 {
		out.Find = rules[0].Find
		out.ReplaceWith = rules[0].ReplaceWith
	}
	return out
}

func sanitizeRenameName(raw string) string {
	name := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(raw))
	name = strings.NewReplacer("/", "-", "\\", "-").Replace(name)
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if utf8.RuneCountInString(name) > maxRenameNameRunes {
		return ""
	}
	return name
}

func normalizeDestinationBasePath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/"
	}
	return path
}

func clampSeason(season int) int {
	if season < 0 {
		return 0
	}
	if season > 99 {
		return 99
	}
	return season
}

// normalizeTargetPath 清洗目标文件夹层级，避免路径分隔符和不可见字符进入驱动。
func normalizeTargetPath(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > maxTargetPathSegments {
		return nil, domain.Errorf(domain.CodeValidation, "目标文件夹层级过多")
	}
	segments := make([]string, 0, len(raw))
	for _, segment := range raw {
		name := sanitizeFolderName(segment)
		if name == "" {
			return nil, domain.Errorf(domain.CodeValidation, "目标文件夹名不能为空或包含非法字符")
		}
		segments = append(segments, name)
	}
	return segments, nil
}

func sanitizeFolderName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	name = strings.NewReplacer(
		"/", " ", "\\", " ", ":", " ", "*", " ", "?", " ",
		"\"", " ", "<", " ", ">", " ", "|", " ",
	).Replace(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	name = strings.Trim(name, ". ")
	if utf8.RuneCountInString(name) > maxFolderNameRunes {
		name = strings.Trim(string([]rune(name)[:maxFolderNameRunes]), ". ")
	}
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

func cleanDirs(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, dir := range raw {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ingestBaseDirs 优先使用明确传入的目录段；缺失时从展示路径兜底。
func ingestBaseDirs(in IngestInput) []string {
	dirs := cleanDirs(in.BaseDirs)
	if len(dirs) > 0 {
		return dirs
	}
	path := strings.Trim(strings.TrimSpace(in.BasePath), "/")
	if path == "" {
		return nil
	}
	return cleanDirs(strings.Split(path, "/"))
}

func normalizeParentID(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" || p == "0" || p == "/" || strings.EqualFold(p, "root") {
		return ""
	}
	return p
}
