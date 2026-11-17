package app

import (
	"context"

	"litepan/internal/embyproxy"
)

// bueiEmbyRefresher 把 Emby/Jellyfin 刷库适配成光鸭转存后可调用的回调。
func bueiEmbyRefresher(svc *embyproxy.Service) func(ctx context.Context, configID, mode, libraryID string) error {
	if svc == nil {
		return nil
	}
	return func(ctx context.Context, configID, mode, libraryID string) error {
		_, err := svc.RefreshLibrary(ctx, embyproxy.RefreshRequest{
			ConfigID:  configID,
			Mode:      mode,
			LibraryID: libraryID,
		})
		return err
	}
}
