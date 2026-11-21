package api

import (
	"net/http"
	"strings"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// buei:emby-library —— 移回逻辑在 internal/buei/embyarchive。

func (h *Handler) bueiEmbyLibraryRestore(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil || h.strm == nil || h.strmScrape == nil {
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

	result, err := h.buei.EmbyArchive.RestoreItem(r.Context(), embyarchive.RestoreRequest{
		ConfigID:   strings.TrimSpace(req.ConfigID),
		TargetTask: targetTask,
		Item:       *currentItem,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiEmbyLibraryRestoreResult{
		Moved:          true,
		StrmMoved:      true,
		EmbyRefreshed:  true,
		ItemName:       result.ItemName,
		SourceTaskName: result.SourceTaskName,
		TargetTaskName: result.TargetTaskName,
		SourcePath:     result.SourcePath,
		TargetPath:     result.TargetPath,
	})
}
