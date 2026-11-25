package embyarchive

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"litepan/internal/domain"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

// ProjectDeleteRequest 描述一次项目目录内的删除：
// 删除当前目录下选中的文件/文件夹，并连带删除同名的关联文件（nfo、图片、字幕等）
// 以及对侧（网盘 / 本地 STRM）的同名内容。
type ProjectDeleteRequest struct {
	ConfigID  string
	LibraryID string
	Task      *domain.StrmTask
	Item      strmscrape.Item
	// Path 是项目内的相对目录（从项目根目录开始）。
	Path []string
	// Names 是选中的条目名称（当前目录下）。
	Names []string
	// Side 表示勾选来自哪一侧："strm" 或 "cloud"（空值按 strm 处理）。
	// 非主文件只删除该侧勾选的文件，不影响另一侧。
	Side string
}

// ProjectDeleteResult 是一次目录内删除的结果。
type ProjectDeleteResult struct {
	DeletedNames []string
	CloudNames   []string
	StrmNames    []string
	IndexRefresh bool
	EmbyRefresh  bool
	// ProjectRemoved 表示项目里已没有 strm 文件，整个项目文件夹（网盘 + STRM）被一并删除。
	ProjectRemoved bool
	ProjectPath    string
}

// mediaImageSuffixes 是刮削写在媒体文件旁边、与主文件同名的图片后缀。
var mediaImageSuffixes = []string{
	"-thumb", "-poster", "-fanart", "-banner", "-logo", "-clearlogo", "-clearart",
	"-landscape", "-disc", "-discart", "-backdrop", "-keyart", "-cdart", "-trailer", "-theme",
}

// mediaLanguageCodes 是字幕常见语言标记，用于把「xxx.zh.srt」归到「xxx」。
var mediaLanguageCodes = map[string]bool{
	"zh": true, "chs": true, "cht": true, "zh-cn": true, "zh-tw": true, "zh-hk": true,
	"en": true, "eng": true, "ja": true, "jpn": true, "ko": true, "kor": true,
	"chi": true, "zho": true, "sc": true, "tc": true,
}

// MediaBaseKey 取媒体项的基础名：去掉扩展名、图片后缀与语言标记，
// 让 aaaa.strm / aaaa.nfo / aaaa-thumb.jpg / aaaa.zh.srt 归为同一组。
func MediaBaseKey(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	lower := strings.ToLower(base)
	for _, suffix := range mediaImageSuffixes {
		if strings.HasSuffix(lower, suffix) {
			base = base[:len(base)-len(suffix)]
			lower = strings.ToLower(base)
			break
		}
	}
	if index := strings.LastIndex(base, "."); index > 0 {
		if mediaLanguageCodes[strings.ToLower(base[index+1:])] {
			base = base[:index]
		}
	}
	return strings.ToLower(strings.TrimSpace(base))
}

// DeleteProjectEntries 删除项目目录内选中的条目及关联文件：
// 先删网盘侧（失败则不删本地），再删本地 STRM 侧，最后刷新任务索引与 Emby 媒体库。
func (s *Service) DeleteProjectEntries(ctx context.Context, req ProjectDeleteRequest) (ProjectDeleteResult, error) {
	task := req.Task
	if task == nil {
		return ProjectDeleteResult{}, domain.Errorf(domain.CodeValidation, "STRM 任务不存在")
	}
	names := make([]string, 0, len(req.Names))
	selected := make(map[string]bool, len(req.Names))
	for _, name := range req.Names {
		name = strings.TrimSpace(name)
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return ProjectDeleteResult{}, domain.Errorf(domain.CodeValidation, "删除目标名称无效：%s", name)
		}
		if selected[name] {
			continue
		}
		selected[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return ProjectDeleteResult{}, domain.Errorf(domain.CodeValidation, "请先选择要删除的文件")
	}

	release, err := s.lockTasks(task.ID, task.ID)
	if err != nil {
		return ProjectDeleteResult{}, err
	}
	defer release()

	relative := append(SplitArchivePath(req.Item.RelDir), req.Path...)
	strmRoot := strm.TaskOutputDir(s.strmDir, strm.TaskRelDir(task.GroupDir, task.OutputFolder))
	strmDir, err := safeJoin(strmRoot, relative)
	if err != nil {
		return ProjectDeleteResult{}, err
	}
	strmNames, err := readLocalDirNames(strmDir)
	if err != nil {
		return ProjectDeleteResult{}, err
	}
	cloudDirID, cloudItems, cloudErr := s.BrowseCloudDirWithID(ctx, task.AccountID, task.ParentID, relative, true)
	cloudNames := make([]string, 0, len(cloudItems))
	cloudIDs := make(map[string]string, len(cloudItems))
	for _, item := range cloudItems {
		cloudNames = append(cloudNames, item.Name)
		cloudIDs[item.Name] = item.ID
	}

	// 只有勾选视频正片或 .strm 时才连带删除同组文件，并且连带另一侧；
	// 勾选其它类型时只删除当前这一侧的文件，另一侧完全不动。
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if side != "cloud" {
		side = "strm"
	}
	var trigger func(string) bool
	primaryNames, otherNames := strmNames, cloudNames
	if side == "cloud" {
		trigger = IsVideoFileName
		primaryNames, otherNames = cloudNames, strmNames
	} else {
		trigger = IsSTRMFileName
	}
	keys := associatedKeys(selected, trigger)
	primaryTargets := selectProjectTargets(primaryNames, selected, keys)
	otherTargets := selectProjectTargets(otherNames, nil, keys)
	strmTargets, cloudTargets := primaryTargets, otherTargets
	if side == "cloud" {
		strmTargets, cloudTargets = otherTargets, primaryTargets
	}
	if len(strmTargets) == 0 && len(cloudTargets) == 0 {
		if cloudErr != nil {
			return ProjectDeleteResult{}, cloudErr
		}
		return ProjectDeleteResult{}, domain.Errorf(domain.CodeNotFound, "所选文件已不存在，请刷新后重试")
	}

	result := ProjectDeleteResult{DeletedNames: names, CloudNames: cloudTargets, StrmNames: strmTargets}
	if len(cloudTargets) > 0 {
		ids := make([]string, 0, len(cloudTargets))
		for _, name := range cloudTargets {
			if id := cloudIDs[name]; id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			if err := s.files.DeleteFiles(ctx, task.AccountID, ids, cloudDirID); err != nil {
				return ProjectDeleteResult{}, err
			}
		}
	}
	for _, name := range strmTargets {
		if err := os.RemoveAll(filepath.Join(strmDir, name)); err != nil {
			return ProjectDeleteResult{}, domain.Errorf(domain.CodeDriverError, "网盘文件已删除，但删除本地 STRM 文件失败：%v", err)
		}
	}

	// 项目里已经没有 strm 文件时，整个项目文件夹（网盘 + STRM）一起删除，避免留下空目录与残留文件。
	projectRel := SplitArchivePath(req.Item.RelDir)
	if len(projectRel) > 0 {
		projectSTRMDir, err := safeJoin(strmRoot, projectRel)
		if err != nil {
			return ProjectDeleteResult{}, err
		}
		if !hasSTRMFiles(projectSTRMDir) {
			if err := s.removeCloudProjectDir(ctx, task.AccountID, task.ParentID, projectRel); err != nil {
				return ProjectDeleteResult{}, err
			}
			if err := os.RemoveAll(projectSTRMDir); err != nil {
				return ProjectDeleteResult{}, domain.Errorf(domain.CodeDriverError, "网盘项目文件夹已删除，但删除本地 STRM 项目文件夹失败：%v", err)
			}
			result.ProjectRemoved = true
			result.ProjectPath = JoinArchivePath(task.Path, req.Item.RelDir)
		}
	}

	if err := s.refreshIndexes(ctx, task.ID); err != nil {
		return ProjectDeleteResult{}, domain.Errorf(domain.CodeDriverError, "文件已删除，但刷新 STRM 索引失败：%v", err)
	}
	result.IndexRefresh = true
	if err := RefreshLibraries(ctx, s.emby, strings.TrimSpace(req.ConfigID), strings.TrimSpace(req.LibraryID)); err != nil {
		return ProjectDeleteResult{}, domain.Errorf(domain.CodeDriverError, "文件已删除，但刷新 Emby 媒体库失败：%v", err)
	}
	result.EmbyRefresh = true
	return result, nil
}

// hasSTRMFiles 判断目录（含子目录）里是否还有 .strm 文件。
func hasSTRMFiles(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".strm") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// removeCloudProjectDir 删除网盘项目文件夹；目录已不存在时直接忽略。
func (s *Service) removeCloudProjectDir(ctx context.Context, accountID int64, rootID string, projectRel []string) error {
	folder, parentID, err := s.resolveDir(ctx, accountID, rootID, projectRel)
	if err != nil {
		if appErr, ok := domain.AsAppError(err); ok && appErr.Code == domain.CodeNotFound {
			return nil
		}
		return err
	}
	if folder == nil || !folder.IsDir {
		return nil
	}
	return s.files.DeleteFiles(ctx, accountID, []string{folder.ID}, parentID)
}

// videoExtensions 判断网盘里哪些文件是媒体正片，用于「多余 STRM」检测。
var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".ts": true, ".m2ts": true, ".iso": true,
	".avi": true, ".mov": true, ".wmv": true, ".flv": true, ".rmvb": true, ".rm": true,
	".webm": true, ".mpg": true, ".mpeg": true, ".vob": true, ".3gp": true,
}

// IsVideoFileName 判断文件名是否为视频正片。
func IsVideoFileName(name string) bool {
	return videoExtensions[strings.ToLower(filepath.Ext(strings.TrimSpace(name)))]
}

// ProjectSTRMDeleteRequest 删除项目目录里「网盘没有对应视频」的多余 STRM 文件。
type ProjectSTRMDeleteRequest struct {
	ConfigID  string
	LibraryID string
	Task      *domain.StrmTask
	Item      strmscrape.Item
	Path      []string
	Names     []string
}

// ProjectSTRMDeleteResult 是一次多余 STRM 清理的结果。
type ProjectSTRMDeleteResult struct {
	Deleted      []string
	IndexRefresh bool
	EmbyRefresh  bool
}

// DeleteProjectSTRMs 只删除本地多余的 .strm 文件，不动网盘文件与项目目录。
func (s *Service) DeleteProjectSTRMs(ctx context.Context, req ProjectSTRMDeleteRequest) (ProjectSTRMDeleteResult, error) {
	task := req.Task
	if task == nil {
		return ProjectSTRMDeleteResult{}, domain.Errorf(domain.CodeValidation, "STRM 任务不存在")
	}
	names := make([]string, 0, len(req.Names))
	seen := make(map[string]bool, len(req.Names))
	for _, name := range req.Names {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
			return ProjectSTRMDeleteResult{}, domain.Errorf(domain.CodeValidation, "删除目标名称无效：%s", name)
		}
		if !strings.EqualFold(filepath.Ext(name), ".strm") {
			return ProjectSTRMDeleteResult{}, domain.Errorf(domain.CodeValidation, "只能删除 .strm 文件：%s", name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return ProjectSTRMDeleteResult{}, domain.Errorf(domain.CodeValidation, "没有要删除的 STRM 文件")
	}

	release, err := s.lockTasks(task.ID, task.ID)
	if err != nil {
		return ProjectSTRMDeleteResult{}, err
	}
	defer release()

	strmDir, err := safeJoin(
		strm.TaskOutputDir(s.strmDir, strm.TaskRelDir(task.GroupDir, task.OutputFolder)),
		append(SplitArchivePath(req.Item.RelDir), req.Path...),
	)
	if err != nil {
		return ProjectSTRMDeleteResult{}, err
	}
	result := ProjectSTRMDeleteResult{Deleted: make([]string, 0, len(names))}
	for _, name := range names {
		err := os.Remove(filepath.Join(strmDir, name))
		if err != nil && !os.IsNotExist(err) {
			return ProjectSTRMDeleteResult{}, domain.Errorf(domain.CodeDriverError, "删除 STRM 文件失败：%v", err)
		}
		result.Deleted = append(result.Deleted, name)
	}
	if err := s.refreshIndexes(ctx, task.ID); err != nil {
		return ProjectSTRMDeleteResult{}, domain.Errorf(domain.CodeDriverError, "文件已删除，但刷新 STRM 索引失败：%v", err)
	}
	result.IndexRefresh = true
	if err := RefreshLibraries(ctx, s.emby, strings.TrimSpace(req.ConfigID), strings.TrimSpace(req.LibraryID)); err != nil {
		return ProjectSTRMDeleteResult{}, domain.Errorf(domain.CodeDriverError, "文件已删除，但刷新 Emby 媒体库失败：%v", err)
	}
	result.EmbyRefresh = true
	return result, nil
}

func readLocalDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, domain.Wrap(domain.CodeDriverError, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names, nil
}

// isAssociatedTrigger 判断勾选项是否会触发关联删除（视频正片或 .strm）。
func isAssociatedTrigger(name string) bool {
	return IsVideoFileName(name) || IsSTRMFileName(name)
}

// IsSTRMFileName 判断文件名是否为 .strm 文件。
func IsSTRMFileName(name string) bool {
	return strings.EqualFold(filepath.Ext(strings.TrimSpace(name)), ".strm")
}

// associatedKeys 收集会触发关联删除的基础名（视频正片或 .strm）。
func associatedKeys(selected map[string]bool, trigger func(string) bool) map[string]bool {
	keys := make(map[string]bool, len(selected))
	for name := range selected {
		if !trigger(name) {
			continue
		}
		if key := MediaBaseKey(name); key != "" {
			keys[key] = true
		}
	}
	return keys
}

// selectProjectTargets 从目录条目里挑出要删除的文件：
// 显式勾选的（selected 非空时）总是删除；基础名命中的按 keys 连带。
func selectProjectTargets(entries []string, selected map[string]bool, keys map[string]bool) []string {
	out := make([]string, 0, len(selected))
	for _, name := range entries {
		if len(selected) > 0 && selected[name] {
			out = append(out, name)
			continue
		}
		if len(keys) == 0 {
			continue
		}
		key := MediaBaseKey(name)
		if key != "" && keys[key] {
			out = append(out, name)
		}
	}
	return out
}
