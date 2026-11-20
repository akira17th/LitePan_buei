package embyproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"litepan/internal/domain"
)

// LibraryBrowseConfig 是媒体库浏览页可见的 Emby 配置摘要，不包含 API Key。
type LibraryBrowseConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// LibraryBrowseUser 是 Emby 用户的公开摘要。
type LibraryBrowseUser struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PrimaryImageTag string `json:"primary_image_tag,omitempty"`
}

// LibraryBrowseState 是媒体库浏览页的初始化数据。
type LibraryBrowseState struct {
	Config    LibraryBrowseConfig `json:"config"`
	Libraries []Library           `json:"libraries"`
	Users     []LibraryBrowseUser `json:"users"`
}

// LibraryBrowseUserStatus 是一个媒体项目对某个 Emby 用户的观看状态。
type LibraryBrowseUserStatus struct {
	UserID            string `json:"user_id"`
	UserName          string `json:"user_name"`
	PrimaryImageTag   string `json:"primary_image_tag,omitempty"`
	Played            bool   `json:"played"`
	RemainingEpisodes int    `json:"remaining_episodes,omitempty"`
	RemainingKnown    bool   `json:"remaining_known"`
}

// LibraryBrowseItem 是媒体库列表中的电影或剧集。
type LibraryBrowseItem struct {
	ID               string                    `json:"id"`
	Name             string                    `json:"name"`
	Type             string                    `json:"type"`
	ProductionYear   int                       `json:"production_year,omitempty"`
	Overview         string                    `json:"overview,omitempty"`
	PrimaryImageTag  string                    `json:"primary_image_tag,omitempty"`
	BackdropImageTag string                    `json:"backdrop_image_tag,omitempty"`
	Users            []LibraryBrowseUserStatus `json:"users"`
}

// LibraryBrowsePage 是一个媒体库分页结果。
type LibraryBrowsePage struct {
	Items            []LibraryBrowseItem `json:"items"`
	TotalRecordCount int                 `json:"total_record_count"`
	StartIndex       int                 `json:"start_index"`
	Limit            int                 `json:"limit"`
}

// LibraryBrowsePerson 是详情页中的演职员摘要。
type LibraryBrowsePerson struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
	Type string `json:"type,omitempty"`
}

// LibraryBrowseSeason 是剧集详情页中的季摘要。
type LibraryBrowseSeason struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	IndexNumber     int    `json:"index_number,omitempty"`
	ProductionYear  int    `json:"production_year,omitempty"`
	PrimaryImageTag string `json:"primary_image_tag,omitempty"`
}

// LibraryBrowseDetail 是媒体项目详情页数据。
type LibraryBrowseDetail struct {
	LibraryBrowseItem
	PremiereDate    string                `json:"premiere_date,omitempty"`
	Tagline         string                `json:"tagline,omitempty"`
	OfficialRating  string                `json:"official_rating,omitempty"`
	CommunityRating float64               `json:"community_rating,omitempty"`
	RuntimeMinutes  int                   `json:"runtime_minutes,omitempty"`
	Genres          []string              `json:"genres,omitempty"`
	Studios         []string              `json:"studios,omitempty"`
	People          []LibraryBrowsePerson `json:"people,omitempty"`
	Seasons         []LibraryBrowseSeason `json:"seasons,omitempty"`
}

type bueiLibraryItemsPage struct {
	Items            []map[string]any `json:"Items"`
	TotalRecordCount int              `json:"TotalRecordCount"`
}

type bueiLibraryUserProgress struct {
	Played         bool
	Remaining      int
	RemainingKnown bool
}

// BrowseLibraries 返回 Emby 中可浏览的媒体库、配置摘要和用户列表。
func (s *Service) BrowseLibraries(ctx context.Context, configID string) (LibraryBrowseState, error) {
	cfg, err := s.resolveConfig(configID)
	if err != nil {
		return LibraryBrowseState{}, err
	}
	libraries, err := s.listLibraries(ctx, cfg)
	if err != nil {
		return LibraryBrowseState{}, err
	}
	users, err := s.bueiListUsers(ctx, cfg)
	if err != nil {
		return LibraryBrowseState{}, err
	}
	return LibraryBrowseState{
		Config:    LibraryBrowseConfig{ID: cfg.ID, Name: cfg.Name},
		Libraries: libraries,
		Users:     users,
	}, nil
}

// BrowseLibraryItems 返回选定媒体库中的电影和剧集，并补齐每个 Emby 用户的进度。
func (s *Service) BrowseLibraryItems(ctx context.Context, configID, libraryID string, start, limit int) (LibraryBrowsePage, error) {
	cfg, err := s.resolveConfig(configID)
	if err != nil {
		return LibraryBrowsePage{}, err
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return LibraryBrowsePage{}, domain.Errorf(domain.CodeValidation, "请选择 Emby/Jellyfin 媒体库")
	}
	if start < 0 {
		start = 0
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}

	page, err := s.bueiListLibraryItems(ctx, cfg, libraryID, start, limit)
	if err != nil {
		return LibraryBrowsePage{}, err
	}
	// 页面暂时不做前端分页，因此把选中媒体库的所有电影/剧集收齐，
	// 避免用户看到前 200 项后误以为媒体库不完整。
	allRawItems := append([]map[string]any(nil), page.Items...)
	for next := start + len(page.Items); len(page.Items) >= limit && (page.TotalRecordCount <= 0 || next < page.TotalRecordCount); next += len(page.Items) {
		nextPage, nextErr := s.bueiListLibraryItems(ctx, cfg, libraryID, next, limit)
		if nextErr != nil {
			return LibraryBrowsePage{}, nextErr
		}
		if len(nextPage.Items) == 0 {
			break
		}
		allRawItems = append(allRawItems, nextPage.Items...)
		page = nextPage
	}
	if len(allRawItems) == 0 {
		return LibraryBrowsePage{
			Items:            []LibraryBrowseItem{},
			TotalRecordCount: page.TotalRecordCount,
			StartIndex:       start,
			Limit:            limit,
		}, nil
	}

	users, err := s.bueiListUsers(ctx, cfg)
	if err != nil {
		return LibraryBrowsePage{}, err
	}
	ids := make([]string, 0, len(allRawItems))
	types := make(map[string]string, len(allRawItems))
	for _, raw := range allRawItems {
		id := strings.TrimSpace(bueiString(raw, "Id", "ID"))
		if id == "" {
			continue
		}
		ids = append(ids, id)
		types[id] = strings.TrimSpace(bueiString(raw, "Type"))
	}
	progress, err := s.bueiLoadUserProgress(ctx, cfg, users, ids, types)
	if err != nil {
		return LibraryBrowsePage{}, err
	}

	items := make([]LibraryBrowseItem, 0, len(allRawItems))
	for _, raw := range allRawItems {
		id := strings.TrimSpace(bueiString(raw, "Id", "ID"))
		if id == "" {
			continue
		}
		items = append(items, s.bueiParseBrowseItem(raw, users, progress[id]))
	}
	return LibraryBrowsePage{
		Items:            items,
		TotalRecordCount: page.TotalRecordCount,
		StartIndex:       start,
		Limit:            limit,
	}, nil
}

// BrowseLibraryItemDetail 返回一个媒体项目的详情和剧集季列表。
func (s *Service) BrowseLibraryItemDetail(ctx context.Context, configID, itemID string) (LibraryBrowseDetail, error) {
	cfg, err := s.resolveConfig(configID)
	if err != nil {
		return LibraryBrowseDetail{}, err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return LibraryBrowseDetail{}, domain.Errorf(domain.CodeValidation, "媒体项目 ID 为空")
	}

	query := mediaServerQuery(cfg.APIKey)
	query.Set("Ids", itemID)
	query.Set("Limit", "1")
	query.Set("Recursive", "true")
	query.Set("Fields", "Overview,Genres,Studios,People,ProductionYear,PremiereDate,Tagline,OfficialRating,CommunityRating,RunTimeTicks,PrimaryImageAspectRatio")
	query.Set("EnableImageTypes", "Primary,Backdrop")
	var page bueiLibraryItemsPage
	if err := s.bueiReadJSON(ctx, cfg, http.MethodGet, "Items", query, &page); err != nil {
		return LibraryBrowseDetail{}, err
	}
	if len(page.Items) == 0 {
		return LibraryBrowseDetail{}, domain.Errorf(domain.CodeNotFound, "Emby/Jellyfin 媒体项目不存在")
	}
	raw := page.Items[0]

	users, err := s.bueiListUsers(ctx, cfg)
	if err != nil {
		return LibraryBrowseDetail{}, err
	}
	typeName := strings.TrimSpace(bueiString(raw, "Type"))
	progress, err := s.bueiLoadUserProgress(ctx, cfg, users, []string{itemID}, map[string]string{itemID: typeName})
	if err != nil {
		return LibraryBrowseDetail{}, err
	}
	item := s.bueiParseBrowseItem(raw, users, progress[itemID])
	detail := LibraryBrowseDetail{
		LibraryBrowseItem: item,
		PremiereDate:      bueiDate(bueiString(raw, "PremiereDate")),
		Tagline:           strings.TrimSpace(bueiString(raw, "Tagline")),
		OfficialRating:    strings.TrimSpace(bueiString(raw, "OfficialRating")),
		CommunityRating:   bueiFloat(raw["CommunityRating"]),
		RuntimeMinutes:    bueiRuntimeMinutes(raw["RunTimeTicks"]),
		Genres:            bueiStringList(raw["Genres"]),
		Studios:           bueiStudioNames(raw["Studios"]),
		People:            bueiPeople(raw["People"]),
	}

	if strings.EqualFold(typeName, "Series") {
		seasons, seasonErr := s.bueiListSeasons(ctx, cfg, itemID)
		if seasonErr != nil {
			return LibraryBrowseDetail{}, seasonErr
		}
		detail.Seasons = seasons
	}
	return detail, nil
}

// DeleteLibraryItem 删除 Emby 中的一个媒体项目。
func (s *Service) DeleteLibraryItem(ctx context.Context, configID, itemID string) error {
	cfg, err := s.resolveConfig(configID)
	if err != nil {
		return err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return domain.Errorf(domain.CodeValidation, "媒体项目 ID 为空")
	}
	resp, err := s.bueiRequest(ctx, cfg, http.MethodDelete, "Items/"+url.PathEscape(itemID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// OpenLibraryImage 代理 Emby 媒体图片，避免把 API Key 暴露给浏览器。
func (s *Service) OpenLibraryImage(ctx context.Context, configID, itemID, imageType, tag string) (*http.Response, error) {
	cfg, err := s.resolveConfig(configID)
	if err != nil {
		return nil, err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "媒体项目 ID 为空")
	}
	kind := strings.TrimSpace(imageType)
	switch strings.ToLower(kind) {
	case "", "primary":
		kind = "Primary"
	case "backdrop":
		kind = "Backdrop"
	default:
		return nil, domain.Errorf(domain.CodeValidation, "不支持的 Emby 图片类型")
	}
	query := mediaServerQuery(cfg.APIKey)
	if strings.TrimSpace(tag) != "" {
		query.Set("Tag", strings.TrimSpace(tag))
	}
	return s.bueiRequest(ctx, cfg, http.MethodGet, "Items/"+url.PathEscape(itemID)+"/Images/"+kind, query)
}

func (s *Service) bueiListUsers(ctx context.Context, cfg Config) ([]LibraryBrowseUser, error) {
	query := mediaServerQuery(cfg.APIKey)
	query.Set("Fields", "PrimaryImageAspectRatio")
	var payload []map[string]any
	if err := s.bueiReadJSON(ctx, cfg, http.MethodGet, "Users", query, &payload); err != nil {
		return nil, err
	}
	users := make([]LibraryBrowseUser, 0, len(payload))
	for _, raw := range payload {
		id := strings.TrimSpace(bueiString(raw, "Id", "ID"))
		name := strings.TrimSpace(bueiString(raw, "Name"))
		if id == "" || name == "" {
			continue
		}
		users = append(users, LibraryBrowseUser{
			ID:              id,
			Name:            name,
			PrimaryImageTag: bueiPrimaryImageTag(raw),
		})
	}
	return users, nil
}

func (s *Service) bueiListLibraryItems(ctx context.Context, cfg Config, libraryID string, start, limit int) (bueiLibraryItemsPage, error) {
	query := mediaServerQuery(cfg.APIKey)
	query.Set("ParentId", libraryID)
	query.Set("Recursive", "true")
	query.Set("IncludeItemTypes", "Movie,Series")
	query.Set("Fields", "Overview,ProductionYear,PrimaryImageAspectRatio")
	query.Set("EnableImageTypes", "Primary,Backdrop")
	query.Set("SortBy", "SortName,ProductionYear,PremiereDate")
	query.Set("SortOrder", "Ascending")
	query.Set("StartIndex", strconv.Itoa(start))
	query.Set("Limit", strconv.Itoa(limit))
	var page bueiLibraryItemsPage
	if err := s.bueiReadJSON(ctx, cfg, http.MethodGet, "Items", query, &page); err != nil {
		return bueiLibraryItemsPage{}, err
	}
	return page, nil
}

func (s *Service) bueiLoadUserProgress(ctx context.Context, cfg Config, users []LibraryBrowseUser, ids []string, types map[string]string) (map[string]map[string]bueiLibraryUserProgress, error) {
	result := make(map[string]map[string]bueiLibraryUserProgress, len(ids))
	if len(users) == 0 || len(ids) == 0 {
		return result, nil
	}
	for _, user := range users {
		progressByID := make(map[string]bueiLibraryUserProgress, len(ids))
		for start := 0; start < len(ids); start += 100 {
			end := start + 100
			if end > len(ids) {
				end = len(ids)
			}
			query := mediaServerQuery(cfg.APIKey)
			query.Set("Ids", strings.Join(ids[start:end], ","))
			query.Set("Fields", "UserData")
			query.Set("EnableUserData", "true")
			query.Set("Limit", strconv.Itoa(end-start))
			var page bueiLibraryItemsPage
			resource := "Users/" + url.PathEscape(user.ID) + "/Items"
			if err := s.bueiReadJSON(ctx, cfg, http.MethodGet, resource, query, &page); err != nil {
				return nil, err
			}
			for _, raw := range page.Items {
				id := strings.TrimSpace(bueiString(raw, "Id", "ID"))
				if id == "" {
					continue
				}
				progressByID[id] = bueiProgressFromItem(raw)
			}
		}

		// 少数 Jellyfin/Emby 版本不会在用户项目接口返回 UnplayedItemCount，
		// 这时按剧集逐个读取分集的 UserData，仍然给出准确的剩余集数。
		for _, id := range ids {
			if !strings.EqualFold(types[id], "Series") {
				continue
			}
			progress := progressByID[id]
			if progress.RemainingKnown {
				continue
			}
			remaining, known := s.bueiCountUnplayedEpisodes(ctx, cfg, user.ID, id)
			if known {
				progress.Remaining = remaining
				progress.RemainingKnown = true
				progressByID[id] = progress
			}
		}
		for id, progress := range progressByID {
			if result[id] == nil {
				result[id] = make(map[string]bueiLibraryUserProgress)
			}
			result[id][user.ID] = progress
		}
	}
	return result, nil
}

func (s *Service) bueiCountUnplayedEpisodes(ctx context.Context, cfg Config, userID, seriesID string) (int, bool) {
	query := mediaServerQuery(cfg.APIKey)
	query.Set("UserId", userID)
	query.Set("Fields", "UserData")
	query.Set("Limit", "2000")
	var page bueiLibraryItemsPage
	resource := "Shows/" + url.PathEscape(seriesID) + "/Episodes"
	if err := s.bueiReadJSON(ctx, cfg, http.MethodGet, resource, query, &page); err != nil {
		return 0, false
	}
	remaining := 0
	for _, raw := range page.Items {
		if !bueiProgressFromItem(raw).Played {
			remaining++
		}
	}
	return remaining, true
}

func (s *Service) bueiListSeasons(ctx context.Context, cfg Config, seriesID string) ([]LibraryBrowseSeason, error) {
	query := mediaServerQuery(cfg.APIKey)
	query.Set("Fields", "PrimaryImageAspectRatio,ProductionYear")
	query.Set("EnableImageTypes", "Primary")
	var page bueiLibraryItemsPage
	resource := "Shows/" + url.PathEscape(seriesID) + "/Seasons"
	if err := s.bueiReadJSON(ctx, cfg, http.MethodGet, resource, query, &page); err != nil {
		// 部分 Emby/Jellyfin 版本不提供 Shows/{id}/Seasons，
		// 改用通用 Items 接口读取 Season 子项，详情页不应因此失败。
		fallbackQuery := mediaServerQuery(cfg.APIKey)
		fallbackQuery.Set("ParentId", seriesID)
		fallbackQuery.Set("Recursive", "false")
		fallbackQuery.Set("IncludeItemTypes", "Season")
		fallbackQuery.Set("SortBy", "IndexNumber")
		fallbackQuery.Set("SortOrder", "Ascending")
		fallbackQuery.Set("Fields", "PrimaryImageAspectRatio,ProductionYear")
		fallbackQuery.Set("EnableImageTypes", "Primary")
		if fallbackErr := s.bueiReadJSON(ctx, cfg, http.MethodGet, "Items", fallbackQuery, &page); fallbackErr != nil {
			if s.log != nil {
				s.log.Debug("Emby/Jellyfin 季列表接口不可用，跳过季信息", "series_id", seriesID, "error", fallbackErr)
			}
			return []LibraryBrowseSeason{}, nil
		}
	}
	seasons := make([]LibraryBrowseSeason, 0, len(page.Items))
	for _, raw := range page.Items {
		id := strings.TrimSpace(bueiString(raw, "Id", "ID"))
		if id == "" {
			continue
		}
		seasons = append(seasons, LibraryBrowseSeason{
			ID:              id,
			Name:            strings.TrimSpace(bueiString(raw, "Name")),
			IndexNumber:     bueiInt(raw["IndexNumber"]),
			ProductionYear:  bueiInt(raw["ProductionYear"]),
			PrimaryImageTag: bueiPrimaryImageTag(raw),
		})
	}
	return seasons, nil
}

func (s *Service) bueiParseBrowseItem(raw map[string]any, users []LibraryBrowseUser, progress map[string]bueiLibraryUserProgress) LibraryBrowseItem {
	item := LibraryBrowseItem{
		ID:               strings.TrimSpace(bueiString(raw, "Id", "ID")),
		Name:             strings.TrimSpace(bueiString(raw, "Name")),
		Type:             strings.TrimSpace(bueiString(raw, "Type")),
		ProductionYear:   bueiInt(raw["ProductionYear"]),
		Overview:         strings.TrimSpace(bueiString(raw, "Overview")),
		PrimaryImageTag:  bueiPrimaryImageTag(raw),
		BackdropImageTag: bueiBackdropImageTag(raw),
		Users:            make([]LibraryBrowseUserStatus, 0, len(users)),
	}
	for _, user := range users {
		state := progress[user.ID]
		item.Users = append(item.Users, LibraryBrowseUserStatus{
			UserID:            user.ID,
			UserName:          user.Name,
			PrimaryImageTag:   user.PrimaryImageTag,
			Played:            state.Played,
			RemainingEpisodes: state.Remaining,
			RemainingKnown:    state.RemainingKnown && strings.EqualFold(item.Type, "Series"),
		})
	}
	return item
}

func (s *Service) bueiReadJSON(ctx context.Context, cfg Config, method, resource string, query url.Values, target any) error {
	resp, err := s.bueiRequest(ctx, cfg, method, resource, query)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if target == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		if err == io.EOF {
			return nil
		}
		return domain.Errorf(domain.CodeDriverError, "读取 Emby/Jellyfin 响应失败")
	}
	return nil
}

func (s *Service) bueiRequest(ctx context.Context, cfg Config, method, resource string, query url.Values) (*http.Response, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.EmbyURL), "/")
	if base == "" || strings.TrimSpace(cfg.APIKey) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "请先完善 Emby/Jellyfin 地址和 API Key")
	}
	candidates := bueiEndpointCandidates(base, resource)
	for i, endpoint := range candidates {
		params := mediaServerQuery(cfg.APIKey)
		for key, values := range query {
			params[key] = append([]string(nil), values...)
		}
		target := endpoint
		if encoded := params.Encode(); encoded != "" {
			target += "?" + encoded
		}
		req, err := http.NewRequestWithContext(ctx, method, target, nil)
		if err != nil {
			return nil, domain.Errorf(domain.CodeValidation, "Emby/Jellyfin 请求地址无效")
		}
		setMediaServerAuth(req, cfg.APIKey)
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, embyTestConnectError(err)
		}
		if resp.StatusCode == http.StatusNotFound && i < len(candidates)-1 {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			continue
		}
		if resp.StatusCode >= http.StatusBadRequest {
			resp.Body.Close()
			return nil, embyTestHTTPError(resp.StatusCode)
		}
		return resp, nil
	}
	return nil, domainErrorNotFound("Emby/Jellyfin 接口不存在")
}

func bueiEndpointCandidates(base, resource string) []string {
	resource = strings.TrimLeft(resource, "/")
	if strings.HasSuffix(strings.ToLower(base), "/emby") {
		return []string{base + "/" + resource}
	}
	return []string{base + "/" + resource, base + "/emby/" + resource}
}

// domainErrorNotFound 保留与现有 embyproxy 错误体系一致的 404 语义，
// 具体文案不暴露上游响应内容。
func domainErrorNotFound(message string) error {
	return &domain.AppError{Code: domain.CodeNotFound, Message: message}
}

func bueiProgressFromItem(raw map[string]any) bueiLibraryUserProgress {
	data, _ := raw["UserData"].(map[string]any)
	if data == nil {
		return bueiLibraryUserProgress{}
	}
	played := bueiBool(data["Played"])
	remaining, known := bueiOptionalInt(data["UnplayedItemCount"])
	if !known && played {
		remaining, known = 0, true
	}
	return bueiLibraryUserProgress{
		Played:         played,
		Remaining:      remaining,
		RemainingKnown: known,
	}
}

func bueiPrimaryImageTag(raw map[string]any) string {
	if tag := strings.TrimSpace(bueiString(raw, "PrimaryImageTag")); tag != "" {
		return tag
	}
	if tags, ok := raw["ImageTags"].(map[string]any); ok {
		return strings.TrimSpace(bueiString(tags, "Primary"))
	}
	return ""
}

func bueiBackdropImageTag(raw map[string]any) string {
	if tags, ok := raw["BackdropImageTags"].([]any); ok && len(tags) > 0 {
		return strings.TrimSpace(fmt.Sprint(tags[0]))
	}
	if tag := strings.TrimSpace(bueiString(raw, "BackdropImageTag")); tag != "" {
		return tag
	}
	return ""
}

func bueiString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok && value != nil {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func bueiBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(v))
		return parsed
	default:
		return false
	}
}

func bueiInt(value any) int {
	parsed, _ := bueiOptionalInt(value)
	return parsed
}

func bueiOptionalInt(value any) (int, bool) {
	if value == nil {
		return 0, false
	}
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		n, err := strconv.Atoi(v.String())
		return n, err == nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	default:
		return 0, false
	}
}

func bueiFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case json.Number:
		parsed, _ := strconv.ParseFloat(v.String(), 64)
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed
	default:
		return 0
	}
}

func bueiRuntimeMinutes(value any) int {
	ticks := bueiFloat(value)
	if ticks <= 0 {
		return 0
	}
	return int(ticks / 10_000_000 / 60)
}

func bueiDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}

func bueiStringList(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func bueiStudioNames(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		if studio, ok := item.(map[string]any); ok {
			if name := strings.TrimSpace(bueiString(studio, "Name")); name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

func bueiPeople(value any) []LibraryBrowsePerson {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]LibraryBrowsePerson, 0, min(len(values), 16))
	for _, item := range values {
		person, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(bueiString(person, "Name"))
		if name == "" {
			continue
		}
		out = append(out, LibraryBrowsePerson{
			Name: name,
			Role: strings.TrimSpace(bueiString(person, "Role")),
			Type: strings.TrimSpace(bueiString(person, "Type")),
		})
		if len(out) >= 16 {
			break
		}
	}
	return out
}
