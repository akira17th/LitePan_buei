package doubanscore

import (
	"testing"
)

// 多季条目并列时取最新一季；非季条目维持原行为。
func TestPickSearchPrefersLatestSeason(t *testing.T) {
	r1 := intPtr(2016)
	r6 := intPtr(2026)
	r7 := intPtr(2027)
	hits := []searchHit{
		{ID: "s1", Title: "一人之下 第一季", Year: r1, IsTV: true, IsSeasonEntry: true, Rating: floatPtr(8.8), Votes: 132062},
		{ID: "s2", Title: "一人之下 第二季", Year: intPtr(2017), IsTV: true, IsSeasonEntry: true, Rating: floatPtr(8.9), Votes: 111983},
		{ID: "s6", Title: "一人之下 第六季", Year: r6, IsTV: true, IsSeasonEntry: true, Rating: floatPtr(8.1), Votes: 15692},
		{ID: "s7", Title: "一人之下 第七季", Year: r7, IsTV: true, IsSeasonEntry: true}, // 没评分 105 分 < 107，天然落选
		{ID: "mov", Title: "异人之下", Year: intPtr(2024), IsTV: false, Rating: floatPtr(5.1)},
	}

	best := pickSearch(hits, "一人之下", nil, "tv")
	if best == nil || best.DoubanID != "s6" {
		t.Fatalf("整部剧应取最新有评分的一季（第六季），got %+v", best)
	}
	if best.Rating == nil || *best.Rating != 8.1 {
		t.Fatalf("应带上第六季评分 8.1：%+v", best.Rating)
	}

	// 最新一季还没有评分时，同样入选（详情兜底会补，这里只验证选择逻辑）。
	hitsNoRating := []searchHit{
		{ID: "s1", Title: "一人之下 第一季", Year: r1, IsTV: true, IsSeasonEntry: true, Rating: floatPtr(8.8)},
		{ID: "s7", Title: "一人之下 第七季", Year: r7, IsTV: true, IsSeasonEntry: true}, // 没评分 105 分 < 107，天然落选
	}
	best = pickSearch(hitsNoRating, "一人之下", nil, "tv")
	if best == nil || best.DoubanID != "s1" {
		t.Fatalf("最新季没评分时应落到有评分的旧季：got %+v", best)
	}

	// 带年份的查询：年份相同（+30）仍最高。
	best = pickSearch(hits, "一人之下", intPtr(2026), "tv")
	if best == nil || best.DoubanID != "s6" {
		t.Fatalf("wantYear=2026 应命中第六季：got %+v", best)
	}

	// 关键：索引的整部年份是首播年（2016），季数条目不因年份差被淘汰，
	// 并列后按「最新季」决胜 → 取第六季（2026）而不是第一季。
	best = pickSearch(hits, "一人之下", intPtr(2016), "tv")
	if best == nil || best.DoubanID != "s6" {
		t.Fatalf("wantYear=2016（首播年）也应命中最新有评分季：got %+v", best)
	}
	if best.Rating == nil || *best.Rating != 8.1 {
		t.Fatalf("应带上第六季评分：%+v", best.Rating)
	}

	// 非季条目并列：维持先到者，不受季决胜影响。
	ties := []searchHit{
		{ID: "a", Title: "某电影", Year: intPtr(2020), IsTV: false, Rating: floatPtr(7.0)},
		{ID: "b", Title: "某电影", Year: intPtr(2021), IsTV: false, Rating: floatPtr(7.0)},
	}
	best = pickSearch(ties, "某电影", nil, "movie")
	if best == nil || best.DoubanID != "a" {
		t.Fatalf("非季条目并列应维持先到者：got %+v", best)
	}
}

func TestIsSeasonEntryTitle(t *testing.T) {
	cases := []struct {
		title string
		want  bool
	}{
		{"一人之下 第一季", true},
		{"一人之下 第2季", true},
		{"一人之下 Season 3", true},
		{"一人之下 2", true},
		{"一人之下", false},
		{"异人之下", false},
		{"一人之下·锈铁重现", false},
	}
	for _, tc := range cases {
		if got := isSeasonEntryTitle(tc.title); got != tc.want {
			t.Fatalf("isSeasonEntryTitle(%q) = %v, want %v", tc.title, got, tc.want)
		}
	}
}
