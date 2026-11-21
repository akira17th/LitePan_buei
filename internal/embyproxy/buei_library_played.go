package embyproxy

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"litepan/internal/domain"
)

// buei:emby-library —— 用户观看状态快捷切换（标记已看完 / 取消标记）。

// SetLibraryItemPlayed 标记或取消某个 Emby 用户对项目的“已看完”状态。
// 项目为剧集时，等价于把整部剧集标记为已看/未看。
func (s *Service) SetLibraryItemPlayed(ctx context.Context, configID, userID, itemID string, played bool) error {
	userID = strings.TrimSpace(userID)
	itemID = strings.TrimSpace(itemID)
	if userID == "" || itemID == "" {
		return domain.Errorf(domain.CodeValidation, "缺少用户或媒体项目 ID")
	}
	cfg, err := s.resolveConfig(configID)
	if err != nil {
		return err
	}
	resource := "Users/" + url.PathEscape(userID) + "/PlayedItems/" + url.PathEscape(itemID)
	method := http.MethodPost
	if !played {
		method = http.MethodDelete
	}
	return s.bueiReadJSON(ctx, cfg, method, resource, mediaServerQuery(cfg.APIKey), nil)
}
