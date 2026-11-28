package doubanscore

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

type fakeTasks struct{ tasks []*domain.StrmTask }

func (f fakeTasks) ListTasks(context.Context) ([]*domain.StrmTask, error) { return f.tasks, nil }

type fakeItems struct{ byTask map[int64][]strmscrape.Item }

func (f fakeItems) ListItems(_ context.Context, taskID int64, q strmscrape.ItemListQuery) (strmscrape.ItemListResult, error) {
	all := f.byTask[taskID]
	start := q.Offset
	if start > len(all) {
		start = len(all)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 120
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	return strmscrape.ItemListResult{
		Items:   all[start:end],
		Total:   len(all),
		Offset:  start,
		Limit:   limit,
		HasMore: end < len(all),
	}, nil
}

type fakeScores struct {
	mu   sync.Mutex
	data map[string]*domain.BueiDoubanScore
}

func newFakeScores() *fakeScores {
	return &fakeScores{data: make(map[string]*domain.BueiDoubanScore)}
}

func scoreKey(taskID int64, itemID string) string {
	return itemID + "#" + time.Duration(taskID).String()
}

func (f *fakeScores) ListByTask(_ context.Context, taskID int64) ([]*domain.BueiDoubanScore, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.BueiDoubanScore, 0, len(f.data))
	for _, sc := range f.data {
		if sc.StrmTaskID == taskID {
			out = append(out, sc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ItemID < out[j].ItemID })
	return out, nil
}

func (f *fakeScores) Upsert(_ context.Context, scores []*domain.BueiDoubanScore) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sc := range scores {
		f.data[scoreKey(sc.StrmTaskID, sc.ItemID)] = sc
	}
	return nil
}

func (f *fakeScores) Counts(context.Context) (map[int64]domain.BueiDoubanScoreCounts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[int64]domain.BueiDoubanScoreCounts)
	for _, sc := range f.data {
		c := out[sc.StrmTaskID]
		c.Total++
		if sc.Rating != nil {
			c.Scored++
		}
		out[sc.StrmTaskID] = c
	}
	return out, nil
}

func (f *fakeScores) DeleteByTask(_ context.Context, taskID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, sc := range f.data {
		if sc.StrmTaskID == taskID {
			delete(f.data, k)
		}
	}
	return nil
}

func (f *fakeScores) SaveOverride(_ context.Context, taskID int64, itemID string, o domain.BueiDoubanScoreOverride) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := scoreKey(taskID, itemID)
	sc := f.data[key]
	if sc == nil {
		sc = &domain.BueiDoubanScore{StrmTaskID: taskID, ItemID: itemID}
		f.data[key] = sc
	}
	sc.DoubanID = o.DoubanID
	sc.ManualRating = o.ManualRating
	sc.NoAutoQuery = o.NoAutoQuery
	return nil
}

type fakeConfigs map[string]string

func (f fakeConfigs) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f[key]
	return v, ok, nil
}

func (f fakeConfigs) Set(_ context.Context, key, value string) error {
	f[key] = value
	return nil
}

func (f fakeConfigs) All(context.Context) (map[string]string, error) {
	out := make(map[string]string, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out, nil
}

func testService(t *testing.T, items map[int64][]strmscrape.Item, scores *fakeScores, now time.Time) *Service {
	t.Helper()
	tasks := make([]*domain.StrmTask, 0, len(items))
	for id := range items {
		tasks = append(tasks, &domain.StrmTask{ID: id, Name: "任务" + time.Duration(id).String()})
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return New(Options{
		Tasks:   fakeTasks{tasks: tasks},
		Items:   fakeItems{byTask: items},
		Scores:  scores,
		Configs: fakeConfigs{},
		Now:     func() time.Time { return now },
	})
}

func TestItemsMergeFilterAndSort(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {
			{ID: "a", Title: "高分剧", MediaType: strmscrape.MediaTypeTV, TVState: strmscrape.TVStateUpdating, Year: intPtr(2026), TMDBID: "1399"},
			{ID: "b", Title: "无评分电影", MediaType: strmscrape.MediaTypeMovie, Year: intPtr(2024), TMDBID: "693134"},
			{ID: "c", Title: "老片", MediaType: strmscrape.MediaTypeMovie, Year: intPtr(2010), ReleaseDate: "2010-05-01", TMDBID: "27205"},
			{ID: "d", Title: "未刮削", MediaType: strmscrape.MediaTypeMovie, Year: intPtr(2023)},
		},
	}
	scores := newFakeScores()
	scores.data[scoreKey(1, "a")] = &domain.BueiDoubanScore{StrmTaskID: 1, ItemID: "a", Rating: floatPtr(8.8), Status: domain.BueiDoubanScoreMatched}
	scores.data[scoreKey(1, "c")] = &domain.BueiDoubanScore{
		StrmTaskID: 1, ItemID: "c", Rating: floatPtr(7.2), Status: domain.BueiDoubanScoreMatched,
	}
	svc := testService(t, items, scores, now)

	page, err := svc.Items(context.Background(), ItemQuery{StrmTaskID: 1, Limit: 10})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if page.Stats.Total != 4 || page.Stats.Scored != 2 || page.Stats.Unscored != 2 {
		t.Fatalf("stats = %+v", page.Stats)
	}
	if page.Stats.Pending != 1 { // 只有「追更中且有评分」算待更新
		t.Fatalf("pending = %d, want 1", page.Stats.Pending)
	}
	// 默认按评分倒序，未评分排最后。
	if page.Items[0].ID != "a" || page.Items[1].ID != "c" || page.Items[2].ID != "b" || page.Items[3].ID != "d" {
		t.Fatalf("排序不符合预期：%s, %s, %s, %s", page.Items[0].ID, page.Items[1].ID, page.Items[2].ID, page.Items[3].ID)
	}
	if page.Items[1].Douban == nil || page.Items[1].Douban.NeedsQuery {
		t.Fatal("上映超过 1 个月的老片已有评分，不应再需要查询")
	}

	unscored, err := svc.Items(context.Background(), ItemQuery{StrmTaskID: 1, Filter: "unscored"})
	if err != nil {
		t.Fatalf("Items(unscored): %v", err)
	}
	if len(unscored.Items) != 2 || unscored.Items[0].ID != "b" || unscored.Items[1].ID != "d" {
		t.Fatalf("未评分筛选 = %+v", unscored.Items)
	}

	tvOnly, err := svc.Items(context.Background(), ItemQuery{StrmTaskID: 1, MediaType: "tv"})
	if err != nil {
		t.Fatalf("Items(tv): %v", err)
	}
	if len(tvOnly.Items) != 1 || tvOnly.Items[0].ID != "a" {
		t.Fatalf("剧集筛选 = %+v", tvOnly.Items)
	}
}

func TestRunTaskSkipsFinishedMedia(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {
			{ID: "done", Title: "完结老剧", MediaType: strmscrape.MediaTypeTV, TVState: strmscrape.TVStateEnded, Year: intPtr(2018), EndDate: "2018-06-01", TMDBID: "61889"},
			{ID: "airing", Title: "追更剧", MediaType: strmscrape.MediaTypeTV, TVState: strmscrape.TVStateUpdating, Year: intPtr(2026), TMDBID: "1399"},
		},
	}
	scores := newFakeScores()
	scores.data[scoreKey(1, "done")] = &domain.BueiDoubanScore{
		StrmTaskID: 1, ItemID: "done", Rating: floatPtr(9.1), Status: domain.BueiDoubanScoreMatched,
	}

	calls := 0
	svc := testService(t, items, scores, now)
	svc.lookup = func(_ context.Context, title string, year *int, mediaType string) (*LookupResult, error) {
		calls++
		return &LookupResult{DoubanID: "1", DoubanTitle: title, DoubanYear: year, Rating: floatPtr(7.7), IsTV: mediaType == "tv"}, nil
	}
	cfg := svc.configs.(fakeConfigs)

	if _, err := svc.StartQuery(RunRequest{StrmTaskID: 1}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		svc.mu.Lock()
		running := svc.running
		svc.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if calls != 1 {
		t.Fatalf("只应查询追更剧，实际查询 %d 次", calls)
	}
	if _, ok := scores.data[scoreKey(1, "airing")]; !ok {
		t.Fatal("追更剧应写入评分")
	}
	if sc := scores.data[scoreKey(1, "done")]; sc == nil || sc.Rating == nil || *sc.Rating != 9.1 {
		t.Fatalf("完结老剧不应被覆盖：%+v", sc)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cfg[stateConfigKey] != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cfg[stateConfigKey] == "" {
		t.Fatal("整任务查询应写入调度状态")
	}
}

func TestStateNextRun(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 5, 20, 7, 0, 0, 0, loc)
	scores := newFakeScores()
	svc := testService(t, map[int64][]strmscrape.Item{1: {}}, scores, now)
	st, err := svc.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if st.RunAt != "08:00" {
		t.Fatalf("run_at = %q", st.RunAt)
	}
	want := time.Date(2026, 5, 20, 8, 0, 0, 0, loc).Format(time.RFC3339)
	if st.NextRunAt != want {
		t.Fatalf("next_run_at = %q, want %q", st.NextRunAt, want)
	}
}

type failingItems struct{ err error }

func (f failingItems) ListItems(context.Context, int64, strmscrape.ItemListQuery) (strmscrape.ItemListResult, error) {
	return strmscrape.ItemListResult{}, f.err
}

func TestItemsTreatsMissingStrmDirAsEmpty(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	svc := testService(t, map[int64][]strmscrape.Item{1: {}}, newFakeScores(), now)
	svc.items = failingItems{err: domain.Errorf(domain.CodeValidation, "STRM 输出目录不存在：%s", "/data/strm/剧集")}

	page, err := svc.Items(context.Background(), ItemQuery{StrmTaskID: 1})
	if err != nil {
		t.Fatalf("输出目录不存在应视为空，实际报错：%v", err)
	}
	if page.Total != 0 || len(page.Items) != 0 || page.Stats.Total != 0 {
		t.Fatalf("应为空数据：%+v", page)
	}

	// 其它错误仍然要暴露出来，避免把真实故障吞掉。
	svc.items = failingItems{err: domain.Errorf(domain.CodeValidation, "STRM 输出目录无效：%s", "/data/strm")}
	if _, err := svc.Items(context.Background(), ItemQuery{StrmTaskID: 1}); err != nil {
		t.Fatalf("输出目录无效也应视为空，实际报错：%v", err)
	}

	svc.items = failingItems{err: domain.Errf(domain.CodeInternal)}
	if _, err := svc.Items(context.Background(), ItemQuery{StrmTaskID: 1}); err == nil {
		t.Fatal("其它内部错误应继续返回")
	}
}

type fakeRatings struct {
	mu   sync.Mutex
	seen []strmscrape.ItemRating
}

func (f *fakeRatings) UpdateItemRatings(_ context.Context, _ int64, ratings []strmscrape.ItemRating) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, ratings...)
	return nil
}

func TestRunTaskWritesRatingsToNFO(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {{ID: "movie", Title: "示例电影", MediaType: strmscrape.MediaTypeMovie, Year: intPtr(2024), TMDBID: "693134"}},
	}
	scores := newFakeScores()
	ratings := &fakeRatings{}
	svc := testService(t, items, scores, now)
	svc.ratings = ratings
	svc.lookup = func(_ context.Context, title string, year *int, mediaType string) (*LookupResult, error) {
		return &LookupResult{
			DoubanID:    "1",
			DoubanTitle: title,
			DoubanYear:  year,
			Rating:      floatPtr(8.6),
			Votes:       12345,
		}, nil
	}

	if _, err := svc.StartQuery(RunRequest{StrmTaskID: 1}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		svc.mu.Lock()
		running := svc.running
		svc.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	ratings.mu.Lock()
	defer ratings.mu.Unlock()
	if len(ratings.seen) != 1 {
		t.Fatalf("应写入 1 条评分，实际 %d：%+v", len(ratings.seen), ratings.seen)
	}
	got := ratings.seen[0]
	if got.ItemID != "movie" || got.Rating != 8.6 || got.Votes != 12345 {
		t.Fatalf("写入 NFO 的评分不符：%+v", got)
	}
}

func TestRunTaskSkipsItemsWithoutTMDBID(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {
			{ID: "scraped", Title: "已刮削", MediaType: strmscrape.MediaTypeMovie, Year: intPtr(2024), TMDBID: "693134"},
			{ID: "raw", Title: "未刮削电影", MediaType: strmscrape.MediaTypeMovie, Year: intPtr(2023)},
		},
	}
	scores := newFakeScores()
	calls := 0
	svc := testService(t, items, scores, now)
	svc.lookup = func(_ context.Context, title string, year *int, mediaType string) (*LookupResult, error) {
		calls++
		return &LookupResult{DoubanID: "1", DoubanTitle: title, Rating: floatPtr(7.0)}, nil
	}

	if _, err := svc.StartQuery(RunRequest{StrmTaskID: 1}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		svc.mu.Lock()
		running := svc.running
		svc.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if calls != 1 {
		t.Fatalf("只应查询已刮削的作品，实际 %d 次", calls)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.progress.Total != 1 {
		t.Fatalf("总数不应包含跳过项，got %d", svc.progress.Total)
	}
	if svc.progress.Queried != 1 {
		t.Fatalf("已查询数应为 1，got %d", svc.progress.Queried)
	}
	if status := svc.progress.Items["raw"]; status != nil {
		t.Fatalf("跳过项不应出现在逐项进度里：%+v", status)
	}
}

func TestDescribeLookupErrorClassifiesFailures(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errDoubanChallenge, "豆瓣搜索被风控拦截，请稍后重试"},
		{errors.New(`Get "https://search.douban.com": status 429`), "豆瓣接口限流，请稍后重试"},
		{errors.New(`Get "https://m.douban.com": context deadline exceeded`), "豆瓣请求超时，请稍后重试"},
		{errors.New(`dial tcp: connection refused`), "网络连接失败，请检查网络后重试"},
		{errors.New("解析豆瓣条目详情失败: unexpected end of JSON input"), "豆瓣接口返回异常"},
	}
	for _, tc := range cases {
		got := DescribeLookupError(tc.err)
		if !strings.HasPrefix(got, tc.want) {
			t.Fatalf("DescribeLookupError(%v) = %q, want 前缀 %q", tc.err, got, tc.want)
		}
	}
	if DescribeLookupError(nil) != "" {
		t.Fatal("无错误时应返回空文案")
	}
}
