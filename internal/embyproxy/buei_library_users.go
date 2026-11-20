package embyproxy

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// LibraryUserItemStatus 是某个 Emby 用户在一个媒体项目上的完整观看状态。
// 原始 UserData 和项目响应同时保留，便于后续增加用户维度的信息而不需要重新设计文件格式。
type LibraryUserItemStatus struct {
	UserID            string         `json:"user_id"`
	UserName          string         `json:"user_name"`
	PrimaryImageTag   string         `json:"primary_image_tag,omitempty"`
	ItemID            string         `json:"item_id"`
	ItemName          string         `json:"item_name,omitempty"`
	ItemType          string         `json:"item_type,omitempty"`
	Played            bool           `json:"played"`
	WatchedEpisodes   int            `json:"watched_episodes,omitempty"`
	RemainingEpisodes int            `json:"remaining_episodes,omitempty"`
	RemainingKnown    bool           `json:"remaining_known"`
	TotalEpisodes     int            `json:"total_episodes,omitempty"`
	UserData          map[string]any `json:"user_data,omitempty"`
	Raw               map[string]any `json:"raw,omitempty"`
}

// ListLibraryItemUserStatuses 查询媒体库项目对应的所有 Emby 用户观看状态。
// rawItems 应来自 ListLibraryItemsRaw，返回值按 Emby 项目 ID 分组。
func (s *Service) ListLibraryItemUserStatuses(ctx context.Context, configID string, rawItems []map[string]any) (map[string][]LibraryUserItemStatus, error) {
	cfg, err := s.resolveConfig(configID)
	if err != nil {
		return nil, err
	}
	users, err := s.bueiListUsers(ctx, cfg)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]LibraryUserItemStatus, len(rawItems))
	if len(rawItems) == 0 || len(users) == 0 {
		return result, nil
	}

	ids := make([]string, 0, len(rawItems))
	itemsByID := make(map[string]map[string]any, len(rawItems))
	for _, raw := range rawItems {
		id := strings.TrimSpace(bueiString(raw, "Id", "ID"))
		if id == "" {
			continue
		}
		ids = append(ids, id)
		itemsByID[id] = raw
	}

	// RecursiveItemCount 在不同 Emby/Jellyfin 版本中可能包含季节点，
	// 优先通过分集接口取得精确的剧集总集数；接口不可用时再回退到项目字段。
	seriesTotals := make(map[string]int)
	for id, raw := range itemsByID {
		if !strings.EqualFold(strings.TrimSpace(bueiString(raw, "Type")), "Series") {
			continue
		}
		if total, _, known := s.bueiCountEpisodeStats(ctx, cfg, "", id); known && total > 0 {
			seriesTotals[id] = total
		}
	}

	for _, user := range users {
		userItems := make(map[string]map[string]any, len(ids))
		for start := 0; start < len(ids); start += 100 {
			end := min(start+100, len(ids))
			query := mediaServerQuery(cfg.APIKey)
			query.Set("Ids", strings.Join(ids[start:end], ","))
			query.Set("Fields", "UserData,RecursiveItemCount,Name,Type,ProductionYear,ProviderIds,Path")
			query.Set("EnableUserData", "true")
			query.Set("Limit", strconv.Itoa(end-start))
			var page bueiLibraryItemsPage
			resource := "Users/" + url.PathEscape(user.ID) + "/Items"
			if err := s.bueiReadJSON(ctx, cfg, http.MethodGet, resource, query, &page); err != nil {
				return nil, err
			}
			for _, raw := range page.Items {
				id := strings.TrimSpace(bueiString(raw, "Id", "ID"))
				if id != "" {
					userItems[id] = raw
				}
			}
		}

		for _, id := range ids {
			base := itemsByID[id]
			raw := userItems[id]
			if raw == nil {
				raw = map[string]any{
					"Id":   id,
					"Name": bueiString(base, "Name"),
					"Type": bueiString(base, "Type"),
				}
			}
			progress := bueiProgressFromItem(raw)
			itemType := strings.TrimSpace(bueiString(base, "Type"))
			if itemType == "" {
				itemType = strings.TrimSpace(bueiString(raw, "Type"))
			}
			total := bueiInt(raw["RecursiveItemCount"])
			if exactTotal := seriesTotals[id]; exactTotal > 0 {
				total = exactTotal
			} else if total <= 0 {
				total = bueiInt(base["RecursiveItemCount"])
			}
			remaining := progress.Remaining
			remainingKnown := progress.RemainingKnown
			if strings.EqualFold(itemType, "Series") && (total <= 0 || !remainingKnown) {
				if countedTotal, countedRemaining, known := s.bueiCountEpisodeStats(ctx, cfg, user.ID, id); known {
					total = countedTotal
					remaining = countedRemaining
					remainingKnown = true
				}
			}
			watched := 0
			if strings.EqualFold(itemType, "Series") {
				if total > 0 && remainingKnown {
					watched = max(0, total-remaining)
				}
			} else if progress.Played {
				// 电影不是分集媒体，使用 1/1 表示完成状态，仍统一保存到用户文件。
				watched = 1
			}
			played := progress.Played
			if strings.EqualFold(itemType, "Series") && total > 0 && remainingKnown && remaining == 0 {
				played = true
			}
			userData, _ := raw["UserData"].(map[string]any)
			result[id] = append(result[id], LibraryUserItemStatus{
				UserID:            user.ID,
				UserName:          user.Name,
				PrimaryImageTag:   user.PrimaryImageTag,
				ItemID:            id,
				ItemName:          strings.TrimSpace(bueiString(base, "Name")),
				ItemType:          itemType,
				Played:            played,
				WatchedEpisodes:   watched,
				RemainingEpisodes: remaining,
				RemainingKnown:    remainingKnown,
				TotalEpisodes:     total,
				UserData:          userData,
				Raw:               raw,
			})
		}
	}
	return result, nil
}

func (s *Service) bueiCountEpisodeStats(ctx context.Context, cfg Config, userID, seriesID string) (total, remaining int, known bool) {
	query := mediaServerQuery(cfg.APIKey)
	if strings.TrimSpace(userID) != "" {
		query.Set("UserId", userID)
	}
	query.Set("Fields", "UserData")
	query.Set("Limit", "2000")
	var page bueiLibraryItemsPage
	resource := "Shows/" + url.PathEscape(seriesID) + "/Episodes"
	if err := s.bueiReadJSON(ctx, cfg, http.MethodGet, resource, query, &page); err != nil {
		return 0, 0, false
	}
	for _, raw := range page.Items {
		total++
		if !bueiProgressFromItem(raw).Played {
			remaining++
		}
	}
	return total, remaining, true
}
