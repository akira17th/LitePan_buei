package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

type bueiEmbyLibraryArchiveRequest struct {
	ItemID    string `json:"item_id"`
	ConfigID  string `json:"config_id"`
	LibraryID string `json:"library_id"`
}

type bueiEmbyLibraryArchiveResult struct {
	Moved           bool   `json:"moved"`
	StrmMoved       bool   `json:"strm_moved"`
	EmbyRefreshed   bool   `json:"emby_refreshed"`
	EmbyLibraryName string `json:"emby_library_name,omitempty"`
	ItemName        string `json:"item_name"`
	SourceTaskID    int64  `json:"source_task_id"`
	SourceTaskName  string `json:"source_task_name"`
	TargetTaskID    int64  `json:"target_task_id"`
	TargetTaskName  string `json:"target_task_name"`
	SourcePath      string `json:"source_path"`
	TargetPath      string `json:"target_path"`
}

func (h *Handler) bueiEmbyLibraryArchive(w http.ResponseWriter, r *http.Request) {
	if h.files == nil || h.strm == nil || h.strmScrape == nil || h.embyProxy == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req bueiEmbyLibraryArchiveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	sourceTaskID, itemID, err := bueiParseStrmItemKey(req.ItemID)
	if err != nil {
		writeErr(w, err)
		return
	}
	sourceTask, err := h.strm.GetTask(r.Context(), sourceTaskID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if sourceTask == nil {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "源 STRM 任务不存在"))
		return
	}

	items, err := bueiListAllStrmItems(r.Context(), h.strmScrape, sourceTask.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	var sourceItem *strmscrape.Item
	for i := range items {
		if items[i].ID == itemID {
			sourceItem = &items[i]
			break
		}
	}
	if sourceItem == nil {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "STRM 媒体项目不存在"))
		return
	}
	if sourceItem.MediaType != strmscrape.MediaTypeMovie && sourceItem.MediaType != strmscrape.MediaTypeTV {
		writeErr(w, domain.Errorf(domain.CodeValidation, "只能归档电影或剧集项目"))
		return
	}
	itemRel := bueiSplitArchivePath(sourceItem.RelDir)
	if len(itemRel) == 0 {
		writeErr(w, domain.Errorf(domain.CodeValidation, "当前项目没有对应的媒体文件夹"))
		return
	}

	tasks, err := h.strm.ListTasks(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	archiveLibraryName := "归档 · 电影"
	if sourceItem.MediaType == strmscrape.MediaTypeTV {
		archiveLibraryName = "归档 · 剧集"
	}
	sourceLibrary, err := bueiFindEmbyLibraryByID(r.Context(), h.embyProxy, req.ConfigID, req.LibraryID)
	if err != nil {
		writeErr(w, err)
		return
	}
	archiveLibrary, err := bueiFindEmbyLibrary(r.Context(), h.embyProxy, req.ConfigID, archiveLibraryName)
	if err != nil {
		writeErr(w, err)
		return
	}
	candidates := bueiMatchStrmTasks(tasks, archiveLibraryName)
	archiveTasks := make([]*domain.StrmTask, 0, len(candidates))
	for _, task := range candidates {
		if task != nil && task.ID != sourceTask.ID && task.AccountID == sourceTask.AccountID {
			archiveTasks = append(archiveTasks, task)
		}
	}
	if len(archiveTasks) == 0 {
		writeErr(w, domain.Errorf(domain.CodeValidation, "没有找到同一网盘账号对应的归档 STRM 任务：%s", archiveLibraryName))
		return
	}
	if len(archiveTasks) > 1 {
		names := make([]string, 0, len(archiveTasks))
		for _, task := range archiveTasks {
			names = append(names, task.Name)
		}
		writeErr(w, domain.Errorf(domain.CodeValidation, "匹配到多个归档 STRM 任务，请保留一个：%s", strings.Join(names, "、")))
		return
	}
	targetTask := archiveTasks[0]

	// 归档期间锁住源、目标任务，避免扫描或刮削同时修改目录和索引。
	release, err := h.bueiLockArchiveTasks(sourceTask.ID, targetTask.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer release()

	sourceFolder, sourceParentID, err := h.bueiResolveArchiveDir(r.Context(), sourceTask.AccountID, sourceTask.ParentID, itemRel)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !sourceFolder.IsDir {
		writeErr(w, domain.Errorf(domain.CodeValidation, "源项目不是文件夹：%s", sourceItem.RelDir))
		return
	}

	targetRelative := bueiArchiveTargetRelativePath(sourceTask.Path, targetTask.Path, itemRel)
	sourceSTRMPath, targetSTRMPath, err := bueiPrepareArchiveSTRMPaths(h.strmDir, sourceTask, targetTask, itemRel, targetRelative, sourceFolder.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	targetParentID, err := h.bueiEnsureArchiveDir(r.Context(), sourceTask.AccountID, targetTask.ParentID, targetRelative)
	if err != nil {
		writeErr(w, err)
		return
	}
	targetChildren, err := h.files.List(r.Context(), sourceTask.AccountID, targetParentID, true)
	if err != nil {
		writeErr(w, err)
		return
	}
	for _, child := range targetChildren {
		if child.Name == sourceFolder.Name {
			writeErr(w, domain.Errorf(domain.CodeValidation, "归档目标已存在同名文件夹：%s", sourceFolder.Name))
			return
		}
	}
	if err := h.files.MoveFiles(r.Context(), sourceTask.AccountID, []string{sourceFolder.ID}, targetParentID, sourceParentID); err != nil {
		writeErr(w, err)
		return
	}
	if err := bueiMoveArchiveSTRMFolder(sourceSTRMPath, targetSTRMPath); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "网盘文件已移动，但 STRM 文件夹移动失败：%v", err))
		return
	}
	if err := bueiWriteArchiveRecord(targetSTRMPath, bueiArchiveRecord{
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
		SourceLibraryID:    sourceLibrary.ID,
		SourceLibraryName:  sourceLibrary.Name,
		ArchiveLibraryID:   archiveLibrary.ID,
		ArchiveLibraryName: archiveLibrary.Name,
	}); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "文件已移动，但保存原始位置失败：%v", err))
		return
	}
	// 源索引清除已移动项目，目标索引纳入新项目；目标刷新是归档流程的必要步骤。
	if _, err := h.strmScrape.RefreshIndex(r.Context(), sourceTask.ID, strmscrape.ItemListQuery{Offset: 0, Limit: 200, Sort: strmscrape.ItemListSortTitleAsc}); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "STRM 文件已移动，但刷新源任务索引失败：%v", err))
		return
	}
	if _, err := h.strmScrape.RefreshIndex(r.Context(), targetTask.ID, strmscrape.ItemListQuery{Offset: 0, Limit: 200, Sort: strmscrape.ItemListSortTitleAsc}); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "STRM 文件已移动，但刷新归档任务索引失败：%v", err))
		return
	}
	if err := bueiRefreshArchiveLibraries(r.Context(), h.embyProxy, strings.TrimSpace(req.ConfigID), sourceLibrary.ID, archiveLibrary.ID); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "文件已归档，但刷新 Emby 媒体库失败：%v", err))
		return
	}

	writeOK(w, bueiEmbyLibraryArchiveResult{
		Moved:           true,
		StrmMoved:       true,
		EmbyRefreshed:   true,
		EmbyLibraryName: archiveLibrary.Name,
		ItemName:        sourceFolder.Name,
		SourceTaskID:    sourceTask.ID,
		SourceTaskName:  sourceTask.Name,
		TargetTaskID:    targetTask.ID,
		TargetTaskName:  targetTask.Name,
		SourcePath:      bueiJoinArchivePath(sourceTask.Path, sourceItem.RelDir),
		TargetPath:      bueiJoinArchivePath(targetTask.Path, strings.Join(append(append([]string(nil), targetRelative...), sourceFolder.Name), "/")),
	})
}

func bueiFindEmbyLibrary(ctx context.Context, svc *embyproxy.Service, configID, wanted string) (embyproxy.Library, error) {
	libraries, err := svc.ListLibraries(ctx, strings.TrimSpace(configID))
	if err != nil {
		return embyproxy.Library{}, err
	}
	compact := func(value string) string {
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' || r == '·' || r == '•' || r == '-' || r == '_' {
				return -1
			}
			return r
		}, strings.TrimSpace(value))
	}
	for _, library := range libraries {
		if strings.TrimSpace(library.Name) == strings.TrimSpace(wanted) || compact(library.Name) == compact(wanted) {
			return library, nil
		}
	}
	return embyproxy.Library{}, domain.Errorf(domain.CodeValidation, "未找到 Emby 媒体库：%s", wanted)
}

func bueiPrepareArchiveSTRMPaths(strmDir string, sourceTask, targetTask *domain.StrmTask, sourceRel, targetRel []string, itemName string) (string, string, error) {
	if sourceTask == nil || targetTask == nil {
		return "", "", domain.Errorf(domain.CodeValidation, "STRM 任务不存在")
	}
	sourceRoot := strm.TaskOutputDir(strmDir, strm.TaskRelDir(sourceTask.GroupDir, sourceTask.OutputFolder))
	targetRoot := strm.TaskOutputDir(strmDir, strm.TaskRelDir(targetTask.GroupDir, targetTask.OutputFolder))
	if sourceRoot == "" || targetRoot == "" {
		return "", "", domain.Errorf(domain.CodeValidation, "STRM 输出目录无效")
	}
	sourcePath, err := bueiArchiveSafeJoin(sourceRoot, sourceRel)
	if err != nil {
		return "", "", err
	}
	targetParts := append(append([]string(nil), targetRel...), itemName)
	targetPath, err := bueiArchiveSafeJoin(targetRoot, targetParts)
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

func bueiArchiveSafeJoin(root string, parts []string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", domain.Wrap(domain.CodeInternal, err)
	}
	cleanParts := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.ReplaceAll(part, "\\", "/"))
		if part == "" || part == "." || part == ".." || strings.Contains(part, "/") {
			return "", domain.Errorf(domain.CodeValidation, "STRM 相对路径无效")
		}
		cleanParts = append(cleanParts, part)
	}
	joined := filepath.Join(append([]string{rootAbs}, cleanParts...)...)
	joinedAbs, err := filepath.Abs(joined)
	if err != nil {
		return "", domain.Wrap(domain.CodeInternal, err)
	}
	rel, err := filepath.Rel(rootAbs, joinedAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", domain.Errorf(domain.CodeValidation, "STRM 目标路径越界")
	}
	return joinedAbs, nil
}

func bueiMoveArchiveSTRMFolder(sourcePath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	return os.Rename(sourcePath, targetPath)
}

func bueiParseStrmItemKey(raw string) (int64, string, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return 0, "", domain.Errorf(domain.CodeValidation, "STRM 媒体项目 ID 无效")
	}
	taskID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || taskID <= 0 {
		return 0, "", domain.Errorf(domain.CodeValidation, "STRM 任务 ID 无效")
	}
	return taskID, strings.TrimSpace(parts[1]), nil
}

func bueiSplitArchivePath(raw string) []string {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		out = append(out, part)
	}
	return out
}

func bueiJoinArchivePath(root, relative string) string {
	parts := append(bueiSplitArchivePath(root), bueiSplitArchivePath(relative)...)
	if len(parts) == 0 {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}

func bueiArchiveTargetRelativePath(sourcePath, targetPath string, itemRel []string) []string {
	sourceParts := bueiSplitArchivePath(sourcePath)
	targetParts := bueiSplitArchivePath(targetPath)
	relativeRoot := make([]string, 0)
	// 固定去除源目录的顶级目录，例如 /云影视/电视剧 -> 电视剧。
	if len(sourceParts) > 1 {
		relativeRoot = append(relativeRoot, sourceParts[1:]...)
	}
	// 电影源路径为 /云影视/电影，目标根也是 /归档影视/电影，避免重复的“电影”目录。
	if len(relativeRoot) > 0 && len(targetParts) > 0 && strings.EqualFold(relativeRoot[0], targetParts[len(targetParts)-1]) {
		relativeRoot = relativeRoot[1:]
	}
	if len(itemRel) > 1 {
		relativeRoot = append(relativeRoot, itemRel[:len(itemRel)-1]...)
	}
	return relativeRoot
}

func (h *Handler) bueiResolveArchiveDir(ctx context.Context, accountID int64, rootID string, relative []string) (*domain.FileItem, string, error) {
	parentID := strings.TrimSpace(rootID)
	var found *domain.FileItem
	for index, name := range relative {
		items, err := h.files.List(ctx, accountID, parentID, true)
		if err != nil {
			return nil, "", err
		}
		var matched *domain.FileItem
		for i := range items {
			if items[i].Name != name {
				continue
			}
			if matched != nil {
				return nil, "", domain.Errorf(domain.CodeValidation, "源路径存在同名项目：%s", name)
			}
			item := items[i]
			matched = &item
		}
		if matched == nil {
			return nil, "", domain.Errorf(domain.CodeNotFound, "源路径不存在：%s", name)
		}
		found = matched
		if index < len(relative)-1 {
			if !matched.IsDir {
				return nil, "", domain.Errorf(domain.CodeValidation, "源路径不是目录：%s", name)
			}
			parentID = matched.ID
		}
	}
	return found, parentID, nil
}

func (h *Handler) bueiEnsureArchiveDir(ctx context.Context, accountID int64, rootID string, relative []string) (string, error) {
	parentID := strings.TrimSpace(rootID)
	for _, name := range relative {
		items, err := h.files.List(ctx, accountID, parentID, true)
		if err != nil {
			return "", err
		}
		var matched *domain.FileItem
		for i := range items {
			if items[i].Name == name {
				if matched != nil {
					return "", domain.Errorf(domain.CodeValidation, "目标路径存在同名项目：%s", name)
				}
				item := items[i]
				matched = &item
			}
		}
		if matched == nil {
			created, err := h.files.CreateFolder(ctx, accountID, parentID, name)
			if err != nil {
				return "", err
			}
			matched = created
		}
		if !matched.IsDir {
			return "", domain.Errorf(domain.CodeValidation, "目标路径中的项目不是目录：%s", name)
		}
		parentID = matched.ID
	}
	return parentID, nil
}

func (h *Handler) bueiLockArchiveTasks(sourceID, targetID int64) (func(), error) {
	ids := []int64{sourceID}
	if targetID != sourceID {
		ids = append(ids, targetID)
	}
	if len(ids) == 2 && ids[0] > ids[1] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	releases := make([]func(), 0, len(ids))
	for _, id := range ids {
		release, ok := h.strm.TryBeginTaskFileOperation(id)
		if !ok {
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
			return nil, domain.Errorf(domain.CodeValidation, "相关 STRM 任务正在运行，请稍后再归档")
		}
		releases = append(releases, release)
	}
	return func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}, nil
}
