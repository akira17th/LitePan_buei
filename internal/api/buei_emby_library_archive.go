package api

import (
	"net/http"
	"strconv"
	"strings"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// buei:emby-library —— 归档/移回的执行逻辑在 internal/buei/embyarchive，
// 这里只负责解析请求、定位 STRM 项目并转换响应。

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

func (h *Handler) bueiEmbyLibraryArchive(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil || h.strm == nil || h.strmScrape == nil {
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

	result, err := h.buei.EmbyArchive.ArchiveItem(r.Context(), embyarchive.ArchiveRequest{
		ConfigID:        strings.TrimSpace(req.ConfigID),
		SourceLibraryID: strings.TrimSpace(req.LibraryID),
		SourceTask:      sourceTask,
		Item:            *sourceItem,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiEmbyLibraryArchiveResult{
		Moved:           true,
		StrmMoved:       true,
		EmbyRefreshed:   true,
		EmbyLibraryName: result.ArchiveLibraryName,
		ItemName:        result.ItemName,
		SourceTaskID:    result.SourceTaskID,
		SourceTaskName:  result.SourceTaskName,
		TargetTaskID:    result.TargetTaskID,
		TargetTaskName:  result.TargetTaskName,
		SourcePath:      result.SourcePath,
		TargetPath:      result.TargetPath,
	})
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
