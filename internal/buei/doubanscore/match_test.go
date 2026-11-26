package doubanscore

import (
	"fmt"
	"testing"
)

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }

func TestCleanQueryTitle(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"醉梦", "醉梦"},
		{"醉梦 (2026)", "醉梦"},
		{"醉梦‎ （2026）", "醉梦"}, // 豆瓣标题会带 U+200E
		{"某剧 S02E03", "某剧"},
		{"某剧 第2季", "某剧"},
		{"八仙！ {tmdb-12345}", "八仙！"},
		{"三体 [tmdb=4567]", "三体"},
		{"  ", ""},
	}
	for _, tc := range cases {
		if got := cleanQueryTitle(tc.raw); got != tc.want {
			t.Errorf("cleanQueryTitle(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSplitTitleYear(t *testing.T) {
	title, year := splitTitleYear("醉梦‎ (2026)")
	if title != "醉梦" || year == nil || *year != 2026 {
		t.Fatalf("splitTitleYear = (%q, %v), want (醉梦, 2026)", title, year)
	}
	title, year = splitTitleYear("八仙！")
	if title != "八仙！" || year != nil {
		t.Fatalf("splitTitleYear = (%q, %v), want (八仙！, nil)", title, year)
	}
}

func TestNormalizeTitle(t *testing.T) {
	if a, b := normalizeTitle("八仙！"), normalizeTitle("八仙!"); a != b {
		t.Fatalf("标点应被忽略：%q != %q", a, b)
	}
	if a, b := normalizeTitle("Spider Man"), normalizeTitle("spiderman"); a != b {
		t.Fatalf("空格与大小写应被忽略：%q != %q", a, b)
	}
}

func TestPickSearchPrefersExactYearAndType(t *testing.T) {
	hits := []searchHit{
		{ID: "1", Title: "醉梦", Year: intPtr(2026), Rating: floatPtr(7.4), IsTV: true},
		{ID: "2", Title: "醉玲珑番外之玲珑醉梦", Year: intPtr(2017), Rating: floatPtr(6.2), IsTV: true},
		{ID: "3", Title: "醉梦", Year: intPtr(2026), Rating: floatPtr(5.0), IsTV: false},
	}
	best := pickSearch(hits, "醉梦", intPtr(2026), "tv")
	if best == nil || best.DoubanID != "1" {
		t.Fatalf("应命中剧集 1，得到 %+v", best)
	}
	if best.Rating == nil || *best.Rating != 7.4 {
		t.Fatalf("评分应取 7.4，得到 %+v", best.Rating)
	}
}

func TestPickSearchRejectsTypeMismatch(t *testing.T) {
	// 电影《八仙！》不应匹配到同名剧集，也不应因为「八仙」是老片而命中。
	hits := []searchHit{
		{ID: "tv", Title: "八仙过海", Year: intPtr(1985), Rating: floatPtr(7.7), IsTV: true},
		{ID: "old", Title: "八仙", Year: intPtr(2018), IsTV: false},
	}
	if best := pickSearch(hits, "八仙！", intPtr(2026), "movie"); best != nil {
		t.Fatalf("不应命中，得到 %+v", best)
	}
	hits = append(hits, searchHit{ID: "target", Title: "八仙！", Year: intPtr(2026), Rating: floatPtr(8.2), IsTV: false})
	if best := pickSearch(hits, "八仙！", intPtr(2026), "movie"); best == nil || best.DoubanID != "target" {
		t.Fatalf("应命中 target，得到 %+v", best)
	}
}

func TestPickSearchNoRatingStillMatches(t *testing.T) {
	hits := []searchHit{{ID: "9", Title: "某部冷门片", Year: intPtr(1999), IsTV: false}}
	best := pickSearch(hits, "某部冷门片", intPtr(1999), "movie")
	if best == nil || best.Rating != nil {
		t.Fatalf("无评分也应返回条目，得到 %+v", best)
	}
}

func TestPickSuggestSkipsCelebrityStyleCandidates(t *testing.T) {
	items := []LookupResult{
		{DoubanID: "1564520", DoubanTitle: "八仙", IsTV: false, Source: "suggest"},
		{DoubanID: "36882191", DoubanTitle: "八仙！", DoubanYear: intPtr(2026), IsTV: false, Source: "suggest"},
	}
	best := pickSuggest(items, "八仙！", intPtr(2026), "movie")
	if best == nil || best.DoubanID != "36882191" {
		t.Fatalf("应命中 36882191，得到 %+v", best)
	}
}

func TestPickSuggestTVByEpisode(t *testing.T) {
	items := []LookupResult{
		{DoubanID: "tv", DoubanTitle: "三体", DoubanYear: intPtr(2023), IsTV: true},
		{DoubanID: "movie", DoubanTitle: "三体", DoubanYear: intPtr(2030), IsTV: false},
	}
	best := pickSuggest(items, "三体", intPtr(2023), "tv")
	if best == nil || best.DoubanID != "tv" {
		t.Fatalf("应命中剧集 tv，得到 %+v", best)
	}
}

func TestScoreCandidateThreshold(t *testing.T) {
	// 「八仙！」 vs 「八仙过海」：包含关系得分不足以越过阈值。
	score := scoreCandidate("八仙过海", intPtr(1985), true, true, "八仙！", intPtr(2026), "movie")
	if score >= matchThreshold {
		t.Fatalf("分数 %.1f 不应越过阈值 %.1f", score, matchThreshold)
	}
	score = scoreCandidate("八仙！", intPtr(2026), false, true, "八仙！", intPtr(2026), "movie")
	if score < matchThreshold {
		t.Fatalf("完全匹配分数 %.1f 应越过阈值 %.1f", score, matchThreshold)
	}
}

func TestDecodeDoubanData(t *testing.T) {
	html := `<html><script>window.__DATA__ = {"items":[{"tpl_name":"search_subject","title":"醉梦‎ (2026)","url":"https://movie.douban.com/subject/37042229/","rating":{"value":7.4,"count":108833},"labels":[{"text":"剧集"}],"more_url":""}],"total":11}</script>`
	data, err := decodeDoubanData([]byte(html))
	if err != nil {
		t.Fatalf("decodeDoubanData: %v", err)
	}
	if len(data.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(data.Items))
	}
	item := data.Items[0]
	if !labelsContainTV(item) {
		t.Fatal("应识别为剧集")
	}
	if id := subjectIDFromURL(item.URL); id != "37042229" {
		t.Fatalf("subjectIDFromURL = %q", id)
	}
}

func TestDecodeDoubanDataMissing(t *testing.T) {
	if _, err := decodeDoubanData([]byte("<html></html>")); err == nil {
		t.Fatal("缺少 window.__DATA__ 时应返回错误")
	}
}

func TestLookupResultFormatting(t *testing.T) {
	// 备注：lookup 的网络路径由集成测试覆盖，这里只确保错误文案可截断。
	if got := truncateMessage(fmt.Sprintf("%0300d", 1)); len([]rune(got)) != 201 {
		t.Fatalf("truncateMessage 长度 = %d, want 201", len([]rune(got)))
	}
}
