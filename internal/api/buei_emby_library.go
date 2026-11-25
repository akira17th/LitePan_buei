package api

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
)

type bueiEmbyLibraryUserDTO struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PrimaryImageTag string `json:"primary_image_tag,omitempty"`
}

type bueiEmbyLibraryUserStatusDTO struct {
	UserID            string `json:"user_id"`
	UserName          string `json:"user_name"`
	PrimaryImageTag   string `json:"primary_image_tag,omitempty"`
	Played            bool   `json:"played"`
	WatchedEpisodes   int    `json:"watched_episodes"`
	RemainingEpisodes *int   `json:"remaining_episodes,omitempty"`
	TotalEpisodes     int    `json:"total_episodes,omitempty"`
}

type bueiEmbyLibraryItemDTO struct {
	ID             string                         `json:"id"`
	Name           string                         `json:"name"`
	Type           string                         `json:"type"`
	ProductionYear int                            `json:"production_year,omitempty"`
	Overview       string                         `json:"overview,omitempty"`
	PosterURL      string                         `json:"poster_url,omitempty"`
	BackdropURL    string                         `json:"backdrop_url,omitempty"`
	TotalEpisodes  int                            `json:"total_episodes,omitempty"`
	Users          []bueiEmbyLibraryUserStatusDTO `json:"users"`
}

type bueiEmbyLibraryStateDTO struct {
	Config    embyproxy.LibraryBrowseConfig `json:"config"`
	Libraries []embyproxy.Library           `json:"libraries"`
	Users     []bueiEmbyLibraryUserDTO      `json:"users"`
	// Syncs 记录各媒体库最近一次「Emby 同步」成功时间（library_id → RFC3339）。
	Syncs map[string]string `json:"syncs"`
}

type bueiEmbyLibraryPageDTO struct {
	Items            []bueiEmbyLibraryItemDTO `json:"items"`
	TotalRecordCount int                      `json:"total_record_count"`
	StartIndex       int                      `json:"start_index"`
	Limit            int                      `json:"limit"`
}

type bueiEmbyLibraryPersonDTO struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
	Type string `json:"type,omitempty"`
}

type bueiEmbyLibrarySeasonDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	IndexNumber    int    `json:"index_number,omitempty"`
	ProductionYear int    `json:"production_year,omitempty"`
	PosterURL      string `json:"poster_url,omitempty"`
}

type bueiEmbyLibraryDetailDTO struct {
	Item            bueiEmbyLibraryItemDTO     `json:"item"`
	PremiereDate    string                     `json:"premiere_date,omitempty"`
	Tagline         string                     `json:"tagline,omitempty"`
	OfficialRating  string                     `json:"official_rating,omitempty"`
	CommunityRating float64                    `json:"community_rating,omitempty"`
	RuntimeMinutes  int                        `json:"runtime_minutes,omitempty"`
	Genres          []string                   `json:"genres,omitempty"`
	Studios         []string                   `json:"studios,omitempty"`
	People          []bueiEmbyLibraryPersonDTO `json:"people,omitempty"`
	Seasons         []bueiEmbyLibrarySeasonDTO `json:"seasons,omitempty"`
}

func (h *Handler) bueiEmbyLibraryState(w http.ResponseWriter, r *http.Request) {
	if h.embyProxy == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	state, err := h.embyProxy.BrowseLibraries(r.Context(), strings.TrimSpace(r.URL.Query().Get("config_id")))
	if err != nil {
		writeErr(w, err)
		return
	}
	users := make([]bueiEmbyLibraryUserDTO, 0, len(state.Users))
	for _, user := range state.Users {
		users = append(users, bueiEmbyLibraryUserDTO{
			ID:              user.ID,
			Name:            user.Name,
			PrimaryImageTag: user.PrimaryImageTag,
		})
	}
	syncs := make(map[string]string, len(state.Libraries))
	if h.buei.EmbyLibrarySync != nil {
		if records, err := h.buei.EmbyLibrarySync.List(r.Context()); err == nil {
			for _, record := range records {
				if record == nil || record.LastSyncedAt.IsZero() {
					continue
				}
				syncs[record.LibraryID] = record.LastSyncedAt.UTC().Format(time.RFC3339)
			}
		}
	}
	writeOK(w, bueiEmbyLibraryStateDTO{Config: state.Config, Libraries: state.Libraries, Users: users, Syncs: syncs})
}

func (h *Handler) bueiEmbyLibraryItems(w http.ResponseWriter, r *http.Request) {
	if h.embyProxy == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	start, err := bueiEmbyLibraryQueryInt(r, "start_index", 0)
	if err != nil {
		writeErr(w, err)
		return
	}
	limit, err := bueiEmbyLibraryQueryInt(r, "limit", 200)
	if err != nil {
		writeErr(w, err)
		return
	}
	configID := strings.TrimSpace(r.URL.Query().Get("config_id"))
	page, err := h.embyProxy.BrowseLibraryItems(
		r.Context(),
		configID,
		strings.TrimSpace(r.URL.Query().Get("library_id")),
		start,
		limit,
	)
	if err != nil {
		writeErr(w, err)
		return
	}
	items := make([]bueiEmbyLibraryItemDTO, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, bueiEmbyLibraryItemDTOFrom(configID, item))
	}
	writeOK(w, bueiEmbyLibraryPageDTO{
		Items:            items,
		TotalRecordCount: page.TotalRecordCount,
		StartIndex:       page.StartIndex,
		Limit:            page.Limit,
	})
}

func (h *Handler) bueiEmbyLibraryItemDetail(w http.ResponseWriter, r *http.Request) {
	if h.embyProxy == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	configID := strings.TrimSpace(r.URL.Query().Get("config_id"))
	itemID, err := h.bueiResolveEmbyItemID(r.Context(), strings.TrimSpace(chi.URLParam(r, "itemID")))
	if err != nil {
		writeErr(w, err)
		return
	}
	detail, err := h.embyProxy.BrowseLibraryItemDetail(
		r.Context(),
		configID,
		itemID,
	)
	if err != nil {
		writeErr(w, err)
		return
	}
	seasons := make([]bueiEmbyLibrarySeasonDTO, 0, len(detail.Seasons))
	for _, season := range detail.Seasons {
		seasons = append(seasons, bueiEmbyLibrarySeasonDTO{
			ID:             season.ID,
			Name:           season.Name,
			IndexNumber:    season.IndexNumber,
			ProductionYear: season.ProductionYear,
			PosterURL:      bueiEmbyLibraryImageURL(configID, season.ID, "Primary", season.PrimaryImageTag),
		})
	}
	people := make([]bueiEmbyLibraryPersonDTO, 0, len(detail.People))
	for _, person := range detail.People {
		people = append(people, bueiEmbyLibraryPersonDTO{Name: person.Name, Role: person.Role, Type: person.Type})
	}
	writeOK(w, bueiEmbyLibraryDetailDTO{
		Item:            bueiEmbyLibraryItemDTOFrom(configID, detail.LibraryBrowseItem),
		PremiereDate:    detail.PremiereDate,
		Tagline:         detail.Tagline,
		OfficialRating:  detail.OfficialRating,
		CommunityRating: detail.CommunityRating,
		RuntimeMinutes:  detail.RuntimeMinutes,
		Genres:          detail.Genres,
		Studios:         detail.Studios,
		People:          people,
		Seasons:         seasons,
	})
}

func (h *Handler) bueiEmbyLibraryDeleteItem(w http.ResponseWriter, r *http.Request) {
	if h.embyProxy == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	itemID, err := h.bueiResolveEmbyItemID(r.Context(), strings.TrimSpace(chi.URLParam(r, "itemID")))
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.embyProxy.DeleteLibraryItem(
		r.Context(),
		strings.TrimSpace(r.URL.Query().Get("config_id")),
		itemID,
	); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"deleted": true})
}

func (h *Handler) bueiEmbyLibraryImage(w http.ResponseWriter, r *http.Request) {
	if h.embyProxy == nil {
		http.NotFound(w, r)
		return
	}
	resp, err := h.embyProxy.OpenLibraryImage(
		r.Context(),
		strings.TrimSpace(r.URL.Query().Get("config_id")),
		strings.TrimSpace(chi.URLParam(r, "itemID")),
		r.URL.Query().Get("type"),
		r.URL.Query().Get("tag"),
	)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer resp.Body.Close()
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if resp.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func bueiEmbyLibraryItemDTOFrom(configID string, item embyproxy.LibraryBrowseItem) bueiEmbyLibraryItemDTO {
	users := make([]bueiEmbyLibraryUserStatusDTO, 0, len(item.Users))
	for _, status := range item.Users {
		var remaining *int
		if status.RemainingKnown {
			value := status.RemainingEpisodes
			remaining = &value
		}
		users = append(users, bueiEmbyLibraryUserStatusDTO{
			UserID:            status.UserID,
			UserName:          status.UserName,
			PrimaryImageTag:   status.PrimaryImageTag,
			Played:            status.Played,
			RemainingEpisodes: remaining,
		})
	}
	return bueiEmbyLibraryItemDTO{
		ID:             item.ID,
		Name:           item.Name,
		Type:           item.Type,
		ProductionYear: item.ProductionYear,
		Overview:       item.Overview,
		PosterURL:      bueiEmbyLibraryImageURL(configID, item.ID, "Primary", item.PrimaryImageTag),
		BackdropURL:    bueiEmbyLibraryImageURL(configID, item.ID, "Backdrop", item.BackdropImageTag),
		Users:          users,
	}
}

func bueiEmbyLibraryImageURL(configID, itemID, imageType, tag string) string {
	if strings.TrimSpace(itemID) == "" {
		return ""
	}
	query := url.Values{"type": {imageType}}
	if strings.TrimSpace(configID) != "" {
		query.Set("config_id", configID)
	}
	if strings.TrimSpace(tag) != "" {
		query.Set("tag", tag)
	}
	return "/api/admin/buei/emby-library/images/" + url.PathEscape(itemID) + "?" + query.Encode()
}

func bueiEmbyLibraryQueryInt(r *http.Request, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, domain.Errorf(domain.CodeValidation, "%s 参数无效", key)
	}
	return value, nil
}
