package api

import (
	"net/http"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// buei:emby-library —— 点击卡片上的「已完结 / 未完结」切换剧集完结状态。

type bueiEmbyLibraryTVStateRequest struct {
	ItemID string `json:"item_id"`
	Ended  bool   `json:"ended"`
}

type bueiEmbyLibraryTVStateResult struct {
	ItemID  string `json:"item_id"`
	TVState string `json:"tv_state"`
}

func (h *Handler) bueiEmbyLibraryTVState(w http.ResponseWriter, r *http.Request) {
	if h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req bueiEmbyLibraryTVStateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	taskID, strmItemID, err := bueiParseStrmItemKey(req.ItemID)
	if err != nil {
		writeErr(w, err)
		return
	}
	item, err := h.strmScrape.SetTVState(r.Context(), strmscrape.SetTVStateRequest{
		StrmTaskID: taskID,
		ItemID:     strmItemID,
		Ended:      req.Ended,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiEmbyLibraryTVStateResult{ItemID: req.ItemID, TVState: item.TVState})
}
