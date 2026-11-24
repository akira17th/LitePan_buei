package api

import (
	"net/http"
	"strings"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// buei:emby-library —— 删除媒体项目：删网盘文件夹 + STRM 文件夹，
// 再刷新对应 STRM 任务索引并触发 Emby 刷库。

type bueiEmbyLibraryDeleteRequest struct {
	ItemID    string `json:"item_id"`
	ConfigID  string `json:"config_id"`
	LibraryID string `json:"library_id"`
}

type bueiEmbyLibraryDeleteResult struct {
	Deleted      bool   `json:"deleted"`
	ItemName     string `json:"item_name"`
	TaskID       int64  `json:"task_id"`
	TaskName     string `json:"task_name"`
	CloudPath    string `json:"cloud_path"`
	STRMPath     string `json:"strm_path"`
	IndexRefresh bool   `json:"index_refreshed"`
	EmbyRefresh  bool   `json:"emby_refreshed"`
}

func (h *Handler) bueiEmbyLibraryDeleteProject(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil || h.strm == nil || h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req bueiEmbyLibraryDeleteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	taskID, itemID, err := bueiParseStrmItemKey(req.ItemID)
	if err != nil {
		writeErr(w, err)
		return
	}
	task, err := h.strm.GetTask(r.Context(), taskID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if task == nil {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "STRM 任务不存在"))
		return
	}
	items, err := bueiListAllStrmItems(r.Context(), h.strmScrape, task.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	var item *strmscrape.Item
	for i := range items {
		if items[i].ID == itemID {
			item = &items[i]
			break
		}
	}
	if item == nil {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "STRM 媒体项目不存在"))
		return
	}

	result, err := h.buei.EmbyArchive.DeleteItem(r.Context(), embyarchive.DeleteRequest{
		ConfigID:  strings.TrimSpace(req.ConfigID),
		LibraryID: strings.TrimSpace(req.LibraryID),
		Task:      task,
		Item:      *item,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiEmbyLibraryDeleteResult{
		Deleted:      true,
		ItemName:     result.ItemName,
		TaskID:       result.TaskID,
		TaskName:     result.TaskName,
		CloudPath:    result.CloudPath,
		STRMPath:     result.STRMPath,
		IndexRefresh: result.IndexRefresh,
		EmbyRefresh:  result.EmbyRefresh,
	})
}
