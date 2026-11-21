package embyarchive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

// ArchiveRequest 描述一次项目归档。
type ArchiveRequest struct {
	ConfigID string
	// SourceLibraryID 是项目所在 Emby 媒体库；为空时只用于记录。
	SourceLibraryID string
	// ArchiveLibraryID 指定归档目标媒体库；为空时按媒体类型选择「归档 · 电影 / 归档 · 剧集」。
	ArchiveLibraryID string
	SourceTask       *domain.StrmTask
	Item             strmscrape.Item
	// SkipRefresh 由批量执行使用：多个项目连续归档时只在最后统一刷新索引与媒体库。
	SkipRefresh bool
}

// ArchiveResult 是一次归档的结果。
type ArchiveResult struct {
	ItemName           string
	SourceTaskID       int64
	SourceTaskName     string
	TargetTaskID       int64
	TargetTaskName     string
	SourceLibraryID    string
	SourceLibraryName  string
	ArchiveLibraryID   string
	ArchiveLibraryName string
	SourcePath         string
	TargetPath         string
}

// RestoreRequest 描述一次归档项目移回。
type RestoreRequest struct {
	ConfigID   string
	TargetTask *domain.StrmTask
	Item       strmscrape.Item
}

// RestoreResult 是一次移回的结果。
type RestoreResult struct {
	ItemName       string
	SourceTaskName string
	TargetTaskName string
	SourcePath     string
	TargetPath     string
}

type archiveRecord struct {
	Version            int    `json:"version"`
	ArchivedAt         string `json:"archived_at"`
	ItemName           string `json:"item_name"`
	SourceTaskID       int64  `json:"source_task_id"`
	SourceTaskName     string `json:"source_task_name"`
	SourceAccountID    int64  `json:"source_account_id"`
	SourceParentID     string `json:"source_parent_id"`
	SourceTaskPath     string `json:"source_task_path"`
	SourceSTRMOutput   string `json:"source_strm_output"`
	SourceItemRelDir   string `json:"source_item_rel_dir"`
	TargetTaskID       int64  `json:"target_task_id"`
	TargetTaskName     string `json:"target_task_name"`
	TargetParentID     string `json:"target_parent_id"`
	TargetTaskPath     string `json:"target_task_path"`
	TargetItemRelDir   string `json:"target_item_rel_dir"`
	ConfigID           string `json:"config_id"`
	SourceLibraryID    string `json:"source_library_id"`
	SourceLibraryName  string `json:"source_library_name"`
	ArchiveLibraryID   string `json:"archive_library_id"`
	ArchiveLibraryName string `json:"archive_library_name"`
}

type archivePlan struct {
	sourceLibrary  embyproxy.Library
	archiveLibrary embyproxy.Library
	targetTask     *domain.StrmTask
	targetRelative []string
}

// ArchiveItem 按「媒体卡片 -> 更多 -> 归档」的步骤把项目移入归档媒体库。
func (s *Service) ArchiveItem(ctx context.Context, req ArchiveRequest) (ArchiveResult, error) {
	plan, err := s.archivePlan(ctx, req)
	if err != nil {
		return ArchiveResult{}, err
	}
	return s.archiveWithPlan(ctx, plan, req)
}

func (s *Service) archivePlan(ctx context.Context, req ArchiveRequest) (archivePlan, error) {
	sourceTask := req.SourceTask
	if sourceTask == nil {
		return archivePlan{}, domain.Errorf(domain.CodeValidation, "源 STRM 任务不存在")
	}
	item := req.Item
	if item.MediaType != strmscrape.MediaTypeMovie && item.MediaType != strmscrape.MediaTypeTV {
		return archivePlan{}, domain.Errorf(domain.CodeValidation, "只能归档电影或剧集项目")
	}
	itemRel := SplitArchivePath(item.RelDir)
	if len(itemRel) == 0 {
		return archivePlan{}, domain.Errorf(domain.CodeValidation, "当前项目没有对应的媒体文件夹")
	}

	var sourceLibrary embyproxy.Library
	if strings.TrimSpace(req.SourceLibraryID) != "" {
		library, err := FindLibraryByID(ctx, s.emby, req.ConfigID, req.SourceLibraryID)
		if err != nil {
			return archivePlan{}, err
		}
		sourceLibrary = library
	}

	var archiveLibrary embyproxy.Library
	var err error
	if strings.TrimSpace(req.ArchiveLibraryID) != "" {
		archiveLibrary, err = FindLibraryByID(ctx, s.emby, req.ConfigID, req.ArchiveLibraryID)
	} else {
		libraryName := archiveLibraryMovieName
		if item.MediaType == strmscrape.MediaTypeTV {
			libraryName = archiveLibraryTVName
		}
		archiveLibrary, err = FindLibraryByName(ctx, s.emby, req.ConfigID, libraryName)
	}
	if err != nil {
		return archivePlan{}, err
	}

	tasks, err := s.strm.ListTasks(ctx)
	if err != nil {
		return archivePlan{}, err
	}
	targetTask, err := selectArchiveTask(tasks, archiveLibrary.Name, sourceTask)
	if err != nil {
		return archivePlan{}, err
	}
	return archivePlan{
		sourceLibrary:  sourceLibrary,
		archiveLibrary: archiveLibrary,
		targetTask:     targetTask,
		targetRelative: TargetRelativePath(sourceTask.Path, targetTask.Path, itemRel),
	}, nil
}

func selectArchiveTask(tasks []*domain.StrmTask, archiveLibraryName string, sourceTask *domain.StrmTask) (*domain.StrmTask, error) {
	if sourceTask == nil {
		return nil, domain.Errorf(domain.CodeValidation, "源 STRM 任务不存在")
	}
	candidates := MatchStrmTasks(tasks, archiveLibraryName)
	archiveTasks := make([]*domain.StrmTask, 0, len(candidates))
	for _, task := range candidates {
		if task != nil && task.ID != sourceTask.ID && task.AccountID == sourceTask.AccountID {
			archiveTasks = append(archiveTasks, task)
		}
	}
	if len(archiveTasks) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "没有找到同一网盘账号对应的归档 STRM 任务：%s", archiveLibraryName)
	}
	if len(archiveTasks) > 1 {
		names := make([]string, 0, len(archiveTasks))
		for _, task := range archiveTasks {
			names = append(names, task.Name)
		}
		return nil, domain.Errorf(domain.CodeValidation, "匹配到多个归档 STRM 任务，请保留一个：%s", strings.Join(names, "、"))
	}
	return archiveTasks[0], nil
}

func (s *Service) archiveWithPlan(ctx context.Context, plan archivePlan, req ArchiveRequest) (ArchiveResult, error) {
	sourceTask := req.SourceTask
	targetTask := plan.targetTask
	if sourceTask == nil || targetTask == nil {
		return ArchiveResult{}, domain.Errorf(domain.CodeValidation, "STRM 任务不存在")
	}
	item := req.Item
	itemRel := SplitArchivePath(item.RelDir)

	// 归档期间锁住源、目标任务，避免扫描或刮削同时修改目录和索引。
	release, err := s.lockTasks(sourceTask.ID, targetTask.ID)
	if err != nil {
		return ArchiveResult{}, err
	}
	defer release()

	sourceFolder, sourceParentID, err := s.resolveDir(ctx, sourceTask.AccountID, sourceTask.ParentID, itemRel)
	if err != nil {
		return ArchiveResult{}, err
	}
	if sourceFolder == nil || !sourceFolder.IsDir {
		return ArchiveResult{}, domain.Errorf(domain.CodeValidation, "源项目不是文件夹：%s", item.RelDir)
	}

	targetRelative := plan.targetRelative
	sourceSTRMPath, targetSTRMPath, err := s.prepareSTRMPaths(sourceTask, targetTask, itemRel, targetRelative, sourceFolder.Name)
	if err != nil {
		return ArchiveResult{}, err
	}
	targetParentID, err := s.ensureDir(ctx, sourceTask.AccountID, targetTask.ParentID, targetRelative)
	if err != nil {
		return ArchiveResult{}, err
	}
	targetChildren, err := s.files.List(ctx, sourceTask.AccountID, targetParentID, true)
	if err != nil {
		return ArchiveResult{}, err
	}
	for _, child := range targetChildren {
		if child.Name == sourceFolder.Name {
			return ArchiveResult{}, domain.Errorf(domain.CodeValidation, "归档目标已存在同名文件夹：%s", sourceFolder.Name)
		}
	}
	if err := s.files.MoveFiles(ctx, sourceTask.AccountID, []string{sourceFolder.ID}, targetParentID, sourceParentID); err != nil {
		return ArchiveResult{}, err
	}
	if err := moveFolder(sourceSTRMPath, targetSTRMPath); err != nil {
		return ArchiveResult{}, domain.Errorf(domain.CodeDriverError, "网盘文件已移动，但 STRM 文件夹移动失败：%v", err)
	}
	if err := writeArchiveRecord(targetSTRMPath, archiveRecord{
		Version:            1,
		ItemName:           sourceFolder.Name,
		SourceTaskID:       sourceTask.ID,
		SourceTaskName:     sourceTask.Name,
		SourceAccountID:    sourceTask.AccountID,
		SourceParentID:     sourceTask.ParentID,
		SourceTaskPath:     sourceTask.Path,
		SourceSTRMOutput:   strm.TaskRelDir(sourceTask.GroupDir, sourceTask.OutputFolder),
		SourceItemRelDir:   strings.Join(itemRel, "/"),
		TargetTaskID:       targetTask.ID,
		TargetTaskName:     targetTask.Name,
		TargetParentID:     targetTask.ParentID,
		TargetTaskPath:     targetTask.Path,
		TargetItemRelDir:   strings.Join(append(append([]string(nil), targetRelative...), sourceFolder.Name), "/"),
		ConfigID:           strings.TrimSpace(req.ConfigID),
		SourceLibraryID:    plan.sourceLibrary.ID,
		SourceLibraryName:  plan.sourceLibrary.Name,
		ArchiveLibraryID:   plan.archiveLibrary.ID,
		ArchiveLibraryName: plan.archiveLibrary.Name,
	}); err != nil {
		return ArchiveResult{}, domain.Errorf(domain.CodeDriverError, "文件已移动，但保存原始位置失败：%v", err)
	}
	if !req.SkipRefresh {
		if err := s.refreshAfterArchive(ctx, req.ConfigID, sourceTask.ID, targetTask.ID, plan.sourceLibrary.ID, plan.archiveLibrary.ID); err != nil {
			return ArchiveResult{}, err
		}
	}

	return ArchiveResult{
		ItemName:           sourceFolder.Name,
		SourceTaskID:       sourceTask.ID,
		SourceTaskName:     sourceTask.Name,
		TargetTaskID:       targetTask.ID,
		TargetTaskName:     targetTask.Name,
		SourceLibraryID:    plan.sourceLibrary.ID,
		SourceLibraryName:  plan.sourceLibrary.Name,
		ArchiveLibraryID:   plan.archiveLibrary.ID,
		ArchiveLibraryName: plan.archiveLibrary.Name,
		SourcePath:         JoinArchivePath(sourceTask.Path, item.RelDir),
		TargetPath:         JoinArchivePath(targetTask.Path, strings.Join(append(append([]string(nil), targetRelative...), sourceFolder.Name), "/")),
	}, nil
}

func (s *Service) refreshAfterArchive(ctx context.Context, configID string, sourceTaskID, targetTaskID int64, sourceLibraryID, archiveLibraryID string) error {
	// 源索引清除已移动项目，目标索引纳入新项目；目标刷新是归档流程的必要步骤。
	if err := s.refreshIndexes(ctx, sourceTaskID, targetTaskID); err != nil {
		return domain.Errorf(domain.CodeDriverError, "STRM 文件已移动，但刷新索引失败：%v", err)
	}
	if err := RefreshLibraries(ctx, s.emby, configID, sourceLibraryID, archiveLibraryID); err != nil {
		return domain.Errorf(domain.CodeDriverError, "文件已归档，但刷新 Emby 媒体库失败：%v", err)
	}
	return nil
}

func (s *Service) prepareSTRMPaths(sourceTask, targetTask *domain.StrmTask, sourceRel, targetRel []string, itemName string) (string, string, error) {
	if sourceTask == nil || targetTask == nil {
		return "", "", domain.Errorf(domain.CodeValidation, "STRM 任务不存在")
	}
	sourceRoot := strm.TaskOutputDir(s.strmDir, strm.TaskRelDir(sourceTask.GroupDir, sourceTask.OutputFolder))
	targetRoot := strm.TaskOutputDir(s.strmDir, strm.TaskRelDir(targetTask.GroupDir, targetTask.OutputFolder))
	if sourceRoot == "" || targetRoot == "" {
		return "", "", domain.Errorf(domain.CodeValidation, "STRM 输出目录无效")
	}
	sourcePath, err := safeJoin(sourceRoot, sourceRel)
	if err != nil {
		return "", "", err
	}
	targetParts := append(append([]string(nil), targetRel...), itemName)
	targetPath, err := safeJoin(targetRoot, targetParts)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", domain.Errorf(domain.CodeNotFound, "源 STRM 文件夹不存在：%s", filepath.ToSlash(sourcePath))
		}
		return "", "", domain.Wrap(domain.CodeDriverError, err)
	}
	if !info.IsDir() {
		return "", "", domain.Errorf(domain.CodeValidation, "源 STRM 项目不是文件夹：%s", filepath.ToSlash(sourcePath))
	}
	if _, err := os.Stat(targetPath); err == nil {
		return "", "", domain.Errorf(domain.CodeValidation, "目标 STRM 文件夹已存在：%s", filepath.ToSlash(targetPath))
	} else if !os.IsNotExist(err) {
		return "", "", domain.Wrap(domain.CodeDriverError, err)
	}
	return sourcePath, targetPath, nil
}

func moveFolder(sourcePath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	return os.Rename(sourcePath, targetPath)
}

func writeArchiveRecord(projectPath string, record archiveRecord) error {
	record.ArchivedAt = time.Now().UTC().Format(time.RFC3339)
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return writeFileAtomic(filepath.Join(projectPath, archiveRecordFileName), body)
}

func readArchiveRecord(projectPath string) (archiveRecord, error) {
	body, err := os.ReadFile(filepath.Join(projectPath, archiveRecordFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return archiveRecord{}, domain.Errorf(domain.CodeValidation, "未找到原始位置信息，请先完成归档同步")
		}
		return archiveRecord{}, err
	}
	var record archiveRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return archiveRecord{}, domain.Errorf(domain.CodeDriverError, "解析原始位置信息失败：%v", err)
	}
	if record.Version <= 0 || record.SourceTaskID <= 0 || record.TargetTaskID <= 0 || record.SourceItemRelDir == "" {
		return archiveRecord{}, domain.Errorf(domain.CodeValidation, "原始位置信息不完整")
	}
	return record, nil
}

// RestoreItem 按 buei_archive.json 把归档项目移回原位置。
func (s *Service) RestoreItem(ctx context.Context, req RestoreRequest) (RestoreResult, error) {
	targetTask := req.TargetTask
	if targetTask == nil {
		return RestoreResult{}, domain.Errorf(domain.CodeValidation, "归档 STRM 任务不存在")
	}
	item := req.Item
	if strings.TrimSpace(item.RelDir) == "" {
		return RestoreResult{}, domain.Errorf(domain.CodeNotFound, "归档 STRM 媒体项目不存在")
	}
	targetSTRMPath, err := safeJoin(
		strm.TaskOutputDir(s.strmDir, strm.TaskRelDir(targetTask.GroupDir, targetTask.OutputFolder)),
		SplitArchivePath(item.RelDir),
	)
	if err != nil {
		return RestoreResult{}, err
	}
	record, err := readArchiveRecord(targetSTRMPath)
	if err != nil {
		return RestoreResult{}, err
	}
	if record.TargetTaskID != targetTask.ID {
		return RestoreResult{}, domain.Errorf(domain.CodeValidation, "当前项目与原始归档任务不匹配")
	}
	configID := strings.TrimSpace(req.ConfigID)
	if configID == "" {
		configID = strings.TrimSpace(record.ConfigID)
	}
	sourceLibrary, err := FindLibraryByID(ctx, s.emby, configID, record.SourceLibraryID)
	if err != nil {
		return RestoreResult{}, err
	}
	archiveLibrary, err := FindLibraryByID(ctx, s.emby, configID, record.ArchiveLibraryID)
	if err != nil {
		return RestoreResult{}, err
	}
	sourceTask, err := s.strm.GetTask(ctx, record.SourceTaskID)
	if err != nil {
		return RestoreResult{}, err
	}
	if sourceTask == nil || sourceTask.AccountID != record.SourceAccountID {
		return RestoreResult{}, domain.Errorf(domain.CodeValidation, "原始 STRM 任务不存在或网盘账号已变化")
	}

	release, err := s.lockTasks(targetTask.ID, sourceTask.ID)
	if err != nil {
		return RestoreResult{}, err
	}
	defer release()

	currentFolder, currentParentID, err := s.resolveDir(ctx, sourceTask.AccountID, targetTask.ParentID, SplitArchivePath(item.RelDir))
	if err != nil {
		return RestoreResult{}, err
	}
	if currentFolder == nil || !currentFolder.IsDir || currentFolder.Name != record.ItemName {
		return RestoreResult{}, domain.Errorf(domain.CodeValidation, "当前归档目录与原始项目不匹配")
	}
	sourceRel := SplitArchivePath(record.SourceItemRelDir)
	sourceParentID, err := s.resolveParentDir(ctx, sourceTask.AccountID, record.SourceParentID, sourceRel)
	if err != nil {
		return RestoreResult{}, err
	}
	sourceOutput := record.SourceSTRMOutput
	if sourceOutput == "" {
		sourceOutput = strm.TaskRelDir(sourceTask.GroupDir, sourceTask.OutputFolder)
	}
	sourceSTRMPath, err := safeJoin(strm.TaskOutputDir(s.strmDir, sourceOutput), sourceRel)
	if err != nil {
		return RestoreResult{}, err
	}
	if _, err := os.Stat(sourceSTRMPath); err == nil {
		return RestoreResult{}, domain.Errorf(domain.CodeValidation, "原始 STRM 位置已有同名文件夹：%s", record.ItemName)
	} else if !os.IsNotExist(err) {
		return RestoreResult{}, domain.Wrap(domain.CodeDriverError, err)
	}
	if err := s.files.MoveFiles(ctx, sourceTask.AccountID, []string{currentFolder.ID}, sourceParentID, currentParentID); err != nil {
		return RestoreResult{}, err
	}
	if err := moveFolder(targetSTRMPath, sourceSTRMPath); err != nil {
		return RestoreResult{}, domain.Errorf(domain.CodeDriverError, "网盘文件已移回，但 STRM 文件夹移动失败：%v", err)
	}
	_ = os.Remove(filepath.Join(sourceSTRMPath, archiveRecordFileName))

	if err := s.refreshIndexes(ctx, targetTask.ID, sourceTask.ID); err != nil {
		return RestoreResult{}, domain.Errorf(domain.CodeDriverError, "项目已移回，但刷新 STRM 索引失败：%v", err)
	}
	if err := RefreshLibraries(ctx, s.emby, configID, sourceLibrary.ID, archiveLibrary.ID); err != nil {
		return RestoreResult{}, domain.Errorf(domain.CodeDriverError, "项目已移回，但刷新 Emby 媒体库失败：%v", err)
	}
	return RestoreResult{
		ItemName:       record.ItemName,
		SourceTaskName: sourceTask.Name,
		TargetTaskName: targetTask.Name,
		SourcePath:     JoinArchivePath(sourceTask.Path, record.SourceItemRelDir),
		TargetPath:     JoinArchivePath(targetTask.Path, item.RelDir),
	}, nil
}
