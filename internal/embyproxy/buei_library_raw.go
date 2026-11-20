package embyproxy

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"litepan/internal/domain"
)

// ListLibraryItemsRaw 返回媒体库中所有电影和剧集的原始 Emby 项目信息。
//
// buei 的 Emby 同步需要保留上游返回的完整项目对象，而不是只保留海报墙使用的
// 少量字段，因此这里单独提供原始 map 接口，避免把 API Key 或响应重新暴露给前端。
func (s *Service) ListLibraryItemsRaw(ctx context.Context, configID, libraryID string) ([]map[string]any, error) {
	cfg, err := s.resolveConfig(configID)
	if err != nil {
		return nil, err
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "请选择 Emby/Jellyfin 媒体库")
	}

	const pageSize = 200
	all := make([]map[string]any, 0, pageSize)
	for start := 0; ; start += pageSize {
		query := mediaServerQuery(cfg.APIKey)
		query.Set("ParentId", libraryID)
		query.Set("Recursive", "true")
		query.Set("IncludeItemTypes", "Movie,Series")
		query.Set("Fields", strings.Join([]string{
			"AirDays", "AirTime", "Album", "AlbumArtist", "Artists", "CommunityRating",
			"CriticRating", "DateCreated", "DateLastSaved", "DateModified", "DisplayOrder", "Path",
			"EpisodeTitle", "ExternalUrls", "Genres", "HomePageUrl", "IndexNumber",
			"MediaSources", "MediaStreams", "OfficialRating", "OriginalTitle", "Overview",
			"People", "PremiereDate", "ProductionLocations", "ProviderIds", "RecursiveItemCount",
			"RunTimeTicks", "SeasonId", "SeasonName", "SeriesId", "SeriesName", "SortName",
			"Studios", "Taglines", "Tags", "UserData", "Video3DFormat", "Width", "Height",
		}, ","))
		query.Set("EnableImageTypes", "Primary,Backdrop,Thumb,Logo,Art,Disc")
		query.Set("SortBy", "SortName,ProductionYear,PremiereDate")
		query.Set("SortOrder", "Ascending")
		query.Set("StartIndex", strconv.Itoa(start))
		query.Set("Limit", strconv.Itoa(pageSize))

		var page bueiLibraryItemsPage
		if err := s.bueiReadJSON(ctx, cfg, http.MethodGet, "Items", query, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if len(page.Items) == 0 || len(page.Items) < pageSize || (page.TotalRecordCount > 0 && len(all) >= page.TotalRecordCount) {
			break
		}
	}
	return all, nil
}
