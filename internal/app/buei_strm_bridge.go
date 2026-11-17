package app

import (
	"context"
	"strings"

	"litepan/internal/core/driverexec"
	"litepan/internal/driver"
	"litepan/internal/strm"
)

// bueiStrmDirectoryGenerator 把 STRM 服务的「当前目录生成」适配成光鸭转存后可调用的回调。
// 目录条目由这里现列，避免让光鸭分享服务直接依赖驱动或 STRM 包。
func bueiStrmDirectoryGenerator(exec *driverexec.Executor, svc *strm.Service) func(ctx context.Context, accountID int64, parentID string, dirs []string) (int64, bool, error) {
	if exec == nil || svc == nil {
		return nil
	}
	return func(ctx context.Context, accountID int64, parentID string, dirs []string) (int64, bool, error) {
		var entries []strm.CurrentDirectoryEntry
		err := exec.Run(ctx, accountID, func(drv driver.Driver) error {
			items, listErr := drv.ListFiles(ctx, parentID)
			if listErr != nil {
				return listErr
			}
			entries = make([]strm.CurrentDirectoryEntry, 0, len(items))
			for _, item := range items {
				entries = append(entries, strm.CurrentDirectoryEntry{
					ID: item.ID, Name: item.Name, Size: item.Size, IsDir: item.IsDir,
				})
			}
			return nil
		})
		if err != nil {
			return 0, false, err
		}
		result, genErr := svc.GenerateCurrentDirectory(ctx, accountID, parentID, strings.Join(dirs, "/"), dirs, entries)
		if genErr != nil {
			return 0, false, genErr
		}
		return result.Created + result.Updated, result.MatchedTaskID > 0, nil
	}
}
