package embyproxy

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"litepan/internal/domain"
)

// RefreshLibraryDirect 直接对指定媒体库触发一次目录扫描，不校验媒体库是否存在。
// 媒体库 ID 都由 Emby/Jellyfin 同步而来，媒体库页面的操作只需要刷当前这个库，
// 因此这里省掉 ListLibraries 的往返与校验，直接 POST /Items/{id}/Refresh。
func (s *Service) RefreshLibraryDirect(ctx context.Context, configID, libraryID string) error {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return domain.Errorf(domain.CodeValidation, "请选择 Emby/Jellyfin 媒体库")
	}
	cfg, err := s.resolveConfig(configID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.EmbyURL) == "" {
		return domain.Errorf(domain.CodeValidation, "请先填写 Emby/Jellyfin 地址")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return domain.Errorf(domain.CodeValidation, "请先填写 Emby/Jellyfin API Key")
	}
	base := strings.TrimRight(cfg.EmbyURL, "/")
	query := mediaServerQuery(cfg.APIKey)
	query.Set("Recursive", "true")
	query.Set("ImageRefreshMode", "Default")
	query.Set("MetadataRefreshMode", "Default")
	query.Set("ReplaceAllImages", "false")
	query.Set("ReplaceAllMetadata", "false")
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, base+"/Items/"+url.PathEscape(libraryID)+"/Refresh?"+query.Encode(), nil,
	)
	if err != nil {
		return domain.Wrap(domain.CodeInternal, err)
	}
	setMediaServerAuth(req, cfg.APIKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return embyTestConnectError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return embyTestHTTPError(resp.StatusCode)
	}
	return nil
}
