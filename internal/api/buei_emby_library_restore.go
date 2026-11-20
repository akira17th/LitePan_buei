package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

const bueiArchiveRecordFileName = "buei_archive.json"

type bueiArchiveRecord struct {
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

type bueiEmbyLibraryRestoreRequest struct {
	ItemID   string `json:"item_id"`
	ConfigID string `json:"config_id"`
}

type bueiEmbyLibraryRestoreResult struct {
	Moved          bool   `json:"moved"`
	StrmMoved      bool   `json:"strm_moved"`
	EmbyRefreshed  bool   `json:"emby_refreshed"`
	ItemName       string `json:"item_name"`
	SourceTaskName string `json:"source_task_name"`
	TargetTaskName string `json:"target_task_name"`
	SourcePath     string `json:"source_path"`
	TargetPath     string `json:"target_path"`
}

func bueiWriteArchiveRecord(projectPath string, record bueiArchiveRecord) error {
	record.ArchivedAt = time.Now().UTC().Format(time.RFC3339)
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	path := filepath.Join(projectPath, bueiArchiveRecordFileName)
	tmp := path + ".tmp-" + fmt.Sprint(time.Now().UnixNano())
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func bueiReadArchiveRecord(projectPath string) (bueiArchiveRecord, error) {
	body, err := os.ReadFile(filepath.Join(projectPath, bueiArchiveRecordFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return bueiArchiveRecord{}, domain.Errorf(domain.CodeValidation, "未找到原始位置信息，请先完成归档同步")
		}
		return bueiArchiveRecord{}, err
	}
	var record bueiArchiveRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return bueiArchiveRecord{}, fmt.Errorf("解析原始位置信息失败：%w", err)
	}
	if record.Version <= 0 || record.SourceTaskID <= 0 || record.TargetTaskID <= 0 || record.SourceItemRelDir == "" {
		return bueiArchiveRecord{}, domain.Errorf(domain.CodeValidation, "原始位置信息不完整")
	}
	return record, nil
}

func bueiFindEmbyLibraryByID(ctx context.Context, svc *embyproxy.Service, configID, id string) (embyproxy.Library, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return embyproxy.Library{}, domain.Errorf(domain.CodeValidation, "缺少原 Emby 媒体库 ID")
	}
	libraries, err := svc.ListLibraries(ctx, strings.TrimSpace(configID))
	if err != nil {
		return embyproxy.Library{}, err
	}
	for _, library := range libraries {
		if library.ID == id {
			return library, nil
		}
	}
	return embyproxy.Library{}, domain.Errorf(domain.CodeValidation, "Emby 媒体库不存在：%s", id)
}

func bueiRefreshArchiveLibraries(ctx context.Context, svc *embyproxy.Service, configID string, sourceLibraryID, archiveLibraryID string) error {
	seen := make(map[string]struct{}, 2)
	for _, libraryID := range []string{sourceLibraryID, archiveLibraryID} {
		libraryID = strings.TrimSpace(libraryID)
		if libraryID == "" {
			continue
		}
		if _, ok := seen[libraryID]; ok {
			continue
		}
		seen[libraryID] = struct{}{}
		if _, err := svc.RefreshLibrary(ctx, embyproxy.RefreshRequest{ConfigID: strings.TrimSpace(configID), Mode: "library", LibraryID: libraryID}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) bueiEmbyLibraryRestore(w http.ResponseWriter, r *http.Request) {
	if h.files == nil || h.strm == nil || h.strmScrape == nil || h.embyProxy == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req bueiEmbyLibraryRestoreRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	targetTaskID, itemID, err := bueiParseStrmItemKey(req.ItemID)
	if err != nil {
		writeErr(w, err)
		return
	}
	targetTask, err := h.strm.GetTask(r.Context(), targetTaskID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if targetTask == nil {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "归档 STRM 任务不存在"))
		return
	}
	items, err := bueiListAllStrmItems(r.Context(), h.strmScrape, targetTask.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	var currentItem *strmscrape.Item
	for i := range items {
		if items[i].ID == itemID {
			currentItem = &items[i]
			break
		}
	}
	if currentItem == nil || currentItem.RelDir == "" {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "归档 STRM 媒体项目不存在"))
		return
	}

	targetSTRMPath, err := bueiArchiveSafeJoin(strm.TaskOutputDir(h.strmDir, strm.TaskRelDir(targetTask.GroupDir, targetTask.OutputFolder)), bueiSplitArchivePath(currentItem.RelDir))
	if err != nil {
		writeErr(w, err)
		return
	}
	record, err := bueiReadArchiveRecord(targetSTRMPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	if record.TargetTaskID != targetTask.ID {
		writeErr(w, domain.Errorf(domain.CodeValidation, "当前项目与原始归档任务不匹配"))
		return
	}
	configID := strings.TrimSpace(req.ConfigID)
	if configID == "" {
		configID = strings.TrimSpace(record.ConfigID)
	}
	sourceLibrary, err := bueiFindEmbyLibraryByID(r.Context(), h.embyProxy, configID, record.SourceLibraryID)
	if err != nil {
		writeErr(w, err)
		return
	}
	archiveLibrary, err := bueiFindEmbyLibraryByID(r.Context(), h.embyProxy, configID, record.ArchiveLibraryID)
	if err != nil {
		writeErr(w, err)
		return
	}

	sourceTask, err := h.strm.GetTask(r.Context(), record.SourceTaskID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if sourceTask == nil || sourceTask.AccountID != record.SourceAccountID {
		writeErr(w, domain.Errorf(domain.CodeValidation, "原始 STRM 任务不存在或网盘账号已变化"))
		return
	}
	release, err := h.bueiLockArchiveTasks(targetTask.ID, sourceTask.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer release()

	currentFolder, currentParentID, err := h.bueiResolveArchiveDir(r.Context(), sourceTask.AccountID, targetTask.ParentID, bueiSplitArchivePath(currentItem.RelDir))
	if err != nil {
		writeErr(w, err)
		return
	}
	if !currentFolder.IsDir || currentFolder.Name != record.ItemName {
		writeErr(w, domain.Errorf(domain.CodeValidation, "当前归档目录与原始项目不匹配"))
		return
	}
	sourceRel := bueiSplitArchivePath(record.SourceItemRelDir)
	sourceParentID, err := h.bueiResolveArchiveParentDir(r.Context(), sourceTask.AccountID, record.SourceParentID, sourceRel)
	if err != nil {
		writeErr(w, err)
		return
	}
	sourceOutput := record.SourceSTRMOutput
	if sourceOutput == "" {
		sourceOutput = strm.TaskRelDir(sourceTask.GroupDir, sourceTask.OutputFolder)
	}
	sourceSTRMPath, err := bueiArchiveSafeJoin(strm.TaskOutputDir(h.strmDir, sourceOutput), sourceRel)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := os.Stat(sourceSTRMPath); err == nil {
		writeErr(w, domain.Errorf(domain.CodeValidation, "原始 STRM 位置已有同名文件夹：%s", record.ItemName))
		return
	} else if !os.IsNotExist(err) {
		writeErr(w, domain.Wrap(domain.CodeDriverError, err))
		return
	}
	if err := h.files.MoveFiles(r.Context(), sourceTask.AccountID, []string{currentFolder.ID}, sourceParentID, currentParentID); err != nil {
		writeErr(w, err)
		return
	}
	if err := bueiMoveArchiveSTRMFolder(targetSTRMPath, sourceSTRMPath); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "网盘文件已移回，但 STRM 文件夹移动失败：%v", err))
		return
	}
	_ = os.Remove(filepath.Join(sourceSTRMPath, bueiArchiveRecordFileName))

	if _, err := h.strmScrape.RefreshIndex(r.Context(), targetTask.ID, strmscrape.ItemListQuery{Offset: 0, Limit: 200, Sort: strmscrape.ItemListSortTitleAsc}); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "项目已移回，但刷新归档任务索引失败：%v", err))
		return
	}
	if _, err := h.strmScrape.RefreshIndex(r.Context(), sourceTask.ID, strmscrape.ItemListQuery{Offset: 0, Limit: 200, Sort: strmscrape.ItemListSortTitleAsc}); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "项目已移回，但刷新源任务索引失败：%v", err))
		return
	}
	if err := bueiRefreshArchiveLibraries(r.Context(), h.embyProxy, configID, sourceLibrary.ID, archiveLibrary.ID); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "项目已移回，但刷新 Emby 媒体库失败：%v", err))
		return
	}
	writeOK(w, bueiEmbyLibraryRestoreResult{
		Moved:          true,
		StrmMoved:      true,
		EmbyRefreshed:  true,
		ItemName:       record.ItemName,
		SourceTaskName: sourceTask.Name,
		TargetTaskName: targetTask.Name,
		SourcePath:     bueiJoinArchivePath(sourceTask.Path, record.SourceItemRelDir),
		TargetPath:     bueiJoinArchivePath(targetTask.Path, currentItem.RelDir),
	})
}

func (h *Handler) bueiResolveArchiveParentDir(ctx context.Context, accountID int64, rootID string, relative []string) (string, error) {
	parentID := strings.TrimSpace(rootID)
	if len(relative) <= 1 {
		return parentID, nil
	}
	for _, name := range relative[:len(relative)-1] {
		items, err := h.files.List(ctx, accountID, parentID, true)
		if err != nil {
			return "", err
		}
		var matched *domain.FileItem
		for i := range items {
			if items[i].Name != name {
				continue
			}
			if matched != nil {
				return "", domain.Errorf(domain.CodeValidation, "原始路径存在同名目录：%s", name)
			}
			item := items[i]
			matched = &item
		}
		if matched == nil || !matched.IsDir {
			return "", domain.Errorf(domain.CodeNotFound, "原始父目录不存在：%s", name)
		}
		parentID = matched.ID
	}
	return parentID, nil
}
