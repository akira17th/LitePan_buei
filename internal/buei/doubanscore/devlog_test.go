package doubanscore

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// buei:devlog —— 开发环境的查询日志：开启时必须能看到入参 / 命中 / 本轮汇总，
// 关闭时（生产环境）不能多出任何「[豆瓣评分]」行。

func devLogService(t *testing.T, buf *bytes.Buffer, verbose bool) *Service {
	t.Helper()
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {{ID: "a", Title: "示例电影 2024", MediaType: strmscrape.MediaTypeMovie, Year: intPtr(2024), TMDBID: "693134"}},
	}
	tasks := []*domain.StrmTask{{ID: 1, Name: "示例任务"}}
	svc := New(Options{
		Tasks:      fakeTasks{tasks: tasks},
		Items:      fakeItems{byTask: items},
		Scores:     newFakeScores(),
		Configs:    fakeConfigs{},
		Log:        slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
		Now:        func() time.Time { return now },
		DevVerbose: verbose,
	})
	svc.lookup = func(context.Context, string, *int, string) (*LookupResult, error) {
		return &LookupResult{DoubanID: "1", DoubanTitle: "示例电影", Rating: floatPtr(8.6), Votes: 10, Source: "search"}, nil
	}
	return svc
}

func TestDevVerboseLogsQuery(t *testing.T) {
	var buf bytes.Buffer
	svc := devLogService(t, &buf, true)

	if _, err := svc.StartQuery(RunRequest{StrmTaskID: 1}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)

	out := buf.String()
	for _, want := range []string{
		"[豆瓣评分] 开始查询",
		"任务「示例任务」：索引 1 项，待查 1 项",
		"查询[a]《示例电影 2024》",
		"命中《示例电影》",
		"本轮结束：待查 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("开发日志缺少 %q，实际输出：\n%s", want, out)
		}
	}
}

func TestDevVerboseDisabledByDefault(t *testing.T) {
	var buf bytes.Buffer
	svc := devLogService(t, &buf, false)

	if _, err := svc.StartQuery(RunRequest{StrmTaskID: 1}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)

	if strings.Contains(buf.String(), "[豆瓣评分]") {
		t.Fatalf("未开启开发日志时不应输出查询过程：\n%s", buf.String())
	}
}
