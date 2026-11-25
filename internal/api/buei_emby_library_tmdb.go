package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// buei:emby-library —— 项目目录里的 TMDB 搜索与手动匹配（复用刮削的 TMDB 配置）。

type bueiEmbyLibraryTMDBMatchRequest struct {
	ItemID    string `json:"item_id"`
	TMDBID    string `json:"tmdb_id"`
	MediaType string `json:"media_type"`
	Title     string `json:"title"`
	Year      *int   `json:"year"`
	ConfigID  string `json:"config_id"`
	LibraryID string `json:"library_id"`
}

type bueiEmbyLibraryTMDBMatchResult struct {
	Matched      bool   `json:"matched"`
	ItemID       string `json:"item_id"`
	TMDBID       string `json:"tmdb_id"`
	IndexRefresh bool   `json:"index_refreshed"`
	EmbyRefresh  bool   `json:"emby_refreshed"`
}

func (h *Handler) bueiEmbyLibraryTMDBSearch(w http.ResponseWriter, r *http.Request) {
	if h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		writeErr(w, domain.Errorf(domain.CodeValidation, "请输入要搜索的名称"))
		return
	}
	mediaType := strings.TrimSpace(r.URL.Query().Get("media_type"))
	if mediaType == "" {
		mediaType = "auto"
	}
	var year *int
	if raw := strings.TrimSpace(r.URL.Query().Get("year")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeErr(w, domain.Errorf(domain.CodeValidation, "year 需为整数"))
			return
		}
		year = &parsed
	}
	results, err := h.strmScrape.SearchTMDB(r.Context(), query, year, mediaType)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"results": results})
}

// bueiEmbyLibraryProjectRescrape 按当前 NFO 里的 TMDB ID 重新刮削（不改匹配）。
func (h *Handler) bueiEmbyLibraryProjectRescrape(w http.ResponseWriter, r *http.Request) {
	if h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req struct {
		ItemID    string `json:"item_id"`
		ConfigID  string `json:"config_id"`
		LibraryID string `json:"library_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	taskID, itemID, err := bueiParseStrmItemKey(req.ItemID)
	if err != nil {
		writeErr(w, err)
		return
	}
	item, err := h.strmScrape.RescrapeItem(r.Context(), taskID, itemID)
	if err != nil {
		writeErr(w, err)
		return
	}
	result := bueiEmbyLibraryTMDBMatchResult{
		Matched: true,
		ItemID:  fmt.Sprintf("%d:%s", taskID, item.ID),
		TMDBID:  strings.TrimSpace(item.TMDBID),
	}
	if _, err := h.strmScrape.RefreshIndex(r.Context(), taskID, strmscrape.ItemListQuery{
		Offset: 0,
		Limit:  200,
		Sort:   strmscrape.ItemListSortTitleAsc,
	}); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "刮削已完成，但刷新 STRM 索引失败：%v", err))
		return
	}
	result.IndexRefresh = true
	if h.embyProxy != nil {
		if err := embyarchive.RefreshLibraries(r.Context(), h.embyProxy, strings.TrimSpace(req.ConfigID), strings.TrimSpace(req.LibraryID)); err != nil {
			writeErr(w, domain.Errorf(domain.CodeDriverError, "刮削已完成，但刷新 Emby 媒体库失败：%v", err))
			return
		}
		result.EmbyRefresh = true
	}
	writeOK(w, result)
}

func (h *Handler) bueiEmbyLibraryTMDBMatch(w http.ResponseWriter, r *http.Request) {
	if h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req bueiEmbyLibraryTMDBMatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	taskID, itemID, err := bueiParseStrmItemKey(req.ItemID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(req.TMDBID) == "" {
		writeErr(w, domain.Errorf(domain.CodeValidation, "请选择要匹配的 TMDB 项目"))
		return
	}

	item, err := h.strmScrape.MatchTMDBItem(r.Context(), strmscrape.RematchRequest{
		StrmTaskID: taskID,
		ItemID:     itemID,
		TMDBID:     strings.TrimSpace(req.TMDBID),
		MediaType:  strings.TrimSpace(req.MediaType),
		Title:      strings.TrimSpace(req.Title),
		Year:       req.Year,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	result := bueiEmbyLibraryTMDBMatchResult{
		Matched: true,
		ItemID:  fmt.Sprintf("%d:%s", taskID, item.ID),
		TMDBID:  strings.TrimSpace(item.TMDBID),
	}
	if _, err := h.strmScrape.RefreshIndex(r.Context(), taskID, strmscrape.ItemListQuery{
		Offset: 0,
		Limit:  200,
		Sort:   strmscrape.ItemListSortTitleAsc,
	}); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "刮削已完成，但刷新 STRM 索引失败：%v", err))
		return
	}
	result.IndexRefresh = true
	if h.embyProxy != nil {
		if err := embyarchive.RefreshLibraries(r.Context(), h.embyProxy, strings.TrimSpace(req.ConfigID), strings.TrimSpace(req.LibraryID)); err != nil {
			writeErr(w, domain.Errorf(domain.CodeDriverError, "刮削已完成，但刷新 Emby 媒体库失败：%v", err))
			return
		}
		result.EmbyRefresh = true
	}
	writeOK(w, result)
}
