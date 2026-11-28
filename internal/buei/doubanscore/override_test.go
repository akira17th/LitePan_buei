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
	if d := decideQuery(&domain.BueiDoubanScore{ManualDoubanID: "1292052"}, unscraped, now); !d.Query {
		t.Fatal("手动指定豆瓣 ID 后应当可以查询")
	}
}

func TestRunTaskUsesManualDoubanID(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	items := map[int64][]strmscrape.Item{
		1: {{ID: "a", Title: "手动指定条目的电影", MediaType: strmscrape.MediaTypeMovie}},
	}
	scores := newFakeScores()
	scores.data[scoreKey(1, "a")] = &domain.BueiDoubanScore{
		StrmTaskID: 1, ItemID: "a", MediaType: strmscrape.MediaTypeMovie,
		ManualDoubanID: "1292052",
	}
	svc := testService(t, items, scores, now)
	svc.lookup = func(context.Context, string, *int, string) (*LookupResult, error) {
		t.Fatal("手动指定豆瓣 ID 时不应按片名搜索")
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
	if sc.DoubanID != "1292052" || sc.ManualDoubanID != "1292052" {
		t.Fatalf("豆瓣 ID 应保留手动值：%+v", sc)
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
