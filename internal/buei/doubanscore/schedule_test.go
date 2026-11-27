package doubanscore

import (
	"context"
	"strings"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

func tvItem(state string) strmscrape.Item {
	return strmscrape.Item{ID: "x", Title: "示例剧", MediaType: strmscrape.MediaTypeTV, TVState: state, TMDBID: "61889"}
}

func tvItemWithEnd(endDate string) strmscrape.Item {
	it := tvItem(strmscrape.TVStateEnded)
	it.EndDate = endDate
	return it
}

func movieItem() strmscrape.Item {
	return strmscrape.Item{ID: "m", Title: "示例电影", MediaType: strmscrape.MediaTypeMovie, TMDBID: "693134"}
}

func TestDecideQuerySkipsItemsWithoutTMDBID(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	item := strmscrape.Item{ID: "raw", Title: "未刮削电影", MediaType: strmscrape.MediaTypeMovie, Year: intPtr(2023)}
	d := decideQuery(nil, item, now)
	if d.Query {
		t.Fatal("没有 TMDB ID 的作品不应查询豆瓣")
	}
	if !strings.Contains(d.Reason, "TMDB") {
		t.Fatalf("跳过原因应说明缺 TMDB：%q", d.Reason)
	}
	// 即使已有评分也保持跳过。
	d = decideQuery(&domain.BueiDoubanScore{Rating: floatPtr(8.0)}, item, now)
	if d.Query {
		t.Fatal("已有评分但未刮削 TMDB 仍应跳过")
	}
}

func movieItemWithRelease(releaseDate string) strmscrape.Item {
	it := movieItem()
	it.ReleaseDate = releaseDate
	return it
}

func TestNeedsQueryRules(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	rating := floatPtr(8.1)

	cases := []struct {
		name  string
		score *domain.BueiDoubanScore
		item  strmscrape.Item
		want  bool
	}{
		{"从未查询", nil, tvItemWithEnd("2026-03-01"), true},
		{"没有评分", &domain.BueiDoubanScore{Status: domain.BueiDoubanScoreNotFound}, tvItemWithEnd("2026-03-01"), true},
		{"剧集完结不足一个月", &domain.BueiDoubanScore{Rating: rating}, tvItemWithEnd("2026-05-01"), true},
		{"剧集完结超过一个月", &domain.BueiDoubanScore{Rating: rating}, tvItemWithEnd("2026-01-01"), false},
		{"剧集未完结没有完结日期", &domain.BueiDoubanScore{Rating: rating}, tvItem(strmscrape.TVStateUpdating), true},
		{"剧集完结但日期缺失", &domain.BueiDoubanScore{Rating: rating}, tvItem(strmscrape.TVStateEnded), true},
		{"电影上映不足一个月", &domain.BueiDoubanScore{Rating: rating}, movieItemWithRelease("2026-05-10"), true},
		{"电影上映超过一个月", &domain.BueiDoubanScore{Rating: rating}, movieItemWithRelease("2025-12-01"), false},
		{"电影没有上映日期", &domain.BueiDoubanScore{Rating: rating}, movieItem(), true},
		{
			"日期格式异常按缺失处理",
			&domain.BueiDoubanScore{Rating: rating},
			movieItemWithRelease("2026/03/10"),
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsQuery(tc.score, tc.item, now); got != tc.want {
				t.Fatalf("needsQuery = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNextRunAt(t *testing.T) {
	loc := time.Local
	morning := time.Date(2026, 5, 20, 7, 30, 0, 0, loc)
	afterNoon := time.Date(2026, 5, 20, 13, 0, 0, 0, loc)

	// 从未执行、还没到当天 8 点 → 当天 8 点。
	if got := nextRunAt(morning, time.Time{}); !got.Equal(time.Date(2026, 5, 20, 8, 0, 0, 0, loc)) {
		t.Fatalf("nextRunAt(morning, zero) = %v", got)
	}
	// 从未执行、已过 8 点 → 立即执行。
	if got := nextRunAt(afterNoon, time.Time{}); !got.Equal(afterNoon) {
		t.Fatalf("nextRunAt(afterNoon, zero) = %v", got)
	}
	// 昨天跑过 → 今天 8 点。
	if got := nextRunAt(afterNoon, time.Date(2026, 5, 19, 8, 1, 0, 0, loc)); !got.Equal(time.Date(2026, 5, 20, 8, 0, 0, 0, loc)) {
		t.Fatalf("nextRunAt(afterNoon, yesterday) = %v", got)
	}
	// 今天跑过 → 明天 8 点。
	if got := nextRunAt(afterNoon, time.Date(2026, 5, 20, 8, 1, 0, 0, loc)); !got.Equal(time.Date(2026, 5, 21, 8, 0, 0, 0, loc)) {
		t.Fatalf("nextRunAt(afterNoon, today) = %v", got)
	}
}

func TestScheduleDue(t *testing.T) {
	loc := time.Local
	eight := time.Date(2026, 5, 20, 8, 0, 30, 0, loc)
	if !scheduleDue(eight, time.Date(2026, 5, 19, 8, 0, 0, 0, loc)) {
		t.Fatal("过了 8 点且昨天跑过，应到点执行")
	}
	if scheduleDue(eight, time.Date(2026, 5, 20, 8, 0, 1, 0, loc)) {
		t.Fatal("今天已经跑过，不应重复执行")
	}
	if scheduleDue(time.Date(2026, 5, 20, 7, 0, 0, 0, loc), time.Date(2026, 5, 19, 8, 0, 0, 0, loc)) {
		t.Fatal("还没到 8 点，不应执行")
	}
}

func TestBuildScoreKeepsOldRatingOnError(t *testing.T) {
	now := time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC)
	prev := &domain.BueiDoubanScore{
		Rating:      floatPtr(8.2),
		Votes:       518716,
		DoubanID:    "36882191",
		DoubanTitle: "八仙！",
		Status:      domain.BueiDoubanScoreMatched,
	}
	item := strmscrape.Item{ID: "it", Title: "八仙！ (2026)", MediaType: strmscrape.MediaTypeMovie}
	score := buildScore(7, item, prev, nil, context.DeadlineExceeded, now)
	if score.Status != domain.BueiDoubanScoreError {
		t.Fatalf("status = %q", score.Status)
	}
	if score.Rating == nil || *score.Rating != 8.2 || score.DoubanID != "36882191" {
		t.Fatalf("查询失败不应抹掉已有评分：%+v", score)
	}
	if score.CheckedAt.IsZero() {
		t.Fatal("应记录本次查询时间")
	}
}

func TestBuildScoreNotFoundSetsMessage(t *testing.T) {
	now := time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC)
	item := strmscrape.Item{ID: "it", Title: "某部片子", MediaType: strmscrape.MediaTypeMovie}
	score := buildScore(7, item, nil, nil, nil, now)
	if score.Status != domain.BueiDoubanScoreNotFound {
		t.Fatalf("status = %q, want not_found", score.Status)
	}
	if score.Rating != nil {
		t.Fatalf("未匹配时不应有评分：%+v", score)
	}
}

func TestNormalizeRunRequest(t *testing.T) {
	if _, scope, err := normalizeRunRequest(RunRequest{StrmTaskID: 3}); err != nil || scope != "task" {
		t.Fatalf("task scope: %q %v", scope, err)
	}
	if _, scope, err := normalizeRunRequest(RunRequest{StrmTaskID: 3, ItemIDs: []string{" a ", "a", ""}}); err != nil || scope != "items" {
		t.Fatalf("items scope: %q %v", scope, err)
	}
	if _, scope, err := normalizeRunRequest(RunRequest{AllTasks: true}); err != nil || scope != "all" {
		t.Fatalf("all scope: %q %v", scope, err)
	}
	if _, _, err := normalizeRunRequest(RunRequest{}); err == nil {
		t.Fatal("空请求应报错")
	}
	if _, _, err := normalizeRunRequest(RunRequest{ItemIDs: []string{"a"}}); err == nil {
		t.Fatal("按项目查询缺少任务应报错")
	}
}
