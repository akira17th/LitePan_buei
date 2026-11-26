package doubanscore

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode"
)

// 片名清洗与候选匹配：STRM 索引里的标题常带年份、季集标记、{tmdb-xxx} 后缀，
// 直接拿去查豆瓣命中率很低，这里先清洗再按「片名 + 年份 + 媒体类型」打分。

var (
	reTitleYear     = regexp.MustCompile(`[\(（]\s*(19|20)\d{2}\s*[\)）]`)
	reIDTag         = regexp.MustCompile(`(?i)[\[{【]\s*(tmdb|douban)\s*[:=_-]?\s*\d+\s*[\]}】]`)
	reSeasonEpisode = regexp.MustCompile(`(?i)\bS\d{1,2}(?:[ ._-]?E\d{1,4})?\b`)
	reSeasonCN      = regexp.MustCompile(`第\s*[0-9一二三四五六七八九十]+\s*[季部]`)
	reMultiSpace    = regexp.MustCompile(`\s+`)
	reInvisible     = regexp.MustCompile(`[\x{200b}-\x{200f}\x{feff}]`)
)

// cleanDoubanText 去掉豆瓣标题里夹带的不可见字符（如 ‎）并压缩空白。
func cleanDoubanText(raw string) string {
	s := reInvisible.ReplaceAllString(raw, "")
	return strings.TrimSpace(reMultiSpace.ReplaceAllString(s, " "))
}

// splitTitleYear 拆分「醉梦‎ (2026)」这样的搜索标题。
func splitTitleYear(raw string) (string, *int) {
	clean := cleanDoubanText(raw)
	year := (*int)(nil)
	if m := reTitleYear.FindStringSubmatch(clean); m != nil {
		year = atoiPtr(m[0][1:5])
	}
	title := strings.TrimSpace(reTitleYear.ReplaceAllString(clean, ""))
	return title, year
}

// cleanQueryTitle 生成用于查询的片名：去年份、去季集标记、去 {tmdb-xxx} 标签。
func cleanQueryTitle(raw string) string {
	s := cleanDoubanText(raw)
	s = reIDTag.ReplaceAllString(s, " ")
	s = reTitleYear.ReplaceAllString(s, " ")
	s = reSeasonEpisode.ReplaceAllString(s, " ")
	s = reSeasonCN.ReplaceAllString(s, " ")
	return strings.TrimSpace(reMultiSpace.ReplaceAllString(s, " "))
}

// normalizeTitle 只保留文字与数字，用于宽松比较（忽略空格、标点、全半角差异）。
func normalizeTitle(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(cleanDoubanText(raw)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// 候选匹配阈值：低于该分数视为没有可靠匹配。
const matchThreshold = 60.0

func scoreCandidate(candTitle string, candYear *int, isTV bool, hasRating bool, want string, wantYear *int, mediaType string) float64 {
	candKey := normalizeTitle(candTitle)
	wantKey := normalizeTitle(want)
	if candKey == "" || wantKey == "" {
		return -1
	}
	score := 0.0
	switch {
	case candKey == wantKey:
		score += 100
	case strings.Contains(candKey, wantKey) || strings.Contains(wantKey, candKey):
		score += 45
	default:
		return -1
	}
	if wantYear != nil && candYear != nil {
		diff := *wantYear - *candYear
		if diff < 0 {
			diff = -diff
		}
		switch {
		case diff == 0:
			score += 30
		case diff == 1:
			score += 8
		default:
			// 同名但年份差 2 年以上，基本是另一部作品（如《八仙！》vs 2018《八仙》），直接放弃。
			return -1
		}
	}
	switch mediaType {
	case "tv":
		if isTV {
			score += 20
		} else {
			score -= 45
		}
	case "movie":
		if isTV {
			score -= 35
		} else {
			score += 10
		}
	}
	if hasRating {
		score += 2
	}
	return score
}

func pickSearch(hits []searchHit, want string, wantYear *int, mediaType string) *LookupResult {
	var best *LookupResult
	bestScore := 0.0
	for _, hit := range hits {
		score := scoreCandidate(hit.Title, hit.Year, hit.IsTV, hit.Rating != nil, want, wantYear, mediaType)
		if score < matchThreshold || score <= bestScore {
			continue
		}
		bestScore = score
		source := hit.Source
		if source == "" {
			source = "search"
		}
		best = &LookupResult{
			DoubanID:    hit.ID,
			DoubanTitle: hit.Title,
			DoubanYear:  hit.Year,
			Rating:      hit.Rating,
			Votes:       hit.Votes,
			IsTV:        hit.IsTV,
			Source:      source,
		}
	}
	return best
}

func pickSuggest(items []LookupResult, want string, wantYear *int, mediaType string) *LookupResult {
	var best *LookupResult
	bestScore := 0.0
	for _, item := range items {
		score := scoreCandidate(item.DoubanTitle, item.DoubanYear, item.IsTV, false, want, wantYear, mediaType)
		if score < matchThreshold || score <= bestScore {
			continue
		}
		bestScore = score
		cand := item
		best = &cand
	}
	return best
}

// lookupDouban 依次尝试三条来源，返回 (nil, nil) 表示确实没有匹配的豆瓣条目：
//  1. 搜索页：评分 + 「剧集」标记，最准（被风控拦截时会冷却一段时间，直接跳过）；
//  2. 移动端 rexxar 搜索：自带评分，缺类型标记，靠标题 + 年份匹配；
//  3. suggest + 条目详情：先用集数判断电影/剧集，再用详情补评分。
func (s *Service) lookupDouban(ctx context.Context, title string, year *int, mediaType string) (*LookupResult, error) {
	query := cleanQueryTitle(title)
	if query == "" {
		return nil, nil
	}

	attempted, failed := 0, 0
	var errs []error

	if !s.douban.htmlSearchBlocked() {
		attempted++
		hits, err := s.douban.search(ctx, query)
		if err != nil {
			failed++
			errs = append(errs, err)
		} else if best := pickSearch(hits, query, year, mediaType); best != nil {
			return best, nil
		}
	}

	attempted++
	hits, err := s.douban.searchRexxar(ctx, query)
	if err != nil {
		failed++
		errs = append(errs, err)
	} else if best := pickSearch(hits, query, year, mediaType); best != nil {
		return best, nil
	}

	attempted++
	sugs, err := s.douban.suggest(ctx, query)
	if err != nil {
		failed++
		errs = append(errs, err)
	} else if best := pickSuggest(sugs, query, year, mediaType); best != nil {
		// suggest 不带评分，用条目详情补上（同时校正年份与电影/剧集）。
		if detail, derr := s.douban.subjectDetail(ctx, best.DoubanID); derr == nil && detail != nil {
			return detail, nil
		}
		return best, nil
	}

	if attempted > 0 && failed == attempted {
		return nil, errors.Join(errs...)
	}
	return nil, nil
}
