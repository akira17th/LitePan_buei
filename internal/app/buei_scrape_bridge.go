package app

import (
	"context"
	"strings"
	"time"

	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

// bueiScrapeRunner 把刮削能力适配成光鸭转存后可调用的回调：
// 重建索引、启动刮削（只刮本次转存的作品）、等待刮削结束（按 STRM 任务判断）。
func bueiScrapeRunner(strmSvc *strm.Service, svc *strmscrape.Service) (
	rebuild func(ctx context.Context, strmTaskID int64) error,
	start func(ctx context.Context, strmTaskID int64, remoteDirs []string) error,
	wait func(ctx context.Context, strmTaskID int64) error,
) {
	if svc == nil {
		return nil, nil, nil
	}
	rebuild = func(ctx context.Context, strmTaskID int64) error {
		return svc.RebuildIndex(ctx, strmTaskID)
	}
	start = func(ctx context.Context, strmTaskID int64, remoteDirs []string) error {
		return svc.RunAsync(ctx, strmscrape.RunRequest{
			StrmTaskID:  strmTaskID,
			IncludeDirs: bueiScrapeIncludeDirs(ctx, strmSvc, strmTaskID, remoteDirs),
		})
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

// bueiScrapeIncludeDirs 把转存的远端目标目录换算成「刮削任务输出根」下的相对目录。
// 任务路径不是目标目录前缀、或目标就是任务根时返回 nil，退回刮整个任务。
func bueiScrapeIncludeDirs(ctx context.Context, strmSvc *strm.Service, strmTaskID int64, dirs []string) []string {
	if strmSvc == nil || strmTaskID <= 0 || len(dirs) == 0 {
		return nil
	}
	task, err := strmSvc.GetTask(ctx, strmTaskID)
	if err != nil || task == nil {
		return nil
	}
	rel, ok := bueiDirsBelowRoot(dirs, bueiRemoteSegments(task.Path))
	if !ok || len(rel) == 0 {
		return nil
	}
	// 注意：这里必须拼成一个相对目录，不能把每一段都当成独立的包含范围，
	// 否则第一段（如「电视剧」）会把该层下的所有作品都带上。
	joined := bueiJoinSafeDirs(rel)
	if joined == "" {
		return nil
	}
	return []string{joined}
}

func bueiJoinSafeDirs(dirs []string) string {
	parts := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" || dir == "." || dir == ".." {
			continue
		}
		if safe := strings.TrimSpace(strm.SafeName(dir)); safe != "" {
			parts = append(parts, safe)
		}
	}
	return strings.Join(parts, "/")
}

// bueiDirsBelowRoot 与 strm.segmentsBelowRoot 保持同样的前缀匹配规则。
func bueiDirsBelowRoot(segs, rootSegs []string) ([]string, bool) {
	if len(rootSegs) == 0 {
		return segs, true
	}
	want := strings.ToLower(strm.SafeName(strings.Join(rootSegs, "/")))
	for k := 1; k <= len(segs); k++ {
		if strings.ToLower(strm.SafeName(strings.Join(segs[:k], "/"))) == want {
			return segs[k:], true
		}
	}
	return nil, false
}

func bueiRemoteSegments(p string) []string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
