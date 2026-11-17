package app

import (
	"context"
	"time"

	"litepan/internal/strmscrape"
)

// bueiScrapeRunner 把刮削能力适配成光鸭转存后可调用的回调：
// 重建索引、启动刮削、等待刮削结束（按 STRM 任务判断）。
func bueiScrapeRunner(svc *strmscrape.Service) (rebuild, start, wait func(ctx context.Context, strmTaskID int64) error) {
	if svc == nil {
		return nil, nil, nil
	}
	rebuild = func(ctx context.Context, strmTaskID int64) error {
		return svc.RebuildIndex(ctx, strmTaskID)
	}
	start = func(ctx context.Context, strmTaskID int64) error {
		return svc.RunAsync(ctx, strmscrape.RunRequest{StrmTaskID: strmTaskID})
	}
	wait = func(ctx context.Context, strmTaskID int64) error {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			progress := svc.GetProgress()
			if !progress.Running && (strmTaskID <= 0 || progress.TaskID == strmTaskID) {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
	return rebuild, start, wait
}
