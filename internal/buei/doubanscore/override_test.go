package doubanscore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// buei:douban-override —— 手动编辑项（豆瓣 ID / 评分 / 不再查询）的行为测试。

func TestDecideQueryRespectsOverrides(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	item := strmscrape.Item{ID: "a", Title: "某剧", MediaType: strmscrape.MediaTypeTV, TMDBID: "1399"}

	cases := []struct {
		name  string
		score *domain.BueiDoubanScore
		query bool
	}{
		{name: "无覆盖项", score: nil, query: true},
		{name: "不再查询", score: &domain.BueiDoubanScore{NoAutoQuery: true}, query: false},
		{name: "手动评分", score: &domain.BueiDoubanScore{ManualRating: floatPtr(9.9)}, query: false},
	}
	for _, c := range cases {
		if got := decideQuery(c.score, item, now).Query; got != c.query {
			t.Fatalf("%s：decideQuery = %v, want %v", c.name, got, c.query)
		}
	}

	// 没刮削 TMDB 时正常跳过，但手动指定豆瓣 ID 后可以通过该条目查询。
	unscraped := strmscrape.Item{ID: "b", Title: "未刮削", MediaType: strmscrape.MediaTypeMovie}
	if d := decideQuery(nil, unscraped, now); d.Query {
		t.Fatal("未刮削 TMDB 且没有手动豆瓣 ID 时不应查询")
	}
	if d := decideQuery(&domain.BueiDoubanScore{DoubanID: "1292052"}, unscraped, now); !d.Query {
		t.Fatal("已有豆瓣 ID 后应当可以查询")
	}
}

func TestRunTaskUsesStoredDoubanID(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {{ID: "a", Title: "手动指定条目的电影", MediaType: strmscrape.MediaTypeMovie}},
	}
	scores := newFakeScores()
	scores.data[scoreKey(1, "a")] = &domain.BueiDoubanScore{
		StrmTaskID: 1, ItemID: "a", MediaType: strmscrape.MediaTypeMovie,
		DoubanID: "1292052",
	}
	svc := testService(t, items, scores, now)
	svc.lookup = func(context.Context, string, *int, string) (*LookupResult, error) {
		t.Fatal("已有豆瓣 ID 时不应按片名搜索")
		return nil, nil
	}
	svc.lookupID = func(_ context.Context, id string) (*LookupResult, error) {
		if id != "1292052" {
			t.Fatalf("按豆瓣 ID 查询用了 %q", id)
		}
		return &LookupResult{DoubanID: id, DoubanTitle: "肖申克的救赎", Rating: floatPtr(9.7), Votes: 100}, nil
	}

	if _, err := svc.StartQuery(RunRequest{StrmTaskID: 1}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)

	sc := scores.data[scoreKey(1, "a")]
	if sc == nil || sc.Rating == nil || *sc.Rating != 9.7 {
		t.Fatalf("应按手动豆瓣 ID 查到评分：%+v", sc)
	}
	if sc.DoubanID != "1292052" {
		t.Fatalf("豆瓣 ID 应保留：%+v", sc)
	}
}

func TestSaveOverrideAppliesToWallAndNFOFacingScore(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {{ID: "a", Title: "老片", MediaType: strmscrape.MediaTypeMovie, Year: intPtr(2010), ReleaseDate: "2010-05-01", TMDBID: "27205"}},
	}
	scores := newFakeScores()
	svc := testService(t, items, scores, now)

	rating := 8.94 // 会被归一成 8.9（一位小数）
	if err := svc.SaveOverride(context.Background(), OverrideRequest{
		StrmTaskID:  1,
		ItemID:      "a",
		DoubanID:    "27205",
		Rating:      &rating,
		NoAutoQuery: true,
	}); err != nil {
		t.Fatalf("SaveOverride: %v", err)
	}

	page, err := svc.Items(context.Background(), ItemQuery{StrmTaskID: 1})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Douban == nil {
		t.Fatalf("海报墙数据异常：%+v", page.Items)
	}
	douban := page.Items[0].Douban
	if douban.Rating == nil || *douban.Rating != 8.9 {
		t.Fatalf("手动评分应覆盖展示：%+v", douban.Rating)
	}
	if douban.DoubanID != "27205" || !douban.NoAutoQuery || douban.ManualRating == nil {
		t.Fatalf("手动覆盖项未回显：%+v", douban)
	}
	if douban.NeedsQuery {
		t.Fatal("设为不再查询后不应再标记为待查询")
	}

	if err := svc.SaveOverride(context.Background(), OverrideRequest{
		StrmTaskID: 1,
		ItemID:     "a",
		DoubanID:   "abc",
	}); err == nil {
		t.Fatal("非数字豆瓣 ID 应当被拒绝")
	}
	bad := 11.0
	if err := svc.SaveOverride(context.Background(), OverrideRequest{
		StrmTaskID: 1,
		ItemID:     "a",
		Rating:     &bad,
	}); err == nil {
		t.Fatal("超出 0-10 的评分应当被拒绝")
	}
}

func TestRunLogRecordsItemResults(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {
			{ID: "ok", Title: "成功的电影", MediaType: strmscrape.MediaTypeMovie, TMDBID: "1"},
			{ID: "bad", Title: "失败的电影", MediaType: strmscrape.MediaTypeMovie, TMDBID: "2"},
		},
	}
	svc := testService(t, items, newFakeScores(), now)
	svc.lookup = func(_ context.Context, title string, _ *int, _ string) (*LookupResult, error) {
		if title == "失败的电影" {
			return nil, errDoubanChallenge
		}
		return &LookupResult{DoubanID: "1", DoubanTitle: title, Rating: floatPtr(8.1), Votes: 42}, nil
	}

	if _, err := svc.StartQuery(RunRequest{AllTasks: true}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)

	cfg := svc.configs.(fakeConfigs)
	raw := cfg[runLogConfigKey]
	if raw == "" {
		t.Fatal("自动任务应写入执行日志")
	}
	var runLog RunLog
	if err := json.Unmarshal([]byte(raw), &runLog); err != nil {
		t.Fatalf("解析执行日志：%v", err)
	}
	if len(runLog.Tasks) != 1 {
		t.Fatalf("任务数 = %d, want 1", len(runLog.Tasks))
	}
	task := runLog.Tasks[0]
	if task.Pending != 2 || task.Success != 1 || task.Failed != 1 {
		t.Fatalf("任务统计 = %+v", task)
	}
	if len(task.Items) != 2 {
		t.Fatalf("项目明细 = %+v", task.Items)
	}
	if task.Items[0].State != "success" || task.Items[0].Title != "成功的电影" {
		t.Fatalf("成功项目 = %+v", task.Items[0])
	}
	if task.Items[1].State != "failed" || task.Items[1].Detail != DescribeLookupError(errDoubanChallenge) {
		t.Fatalf("失败项目应带具体原因：%+v", task.Items[1])
	}
}

// buei:force —— 手动点卡片「查询 / 重查」必须直接去查：未刮削 TMDB、已有评分过期、
// 已设「不再查询」/「手动评分」这些调度规则都不能拦下它。
func TestForceQueryBypassesAllRules(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {
			// 没有 TMDB ID：正常规则会跳过。
			{ID: "no-tmdb", Title: "未刮削的电影", MediaType: strmscrape.MediaTypeMovie},
			// 上映超过 3 个月 + 已有评分：正常规则不再刷新。
			{ID: "old", Title: "老片", MediaType: strmscrape.MediaTypeMovie, ReleaseDate: "2001-05-10", TMDBID: "27205"},
			// 手动设为不再查询。
			{ID: "no-auto", Title: "不再查询的剧", MediaType: strmscrape.MediaTypeTV, TMDBID: "1399"},
			// 手动设置了评分。
			{ID: "manual", Title: "手动评分的电影", MediaType: strmscrape.MediaTypeMovie, TMDBID: "603"},
		},
	}
	scores := newFakeScores()
	scores.data[scoreKey(1, "old")] = &domain.BueiDoubanScore{
		StrmTaskID: 1, ItemID: "old", Rating: floatPtr(7.2), Status: domain.BueiDoubanScoreMatched,
	}
	scores.data[scoreKey(1, "no-auto")] = &domain.BueiDoubanScore{
		StrmTaskID: 1, ItemID: "no-auto", Rating: floatPtr(8.0), Status: domain.BueiDoubanScoreMatched,
		NoAutoQuery: true,
	}
	scores.data[scoreKey(1, "manual")] = &domain.BueiDoubanScore{
		StrmTaskID: 1, ItemID: "manual", ManualRating: floatPtr(9.5),
	}
	svc := testService(t, items, scores, now)

	queried := make(map[string]bool)
	svc.lookup = func(_ context.Context, title string, _ *int, _ string) (*LookupResult, error) {
		queried[title] = true
		return &LookupResult{DoubanID: "1", DoubanTitle: title, Rating: floatPtr(9.9), Votes: 1}, nil
	}

	if _, err := svc.StartQuery(RunRequest{
		StrmTaskID: 1,
		ItemIDs:    []string{"no-tmdb", "old", "no-auto", "manual"},
		Force:      true,
	}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)

	for _, title := range []string{"未刮削的电影", "老片", "不再查询的剧", "手动评分的电影"} {
		if !queried[title] {
			t.Fatalf("手动查询应直接查《%s》，不应被规则拦下", title)
		}
	}
	if sc := scores.data[scoreKey(1, "old")]; sc == nil || sc.Rating == nil || *sc.Rating != 9.9 {
		t.Fatalf("老片应被重新查询并更新评分：%+v", sc)
	}
	// 手动评分是展示覆盖项：查询照常执行，但评分仍以手动值为准。
	if sc := scores.data[scoreKey(1, "manual")]; sc == nil || sc.ManualRating == nil || *sc.ManualRating != 9.5 {
		t.Fatalf("手动评分不应被查询冲掉：%+v", sc)
	}
}

// buei:douban-id —— 已经有豆瓣 ID（名称匹配过一次）的作品，查询评分时直接用该 ID，
// 不再重复按片名搜索；只有 ID 查不到时才退回片名搜索。
func TestRunTaskReusesMatchedDoubanID(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {{ID: "a", Title: "老片", MediaType: strmscrape.MediaTypeMovie, TMDBID: "27205"}},
	}
	scores := newFakeScores()
	scores.data[scoreKey(1, "a")] = &domain.BueiDoubanScore{
		StrmTaskID: 1, ItemID: "a", DoubanID: "27205", Status: domain.BueiDoubanScoreNotFound,
	}
	svc := testService(t, items, scores, now)
	svc.lookup = func(context.Context, string, *int, string) (*LookupResult, error) {
		t.Fatal("已经有豆瓣 ID 时不应再按片名搜索")
		return nil, nil
	}
	svc.lookupID = func(_ context.Context, id string) (*LookupResult, error) {
		if id != "27205" {
			t.Fatalf("应按已有豆瓣 ID 查询，实际用了 %q", id)
		}
		return &LookupResult{DoubanID: id, DoubanTitle: "盗梦空间", Rating: floatPtr(9.4), Votes: 100, Source: "detail"}, nil
	}

	if _, err := svc.StartQuery(RunRequest{StrmTaskID: 1}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)

	sc := scores.data[scoreKey(1, "a")]
	if sc == nil || sc.Rating == nil || *sc.Rating != 9.4 {
		t.Fatalf("应按已有豆瓣 ID 查到评分：%+v", sc)
	}
}

// 按已有豆瓣 ID 查不到时不做名称兜底：如实记下失败原因，由用户决定是否清掉 ID。
func TestRunTaskReportsErrorWhenDoubanIDFails(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {{ID: "a", Title: "老片", MediaType: strmscrape.MediaTypeMovie, TMDBID: "27205"}},
	}
	scores := newFakeScores()
	scores.data[scoreKey(1, "a")] = &domain.BueiDoubanScore{
		StrmTaskID: 1, ItemID: "a", DoubanID: "99999999", Rating: floatPtr(7.0),
		Status: domain.BueiDoubanScoreMatched,
	}
	svc := testService(t, items, scores, now)
	svc.lookupID = func(context.Context, string) (*LookupResult, error) {
		return nil, errLookupFailed
	}
	svc.lookup = func(context.Context, string, *int, string) (*LookupResult, error) {
		t.Fatal("按豆瓣 ID 查询失败时不应退回片名搜索")
		return nil, nil
	}

	if _, err := svc.StartQuery(RunRequest{StrmTaskID: 1}); err != nil {
		t.Fatalf("StartQuery: %v", err)
	}
	waitIdle(t, svc)

	sc := scores.data[scoreKey(1, "a")]
	if sc == nil || sc.Status != domain.BueiDoubanScoreError {
		t.Fatalf("应记录查询失败状态：%+v", sc)
	}
	if sc.DoubanID != "99999999" {
		t.Fatalf("失败的 ID 应保留给用户确认：%+v", sc)
	}
	if sc.Rating == nil || *sc.Rating != 7.0 {
		t.Fatalf("查询失败不应抹掉已有评分：%+v", sc)
	}
}
