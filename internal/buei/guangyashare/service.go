package guangyashare

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"litepan/internal/core/driverexec"
	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/pkg/safego"
)

const (
	shareTokenCacheTTL = 30 * time.Minute
	// 转存任务完成后，目标目录里的文件可能略有延迟，先短暂等待再重命名。
	renameWaitTimeout  = 20 * time.Second
	renameWaitInterval = time.Second
	// 转存等待超时后的后台补重命名：最多继续等待 15 分钟。
	renameLaterTimeout  = 15 * time.Minute
	renameLaterInterval = 5 * time.Second
	maxRenamePairs      = 2000
	// 转存成功后生成 STRM 的超时；失败只记录日志，不影响转存结果。
	strmGenerateTimeout = 60 * time.Second
	// lastBaseConfigKey 保存在通用 configs 表中，避免浏览器清理丢失上次转存位置。
	lastBaseConfigKey = "buei_guangya_share_last_base"
)

// Options 构造光鸭分享收藏服务。
type Options struct {
	Exec      *driverexec.Executor
	Accounts  domain.AccountRepository
	Links     domain.GuangyaShareLinkRepository
	Transfers domain.GuangyaShareTransferredRepository
	Configs   domain.ConfigRepository
	Log       *slog.Logger
	// InvalidateDir 在重命名后失效目标目录缓存；为空时跳过。
	InvalidateDir func(accountID int64, parentID string)
	// GenerateStrm 在指定目录内生成/更新 STRM；由装配层注入，为空时跳过。
	// dirs 是目标目录相对账号根的目录段数组。
	GenerateStrm func(ctx context.Context, accountID int64, parentID string, dirs []string) (generated int64, matched bool, err error)
}

type cachedShareToken struct {
	token   string
	expires time.Time
}

// Service 负责分享解析与转存编排；公开解析不依赖任何账号凭证。
type Service struct {
	exec      *driverexec.Executor
	accounts  domain.AccountRepository
	links     domain.GuangyaShareLinkRepository
	transfers domain.GuangyaShareTransferredRepository
	configs   domain.ConfigRepository
	log       *slog.Logger
	// invalidateDir 由装配层注入，避免服务直接依赖缓存实现。
	invalidateDir func(accountID int64, parentID string)
	// strmGen 同理由装配层注入，避免服务直接依赖 STRM 包。
	strmGen func(ctx context.Context, accountID int64, parentID string, dirs []string) (int64, bool, error)
	client  *shareClient
	// progress 记录进行中的转存阶段，供前端轮询。
	progress *transferProgressTracker

	tokenMu sync.Mutex
	tokens  map[string]cachedShareToken
}

// ShareItem 是分享内的一个文件或目录。
type ShareItem struct {
	ID       string    `json:"id"`
	ParentID string    `json:"parent_id"`
	Name     string    `json:"name"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	FileType int       `json:"file_type"`
	Kind     string    `json:"kind"`
	Path     string    `json:"path"`
	Depth    int       `json:"depth"`
}

// ParseInput 是解析分享链接的入参。
type ParseInput struct {
	URL      string
	Password string
}

// ParseResult 是分享解析结果，只包含分享根目录内容，子目录按需通过 List 加载。
type ParseResult struct {
	ShareID          string                                `json:"share_id"`
	Title            string                                `json:"title"`
	Owner            string                                `json:"owner"`
	NeedCode         bool                                  `json:"need_code"`
	NeedPassword     bool                                  `json:"need_password"`
	Items            []ShareItem                           `json:"items"`
	LastDestination  *domain.GuangyaShareDestination       `json:"last_destination,omitempty"`
	LastBase         *domain.GuangyaShareDestinationBase   `json:"last_base,omitempty"`
	TransferredFiles []*domain.GuangyaShareTransferredFile `json:"transferred_files,omitempty"`
	LastBrowsePath   []domain.GuangyaShareBrowseCrumb      `json:"last_browse_path,omitempty"`
	RenameReplace    *domain.GuangyaShareRenameReplace     `json:"rename_replace,omitempty"`
}

// ListInput 是列出分享内某个目录的入参。
type ListInput struct {
	URL        string
	ShareID    string
	Password   string
	ParentID   string
	ParentPath string
}

// ListResult 是目录列表结果。
type ListResult struct {
	Items []ShareItem `json:"items"`
}

// TransferInput 是转存入参。
type TransferInput struct {
	URL       string
	ShareID   string
	Password  string
	FileIDs   []string
	AccountID int64
	ParentID  string
	// TargetPath 是相对 ParentID 的目标文件夹层级，例如 ["剧名 (2026)", "Season 01"]。
	// 只在真正转存时检查并创建，解析阶段不会访问目标账号目录。
	TargetPath []string
	BasePath   string
	BaseDirs   []string
	Title      string
	Season     int
	IsSeries   bool
	// Files 是本次转存的文件清单，用于重命名和转存历史持久化。
	Files []TransferFile
	// BrowsePath 是点击转存时浏览到的分享目录，用于下次打开时恢复位置。
	BrowsePath []domain.GuangyaShareBrowseCrumb
	// RenameReplace 是本次使用的替换文本规则，成功后保存供下次直接应用。
	RenameReplace *domain.GuangyaShareRenameReplace
	// AutoStrm 表示转存成功后自动在目标文件夹生成 STRM。
	AutoStrm bool
	// ProgressID 是前端生成的进度标识，用于轮询「转存 → 重命名 → 生成 STRM」阶段。
	ProgressID string
}

// TransferFile 是本次转存的一个文件及其新旧名称。
type TransferFile struct {
	FileID       string
	OriginalName string
	NewName      string
	IsDir        bool
}

// RenamePair 描述转存落地后的一次重命名。
type RenamePair struct {
	FileName string
	NewName  string
}

// DiscardInput 是手动丢弃文件的入参。
type DiscardInput struct {
	ShareID string
	Files   []TransferFile
}

// TransferResult 是转存结果。
type TransferResult struct {
	TaskID          string `json:"task_id"`
	Completed       bool   `json:"completed"`
	Renamed         int    `json:"renamed"`
	RenameSkipped   int    `json:"rename_skipped"`
	RenameScheduled bool   `json:"rename_scheduled"`
	// StrmGenerated 是自动生成 STRM 时新建/更新的文件数；0 表示没有待生成内容。
	StrmGenerated int64 `json:"strm_generated"`
	// StrmMatched 表示目标目录命中了 STRM 任务；未命中时不会生成任何文件。
	StrmMatched bool `json:"strm_matched"`
	// StrmScheduled 表示转存任务等待超时，STRM 生成已转为后台跟进。
	StrmScheduled bool `json:"strm_scheduled"`
	// StrmError 是自动生成 STRM 失败的原因（转存本身仍然成功）。
	StrmError string `json:"strm_error,omitempty"`
}

// New 创建服务。
func New(opts Options) *Service {
	logger := opts.Log
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		exec:          opts.Exec,
		accounts:      opts.Accounts,
		links:         opts.Links,
		transfers:     opts.Transfers,
		configs:       opts.Configs,
		log:           logger,
		invalidateDir: opts.InvalidateDir,
		strmGen:       opts.GenerateStrm,
		client:        newShareClient(),
		progress:      newTransferProgressTracker(),
		tokens:        make(map[string]cachedShareToken),
	}
}

// TransferProgress 返回一次转存的阶段进度，供前端轮询展示。
func (s *Service) TransferProgress(id string) (TransferProgress, bool) {
	if s == nil {
		return TransferProgress{}, false
	}
	return s.progress.get(id)
}

// SetStrmGenerator 注入转存后自动生成 STRM 的适配器；装配期调用一次。
func (s *Service) SetStrmGenerator(gen func(ctx context.Context, accountID int64, parentID string, dirs []string) (int64, bool, error)) {
	if s == nil {
		return
	}
	s.strmGen = gen
}

// Parse 解析分享链接并列出分享根目录内容。
func (s *Service) Parse(ctx context.Context, in ParseInput) (*ParseResult, error) {
	if s == nil || s.client == nil {
		return nil, domain.Errorf(domain.CodeInternal, "光鸭分享服务未初始化")
	}
	link, err := ParseShareLink(in.URL)
	if err != nil {
		return nil, err
	}
	password := strings.TrimSpace(in.Password)
	if password == "" {
		password = link.Password
	}

	summary, err := s.client.shareSummary(ctx, link.ShareID)
	if err != nil {
		return nil, err
	}
	result := &ParseResult{
		ShareID:  link.ShareID,
		Title:    strings.TrimSpace(summary.Title),
		Owner:    strings.TrimSpace(summary.NickName),
		NeedCode: summary.NeedCode,
	}
	if summary.NeedCode && password == "" {
		result.NeedPassword = true
		return result, nil
	}
	if !summary.NeedCode {
		// 无提取码的分享不携带口令，避免用户误填的口令触发 209。
		password = ""
	}

	items, err := s.listItems(ctx, link.ShareID, password, "", "")
	if err != nil {
		return nil, err
	}
	result.Items = items
	if saved := s.saveLink(ctx, link, password, summary, in.URL); saved != nil {
		result.LastDestination = saved.LastDestination
		result.LastBrowsePath = saved.LastBrowsePath
		if saved.RenameReplace.Enabled || saved.RenameReplace.Find != "" || saved.RenameReplace.ReplaceWith != "" {
			rule := saved.RenameReplace
			result.RenameReplace = &rule
		}
	}
	result.LastBase = s.loadLastBase(ctx)
	result.TransferredFiles = s.loadTransferred(ctx, link.ShareID)
	return result, nil
}

// SavedLinks 返回已收藏的分享链接，按最近使用时间倒序。
func (s *Service) SavedLinks(ctx context.Context) ([]*domain.GuangyaShareLink, error) {
	if s == nil || s.links == nil {
		return []*domain.GuangyaShareLink{}, nil
	}
	return s.links.List(ctx)
}

// DeleteSavedLink 删除一条收藏记录；不影响网盘文件与分享本身。
// Discard 把选中的分享文件标记为已丢弃，后续展示与已转存文件一致。
func (s *Service) Discard(ctx context.Context, in DiscardInput) (int, error) {
	if s == nil || s.transfers == nil {
		return 0, domain.Errorf(domain.CodeNotImplement, "转存历史未启用")
	}
	shareID := strings.TrimSpace(in.ShareID)
	if shareID == "" {
		return 0, domain.Errorf(domain.CodeValidation, "分享 ID 不能为空")
	}
	files := normalizeTransferFiles(in.Files)
	if len(files) == 0 {
		return 0, domain.Errorf(domain.CodeValidation, "请先勾选要丢弃的文件")
	}
	now := time.Now().UTC()
	records := make([]*domain.GuangyaShareTransferredFile, 0, len(files))
	for _, file := range files {
		if file.IsDir {
			continue
		}
		records = append(records, &domain.GuangyaShareTransferredFile{
			FileID:       file.FileID,
			OriginalName: file.OriginalName,
			NewName:      file.OriginalName,
			Status:       domain.GuangyaShareFileDiscarded,
			UpdatedAt:    now,
		})
	}
	if len(records) == 0 {
		return 0, domain.Errorf(domain.CodeValidation, "请先勾选要丢弃的文件")
	}
	if err := s.transfers.UpsertMany(ctx, shareID, records); err != nil {
		return 0, err
	}
	return len(records), nil
}

func (s *Service) DeleteSavedLink(ctx context.Context, id int64) error {
	if s == nil || s.links == nil {
		return domain.Errorf(domain.CodeNotImplement, "收藏功能未启用")
	}
	shareID, err := s.links.Delete(ctx, id)
	if err != nil {
		return err
	}
	// 外键级联之外再显式清理一次，避免旧库未启用 foreign_keys 时残留历史。
	if s.transfers != nil {
		if err := s.transfers.DeleteByShare(ctx, shareID); err != nil {
			s.logger().Warn("清理光鸭分享转存历史失败", "share_id", shareID, "error", err)
		}
	}
	return nil
}

// List 列出分享内指定目录的直接内容。
func (s *Service) List(ctx context.Context, in ListInput) (*ListResult, error) {
	if s == nil || s.client == nil {
		return nil, domain.Errorf(domain.CodeInternal, "光鸭分享服务未初始化")
	}
	shareID, password, err := resolveShareInput(in.URL, in.ShareID, in.Password)
	if err != nil {
		return nil, err
	}
	items, err := s.listItems(ctx, shareID, password, strings.TrimSpace(in.ParentID), normalizeParentPath(in.ParentPath))
	if err != nil {
		return nil, err
	}
	return &ListResult{Items: items}, nil
}

// Transfer 把分享内选中的文件转存到指定光鸭账号目录。
func (s *Service) Transfer(ctx context.Context, in TransferInput) (out *TransferResult, err error) {
	if s == nil {
		return nil, domain.Errorf(domain.CodeInternal, "光鸭分享服务未初始化")
	}
	shareID, password, err := resolveShareInput(in.URL, in.ShareID, in.Password)
	if err != nil {
		return nil, err
	}
	fileIDs := normalizeFileIDs(in.FileIDs)
	if len(fileIDs) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请选择要转存的文件")
	}
	targetPath, err := normalizeTargetPath(in.TargetPath)
	if err != nil {
		return nil, err
	}
	if in.AccountID <= 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请选择转存账号")
	}
	if s.exec == nil || s.accounts == nil {
		return nil, domain.Errorf(domain.CodeInternal, "转存服务未初始化")
	}

	account, err := s.accounts.Get(ctx, in.AccountID)
	if err != nil {
		return nil, err
	}
	if account.DriverType != "guangya" {
		return nil, domain.Errorf(domain.CodeValidation, "只能转存到光鸭云盘账号")
	}
	if !account.IsActive {
		return nil, domain.Errorf(domain.CodeValidation, "目标账号已停用")
	}

	progressID := strings.TrimSpace(in.ProgressID)
	// 进度跟踪在真正开始转存时登记；前面的校验失败不需要进度。
	s.progress.start(progressID)
	defer func() {
		if err != nil {
			s.progress.finish(progressID, err.Error())
			return
		}
		s.progress.finish(progressID, "")
	}()

	summary, err := s.client.shareSummary(ctx, shareID)
	if err != nil {
		return nil, err
	}
	if !summary.NeedCode {
		// 分享已移除提取码时忽略前端残留的口令。
		password = ""
	}
	accessToken, err := s.accessToken(ctx, shareID, password, false)
	if err != nil {
		return nil, err
	}

	var (
		taskID         string
		completed      bool
		baseParentID   = strings.TrimSpace(in.ParentID)
		targetParentID = baseParentID
	)
	err = s.exec.Run(ctx, in.AccountID, func(drv driver.Driver) error {
		restorer, ok := drv.(driver.GuangyaShareRestorer)
		if !ok {
			return domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持光鸭分享转存")
		}
		// 每次尝试都从基础目录重新解析，重试时不会把已创建的层级再套一层。
		parentID := baseParentID
		if len(targetPath) > 0 {
			var folderErr error
			parentID, folderErr = ensureTargetFolder(ctx, drv, baseParentID, targetPath)
			if folderErr != nil {
				return folderErr
			}
		}
		targetParentID = parentID
		var runErr error
		taskID, completed, runErr = restorer.RestoreShare(ctx, driver.GuangyaShareRestoreRequest{
			AccessToken: accessToken,
			FileIDs:     fileIDs,
			ParentID:    parentID,
		})
		return runErr
	})
	if err != nil {
		return nil, err
	}

	files := normalizeTransferFiles(in.Files)
	renamePlan := buildRenamePlan(files)
	result := &TransferResult{TaskID: taskID, Completed: completed}
	strmDirs := targetDirectorySegments(in.BaseDirs, targetPath)
	if completed {
		if len(renamePlan) > 0 {
			s.progress.setPhase(progressID, GuangyaSharePhaseRename)
			result.Renamed, result.RenameSkipped = s.renameAfterRestore(ctx, in.AccountID, targetParentID, renamePlan)
		}
		s.saveTransferred(ctx, shareID, in.AccountID, targetParentID, files)
		if in.AutoStrm {
			s.progress.setPhase(progressID, GuangyaSharePhaseStrm)
			result.StrmGenerated, result.StrmMatched, result.StrmError = s.generateStrm(ctx, in.AccountID, targetParentID, strmDirs)
		}
	} else if len(files) > 0 || in.AutoStrm {
		// 任务等待超时：文件可能仍在落地，重命名与 STRM 生成一起转为后台跟进。
		s.scheduleAfterRestore(shareID, in.AccountID, targetParentID, renamePlan, files, strmDirs, in.AutoStrm)
		result.RenameScheduled = true
		result.StrmScheduled = in.AutoStrm
	}

	// 任务提交成功即记录目标预设；completed=false 只表示等待超时，任务通常仍在执行。
	storedTitle := sanitizeFolderName(in.Title)
	if len(targetPath) > 0 {
		storedTitle = targetPath[0]
	}
	s.saveBrowsePath(ctx, shareID, in.BrowsePath)
	s.saveRenameReplace(ctx, shareID, in.RenameReplace)
	s.saveDestination(ctx, shareID, &domain.GuangyaShareDestination{
		AccountID: in.AccountID,
		ParentID:  strings.TrimSpace(in.ParentID),
		BasePath:  normalizeDestinationBasePath(in.BasePath),
		BaseDirs:  append([]string(nil), in.BaseDirs...),
		Title:     storedTitle,
		Season:    clampShareSeason(in.Season),
		IsSeries:  in.IsSeries,
		AutoStrm:  in.AutoStrm,
	})

	if !completed {
		s.log.Info("光鸭分享转存任务等待超时，任务可能仍在执行", "account_id", in.AccountID, "task_id", taskID, "count", len(fileIDs))
	}
	if result.StrmGenerated > 0 {
		s.log.Info("光鸭分享转存后自动生成 STRM", "account_id", in.AccountID, "parent_id", targetParentID, "dirs", strmDirs, "count", result.StrmGenerated)
	}
	return result, nil
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

// generateStrm 在目标目录生成 STRM；失败只记录日志并回传给前端提示，不影响转存结果。
func (s *Service) generateStrm(ctx context.Context, accountID int64, parentID string, dirs []string) (int64, bool, string) {
	if s == nil || s.strmGen == nil {
		return 0, false, ""
	}
	runCtx, cancel := context.WithTimeout(ctx, strmGenerateTimeout)
	defer cancel()
	generated, matched, err := s.strmGen(runCtx, accountID, parentID, dirs)
	if err != nil {
		s.logger().Warn("光鸭分享转存后自动生成 STRM 失败", "account_id", accountID, "parent_id", parentID, "dirs", dirs, "error", err)
		return 0, false, err.Error()
	}
	if !matched {
		s.logger().Info("光鸭分享转存目标目录未匹配 STRM 任务，跳过自动生成", "account_id", accountID, "dirs", dirs)
	}
	return generated, matched, ""
}

// normalizeTransferFiles 清洗本次转存的文件清单，去重并限制条目数。
func normalizeTransferFiles(raw []TransferFile) []TransferFile {
	if len(raw) == 0 {
		return nil
	}
	files := make([]TransferFile, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, file := range raw {
		fileID := strings.TrimSpace(file.FileID)
		original := strings.TrimSpace(file.OriginalName)
		if fileID == "" || original == "" {
			continue
		}
		if _, ok := seen[fileID]; ok {
			continue
		}
		seen[fileID] = struct{}{}
		newName := sanitizeRenameName(file.NewName)
		if file.IsDir || newName == "" {
			newName = original
		}
		files = append(files, TransferFile{
			FileID:       fileID,
			OriginalName: original,
			NewName:      newName,
			IsDir:        file.IsDir,
		})
		if len(files) >= maxRenamePairs {
			break
		}
	}
	return files
}

// buildRenamePlan 从文件清单中提取真正发生变化的文件重命名。
func buildRenamePlan(files []TransferFile) []RenamePair {
	plan := make([]RenamePair, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.IsDir || file.NewName == "" || file.NewName == file.OriginalName {
			continue
		}
		if _, ok := seen[file.OriginalName]; ok {
			continue
		}
		seen[file.OriginalName] = struct{}{}
		plan = append(plan, RenamePair{FileName: file.OriginalName, NewName: file.NewName})
	}
	if len(plan) == 0 {
		return nil
	}
	return plan
}

func (s *Service) loadTransferred(ctx context.Context, shareID string) []*domain.GuangyaShareTransferredFile {
	if s == nil || s.transfers == nil || strings.TrimSpace(shareID) == "" {
		return nil
	}
	files, err := s.transfers.ListByShare(ctx, shareID)
	if err != nil {
		s.logger().Warn("读取光鸭分享转存历史失败", "share_id", shareID, "error", err)
		return nil
	}
	return files
}

func (s *Service) saveTransferred(ctx context.Context, shareID string, accountID int64, targetParentID string, files []TransferFile) {
	if s == nil || s.transfers == nil || len(files) == 0 {
		return
	}
	now := time.Now().UTC()
	records := make([]*domain.GuangyaShareTransferredFile, 0, len(files))
	for _, file := range files {
		if file.IsDir {
			continue
		}
		records = append(records, &domain.GuangyaShareTransferredFile{
			FileID:         file.FileID,
			OriginalName:   file.OriginalName,
			NewName:        file.NewName,
			AccountID:      accountID,
			TargetParentID: targetParentID,
			Status:         domain.GuangyaShareFileTransferred,
			UpdatedAt:      now,
		})
	}
	if len(records) == 0 {
		return
	}
	if err := s.transfers.UpsertMany(ctx, shareID, records); err != nil {
		s.logger().Warn("保存光鸭分享转存历史失败", "share_id", shareID, "count", len(records), "error", err)
	}
}

func (s *Service) logger() *slog.Logger {
	if s != nil && s.log != nil {
		return s.log
	}
	return slog.Default()
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
	if utf8.RuneCountInString(name) > 200 {
		return ""
	}
	return name
}

// renameAfterRestore 等待目标文件出现后执行重命名，返回成功与跳过数量。
func (s *Service) renameAfterRestore(ctx context.Context, accountID int64, parentID string, plan []RenamePair) (int, int) {
	if len(plan) == 0 {
		return 0, 0
	}
	deadline := time.Now().Add(renameWaitTimeout)
	for {
		present, err := s.targetEntryNames(ctx, accountID, parentID)
		if err == nil && allNamesPresent(present, plan) {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		sleep := time.NewTimer(renameWaitInterval)
		select {
		case <-ctx.Done():
			sleep.Stop()
		case <-sleep.C:
		}
		if ctx.Err() != nil {
			break
		}
	}
	renamed, skipped := s.applyRenames(ctx, accountID, parentID, plan)
	if s.invalidateDir != nil {
		s.invalidateDir(accountID, parentID)
	}
	if skipped > 0 {
		s.log.Warn("光鸭分享转存后有文件未能重命名", "account_id", accountID, "parent_id", parentID, "renamed", renamed, "skipped", skipped)
	}
	return renamed, skipped
}

// scheduleAfterRestore 在转存任务等待超时后继续跟进，完成后补做重命名与 STRM 生成。
func (s *Service) scheduleAfterRestore(shareID string, accountID int64, parentID string, plan []RenamePair, files []TransferFile, strmDirs []string, autoStrm bool) {
	log := s.logger()
	// 后台等待的名单包含文件夹：只转存文件夹时也要等它们出现，避免过早生成 STRM。
	waitPlan := plan
	if len(files) > 0 {
		waitPlan = make([]RenamePair, 0, len(files))
		for _, file := range files {
			waitPlan = append(waitPlan, RenamePair{FileName: file.OriginalName, NewName: file.NewName})
		}
	}
	go func() {
		safego.Guard(log, "buei.guangyashare.rename", func() {
			ctx, cancel := context.WithTimeout(context.Background(), renameLaterTimeout)
			defer cancel()
			deadline := time.Now().Add(renameLaterTimeout)
			for {
				present, err := s.targetEntryNames(ctx, accountID, parentID)
				if err == nil && allNamesPresent(present, waitPlan) {
					break
				}
				if time.Now().After(deadline) || ctx.Err() != nil {
					break
				}
				timer := time.NewTimer(renameLaterInterval)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			renamed, skipped := 0, 0
			if len(plan) > 0 {
				renamed, skipped = s.applyRenames(ctx, accountID, parentID, plan)
				if s.invalidateDir != nil {
					s.invalidateDir(accountID, parentID)
				}
			}
			s.saveTransferred(ctx, shareID, accountID, parentID, files)
			// 后台补生成 STRM：使用独立超时，避免和重命名等待共享同一个 deadline。
			if autoStrm {
				// 顶层条目出现后再留一点缓冲，让其余内容落盘。
				settle := time.NewTimer(renameLaterInterval)
				select {
				case <-ctx.Done():
					settle.Stop()
				case <-settle.C:
				}
				strmCtx, strmCancel := context.WithTimeout(context.Background(), strmGenerateTimeout)
				generated, _, strmErr := s.generateStrm(strmCtx, accountID, parentID, strmDirs)
				strmCancel()
				if strmErr == "" {
					log.Info("光鸭分享转存后台补生成 STRM 结束", "account_id", accountID, "parent_id", parentID, "dirs", strmDirs, "count", generated)
				}
			}
			log.Info("光鸭分享转存后台补重命名结束", "account_id", accountID, "parent_id", parentID, "renamed", renamed, "skipped", skipped)
		})
	}()
}

func allNamesPresent(present map[string]int, plan []RenamePair) bool {
	need := make(map[string]int, len(plan))
	for _, pair := range plan {
		need[pair.FileName]++
	}
	for name, count := range need {
		if present[name] < count {
			return false
		}
	}
	return true
}

func (s *Service) targetEntryNames(ctx context.Context, accountID int64, parentID string) (map[string]int, error) {
	counts := map[string]int{}
	err := s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		items, listErr := drv.ListFiles(ctx, parentID)
		if listErr != nil {
			return listErr
		}
		for _, item := range items {
			counts[item.Name]++
		}
		return nil
	})
	return counts, err
}

// applyRenames 在目标目录里按原文件名匹配并重命名；同一名称出现多次时只处理前若干个。
func (s *Service) applyRenames(ctx context.Context, accountID int64, parentID string, plan []RenamePair) (int, int) {
	if len(plan) == 0 {
		return 0, 0
	}
	renamed, skipped := 0, 0
	err := s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		renamer, ok := drv.(driver.Renamer)
		if !ok {
			skipped = len(plan)
			return nil
		}
		items, listErr := drv.ListFiles(ctx, parentID)
		if listErr != nil {
			return listErr
		}
		pending := make(map[string][]string, len(items))
		for _, item := range items {
			if item.IsDir {
				continue
			}
			pending[item.Name] = append(pending[item.Name], item.ID)
		}
		for _, pair := range plan {
			ids := pending[pair.FileName]
			if len(ids) == 0 {
				skipped++
				continue
			}
			fileID := ids[0]
			pending[pair.FileName] = ids[1:]
			if renameErr := renamer.RenameFile(ctx, fileID, pair.NewName); renameErr != nil {
				s.log.Warn("光鸭分享转存后重命名失败", "account_id", accountID, "parent_id", parentID, "name", pair.FileName, "new_name", pair.NewName, "error", renameErr)
				skipped++
				continue
			}
			renamed++
		}
		return nil
	})
	if err != nil {
		s.log.Warn("光鸭分享转存后重命名失败", "account_id", accountID, "parent_id", parentID, "error", err)
		return renamed, len(plan) - renamed
	}
	return renamed, skipped
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

func clampShareSeason(season int) int {
	if season < 0 {
		return 0
	}
	if season > 99 {
		return 99
	}
	return season
}

const (
	maxTargetPathSegments = 8
	maxFolderNameRunes    = 100
)

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

// ensureTargetFolder 在转存账号内按层级查找目录，不存在时创建。只在用户点击转存后调用。
func ensureTargetFolder(ctx context.Context, drv driver.Driver, parentID string, segments []string) (string, error) {
	for _, name := range segments {
		items, err := drv.ListFiles(ctx, parentID)
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
		creator, ok := drv.(driver.FolderCreator)
		if !ok {
			return "", domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持创建文件夹")
		}
		created, err := creator.CreateFolder(ctx, parentID, name)
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

func (s *Service) saveLink(ctx context.Context, link LinkInfo, password string, summary *shareSummaryData, rawURL string) *domain.GuangyaShareLink {
	if s.links == nil {
		return nil
	}
	now := time.Now().UTC()
	saved, err := s.links.Upsert(ctx, &domain.GuangyaShareLink{
		ShareID:    link.ShareID,
		URL:        strings.TrimSpace(rawURL),
		Password:   password,
		Title:      strings.TrimSpace(summary.Title),
		Owner:      strings.TrimSpace(summary.NickName),
		CreatedAt:  now,
		UpdatedAt:  now,
		LastUsedAt: now,
	})
	if err != nil {
		s.log.Warn("保存光鸭分享收藏失败", "share_id", link.ShareID, "error", err)
		return nil
	}
	return saved
}

// loadLastBase 读取上次成功转存使用的基础目录；数据库不可用时不影响解析。
func (s *Service) loadLastBase(ctx context.Context) *domain.GuangyaShareDestinationBase {
	if s == nil || s.configs == nil {
		return nil
	}
	raw, ok, err := s.configs.Get(ctx, lastBaseConfigKey)
	if err != nil {
		s.log.Warn("读取光鸭分享上次转存目录失败", "error", err)
		return nil
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	var base domain.GuangyaShareDestinationBase
	if err := json.Unmarshal([]byte(raw), &base); err != nil {
		s.log.Warn("解析光鸭分享上次转存目录失败", "error", err)
		return nil
	}
	if base.AccountID <= 0 {
		return nil
	}
	if strings.TrimSpace(base.Path) == "" {
		base.Path = "/"
	}
	return &base
}

// saveDestination 把分享的目标预设和最近基础目录写入数据库。
func (s *Service) saveRenameReplace(ctx context.Context, shareID string, rule *domain.GuangyaShareRenameReplace) {
	if s == nil || s.links == nil || rule == nil {
		return
	}
	if err := s.links.UpdateRenameReplace(ctx, shareID, *rule); err != nil {
		s.logger().Warn("保存光鸭分享替换规则失败", "share_id", shareID, "error", err)
	}
}

func (s *Service) saveBrowsePath(ctx context.Context, shareID string, crumbs []domain.GuangyaShareBrowseCrumb) {
	if s == nil || s.links == nil || len(crumbs) == 0 {
		return
	}
	if err := s.links.UpdateBrowsePath(ctx, shareID, crumbs); err != nil {
		s.logger().Warn("保存光鸭分享浏览目录失败", "share_id", shareID, "error", err)
	}
}

func (s *Service) saveDestination(ctx context.Context, shareID string, dest *domain.GuangyaShareDestination) {
	if dest == nil {
		return
	}
	if s.links != nil {
		if err := s.links.UpdateDestination(ctx, shareID, dest); err != nil {
			s.log.Warn("保存光鸭分享转存位置失败", "share_id", shareID, "error", err)
		}
	}
	if s.configs == nil {
		return
	}
	data, err := json.Marshal(domain.GuangyaShareDestinationBase{
		AccountID: dest.AccountID,
		ParentID:  dest.ParentID,
		Path:      dest.BasePath,
		Dirs:      append([]string(nil), dest.BaseDirs...),
	})
	if err != nil {
		s.log.Warn("序列化光鸭分享转存目录失败", "error", err)
		return
	}
	if err := s.configs.Set(ctx, lastBaseConfigKey, string(data)); err != nil {
		s.log.Warn("保存光鸭分享上次转存目录失败", "error", err)
	}
}

func (s *Service) listItems(ctx context.Context, shareID, password, parentID, parentPath string) ([]ShareItem, error) {
	token, err := s.accessToken(ctx, shareID, password, false)
	if err != nil {
		return nil, err
	}
	entries, err := s.client.listShareFiles(ctx, token, parentID)
	if err != nil {
		if !isShareAccessTokenExpired(err) {
			return nil, err
		}
		token, err = s.accessToken(ctx, shareID, password, true)
		if err != nil {
			return nil, err
		}
		entries, err = s.client.listShareFiles(ctx, token, parentID)
		if err != nil {
			return nil, err
		}
	}

	depth := strings.Count(parentPath, "/")
	items := make([]ShareItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, toShareItem(entry, parentID, parentPath, depth))
	}
	return items, nil
}

func (s *Service) accessToken(ctx context.Context, shareID, password string, force bool) (string, error) {
	key := shareTokenCacheKey(shareID, password)
	if !force {
		if token, ok := s.cachedToken(key); ok {
			return token, nil
		}
	}
	token, err := s.client.shareAccessToken(ctx, shareID, password)
	if err != nil {
		return "", err
	}
	s.storeToken(key, token)
	return token, nil
}

func (s *Service) cachedToken(key string) (string, bool) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	entry, ok := s.tokens[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expires) {
		delete(s.tokens, key)
		return "", false
	}
	return entry.token, true
}

func (s *Service) storeToken(key, token string) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.tokens == nil {
		s.tokens = make(map[string]cachedShareToken)
	}
	if len(s.tokens) >= 128 {
		now := time.Now()
		for k, entry := range s.tokens {
			if now.After(entry.expires) {
				delete(s.tokens, k)
			}
		}
	}
	s.tokens[key] = cachedShareToken{token: token, expires: time.Now().Add(shareTokenCacheTTL)}
}

func shareTokenCacheKey(shareID, password string) string {
	sum := sha256.Sum256([]byte(shareID + "\x00" + password))
	return fmt.Sprintf("%x", sum)
}

func isShareAccessTokenExpired(err error) bool {
	return err != nil && strings.Contains(err.Error(), "访问令牌")
}

func resolveShareInput(rawURL, shareID, password string) (string, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	shareID = strings.TrimSpace(shareID)
	password = strings.TrimSpace(password)
	if rawURL != "" {
		link, err := ParseShareLink(rawURL)
		if err != nil {
			return "", "", err
		}
		if shareID == "" {
			shareID = link.ShareID
		}
		if password == "" {
			password = link.Password
		}
	}
	if shareID == "" {
		return "", "", domain.Errorf(domain.CodeValidation, "分享 ID 为空，请重新解析分享链接")
	}
	return shareID, password, nil
}

func normalizeParentPath(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	return "/" + path
}

func toShareItem(entry shareEntry, parentID, parentPath string, depth int) ShareItem {
	name := strings.TrimSpace(entry.FileName)
	if name == "" {
		name = entry.FileID
	}
	itemPath := "/" + name
	if parentPath != "" {
		itemPath = strings.TrimRight(parentPath, "/") + "/" + name
	}
	modTime := time.Time{}
	if entry.UTime > 0 {
		modTime = time.Unix(entry.UTime, 0)
	} else if entry.CTime > 0 {
		modTime = time.Unix(entry.CTime, 0)
	}
	isDir := entry.ResType == 2
	return ShareItem{
		ID: entry.FileID,
		// 分享列表接口不保证返回 parentId，列表请求的父目录才是可靠来源。
		ParentID: parentID,
		Name:     name,
		IsDir:    isDir,
		Size:     entry.FileSize,
		ModTime:  modTime,
		FileType: entry.FileType,
		Kind:     classifyKind(name, entry.FileType, isDir),
		Path:     itemPath,
		Depth:    depth,
	}
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
