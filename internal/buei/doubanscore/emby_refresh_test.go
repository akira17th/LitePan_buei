package doubanscore

import (
	"context"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// buei:emby-refresh —— 查到评分后触发关联媒体库刷库的联动测试。

type fakeEmbyRefresher struct {
	calls []string
}

func (f *fakeEmbyRefresher) RefreshLibraryDirect(_ context.Context, _, libraryID string) error {
	f.calls = append(f.calls, libraryID)
	return nil
}

// 整轮查询：任务 1 关联 lib-1，查到评分 → 应刷新 lib-1；任务 2 未关联 → 不刷。
func TestRunRefreshesBoundLibrary(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {{ID: "a", Title: "剧A", MediaType: strmscrape.MediaTypeTV, TMDBID: "1399"}},
		2: {{ID: "b", Title: "剧B", MediaType: strmscrape.MediaTypeTV, TMDBID: "1400"}},
	}
	scores := newFakeScores()
	libs := map[int64]domain.BueiDoubanTaskLibrary{
		1: {LibraryID: "lib-1", LibraryName: "剧集库"},
	}
	emby := &fakeEmbyRefresher{}
	svc := New(Options{
		Tasks:     fakeTasks{tasks: []*domain.StrmTask{{ID: 1, Name: "任务1"}, {ID: 2, Name: "任务2"}}},
		Items:     fakeItems{byTask: items},
		Scores:    scores,
		Configs:   fakeConfigs{},
		Libraries: fakeTaskLibs{data: libs},
		Emby:      emby,
		Now:       func() time.Time { return now },
		Lookup: func(context.Context, string, *int, string) (*LookupResult, error) {
			return &LookupResult{DoubanID: "d1", Rating: floatPtr(7.5)}, nil
		},
	})

	if _, err := svc.StartQuery(RunRequest{AllTasks: true}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)

	if len(emby.calls) != 1 || emby.calls[0] != "lib-1" {
		t.Fatalf("只应刷新任务1关联的 lib-1，实际 %v", emby.calls)
	}
}

// 任务没查到任何评分（索引为空）→ 不触发刷库。
func TestRunSkipsRefreshWhenNothingScored(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	scores := newFakeScores()
	libs := map[int64]domain.BueiDoubanTaskLibrary{
		1: {LibraryID: "lib-1", LibraryName: "剧集库"},
	}
	emby := &fakeEmbyRefresher{}
	svc := New(Options{
		Tasks:     fakeTasks{tasks: []*domain.StrmTask{{ID: 1, Name: "任务1"}}},
		Items:     fakeItems{byTask: map[int64][]strmscrape.Item{}},
		Scores:    scores,
		Configs:   fakeConfigs{},
		Libraries: fakeTaskLibs{data: libs},
		Emby:      emby,
		Now:       func() time.Time { return now },
	})

	if _, err := svc.StartQuery(RunRequest{AllTasks: true}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)
	if len(emby.calls) != 0 {
		t.Fatalf("没有查到评分不应刷库，实际 %v", emby.calls)
	}
}

// buei:run-log —— 自动任务（all）记录各任务统计；手动任务不记录。
func TestRunLogRecordedForAutoRunOnly(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {
			{ID: "a", Title: "剧A", MediaType: strmscrape.MediaTypeTV, TMDBID: "1399"},
			{ID: "b", Title: "剧B", MediaType: strmscrape.MediaTypeTV, TMDBID: "1400"},
		},
	}
	scores := newFakeScores()
	cfgs := fakeConfigs{}
	svc := New(Options{
		Tasks:   fakeTasks{tasks: []*domain.StrmTask{{ID: 1, Name: "任务1"}}},
		Items:   fakeItems{byTask: items},
		Scores:  scores,
		Configs: cfgs,
		Now:     func() time.Time { return now },
		Lookup: func(context.Context, string, *int, string) (*LookupResult, error) {
			return nil, errLookupFailed
		},
	})

	// 自动任务：失败 2 条 → 日志应记录 pending=2 failed=2。
	if _, err := svc.StartQuery(RunRequest{AllTasks: true}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)

	log, err := loadRunLog(context.Background(), cfgs)
	if err != nil || log == nil {
		t.Fatalf("自动任务应留下日志：log=%v err=%v", log, err)
	}
	if log.Total != 2 || log.Failed != 2 || len(log.Tasks) != 1 {
		t.Fatalf("日志统计不符：%+v", log)
	}
	if log.Tasks[0].TaskName != "任务1" || len(log.Tasks[0].Reasons) == 0 {
		t.Fatalf("日志应含任务名与失败原因：%+v", log.Tasks[0])
	}

	// 手动任务：不覆盖日志。
	for k := range cfgs {
		delete(cfgs, k)
	}
	if _, err := svc.StartQuery(RunRequest{StrmTaskID: 1}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)
	log, _ = loadRunLog(context.Background(), cfgs)
	if log != nil {
		t.Fatalf("手动任务不应写自动日志：%+v", log)
	}
}

// fakeTaskLibs 是任务 ↔ 媒体库关联仓储的内存实现。
type fakeTaskLibs struct {
	data map[int64]domain.BueiDoubanTaskLibrary
}

func (f fakeTaskLibs) Map(context.Context) (map[int64]domain.BueiDoubanTaskLibrary, error) {
	out := make(map[int64]domain.BueiDoubanTaskLibrary, len(f.data))
	for k, v := range f.data {
		out[k] = v
	}
	return out, nil
}

func (f fakeTaskLibs) Set(_ context.Context, taskID int64, libraryID, libraryName string) error {
	f.data[taskID] = domain.BueiDoubanTaskLibrary{LibraryID: libraryID, LibraryName: libraryName}
	return nil
}

func (f fakeTaskLibs) Delete(_ context.Context, taskID int64) error {
	delete(f.data, taskID)
	return nil
}

func waitIdle(t *testing.T, svc *Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		svc.mu.Lock()
		running := svc.running
		svc.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("查询未在 5 秒内结束")
}

// errLookupFailed 让 Lookup 返回失败（被 DescribeLookupError 归类为「其它」）。
var errLookupFailed = errFail{}

type errFail struct{}

func (errFail) Error() string { return "模拟查询失败" }
