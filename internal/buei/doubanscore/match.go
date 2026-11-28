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
	// reSeasonTail 匹配候选标题末尾的季数信息（第一季 / Season 1 / 第2部 / 罗马数字 / 裸数字）。
	reSeasonTail = regexp.MustCompile(`(?i)(第\s*[0-9一二三四五六七八九十]+\s*[季部]|season\s*\d+|[IVXivx]+\s*季|\s\d{1,2})$`)
)

// stripSeasonSuffix 去掉标题末尾的季数标记：「一人之下 第一季」→「一人之下」。
// 豆瓣把多季拆成独立条目，STRM 索引里的整部作品要靠它对上第一季。
func stripSeasonSuffix(title string) string {
	prev := ""
	for prev != title {
		prev = title
		title = strings.TrimSpace(reSeasonTail.ReplaceAllString(title, ""))
	}
	return title
}

// isSeasonEntryTitle 判断豆瓣标题是否是「XXX 第N季 / Season N / XXX 2」这类分季条目。
// 在原始文本上剥离（normalizeTitle 会吃掉空格，裸数字规则依赖空格边界），再宽松比较。
func isSeasonEntryTitle(title string) bool {
	stripped := stripSeasonSuffix(title)
	if stripped == title {
		return false
	}
	return normalizeTitle(stripped) != normalizeTitle(title)
}

// seasonOnlyDiff 判断两个互相包含的标题的差集是否只是季数信息。
// 「一人之下 第一季」vs「一人之下」：差集「第一季」→ true，视为同一部作品。
func seasonOnlyDiff(a, b string) bool {
	long, short := a, b
	if len(short) > len(long) {
		long, short = short, long
	}
	if !strings.HasPrefix(long, short) {
		return false
	}
	return stripSeasonSuffix(strings.TrimSpace(long[len(short):])) == ""
}

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

func scoreCandidate(candTitle string, candYear *int, isTV, hasRating, isSeasonEntry bool, want string, wantYear *int, mediaType string) float64 {
	candKey := normalizeTitle(candTitle)
	wantKey := normalizeTitle(want)
	if candKey == "" || wantKey == "" {
		return -1
	}
	score := 0.0
	switch {
	case candKey == wantKey:
		score += 100
	case seasonOnlyDiff(candKey, wantKey):
		// 差集只是「第X季 / Season N」：就是这部作品（豆瓣分季、索引整部），按接近全等打分。
		score += 85
	case strings.Contains(candKey, wantKey) || strings.Contains(wantKey, candKey):
		score += 45
	default:
		return -1
	}
	// 季数条目（同一部作品的不同季）不参与年份打分：索引的整部年份是首播年，
	// 与最新季年份必然不同，按年份加减分会让首播季永远压住最新季，
	// 「整部剧取最新一季评分作代表」就失效了。年份防混淆只对非季条目生效。
	if wantYear != nil && candYear != nil && !isSeasonEntry {
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
	// bestSeasonYear 只在「多季条目互相打平」时参与比较：取年份更新的那一季。
	// 豆瓣把多季拆成独立条目（标题季数后缀已被 seasonOnlyDiff 抹平），同部作品的各季
	// 打分完全相同；STRM 索引的整部剧集取最新一季的评分作代表（最贴近当前播出状态）。
	// 电影之间天然不会出现季数条目，该决胜规则不影响电影匹配。
	var bestSeasonYear int
	for _, hit := range hits {
		score := scoreCandidate(hit.Title, hit.Year, hit.IsTV, hit.Rating != nil, hit.IsSeasonEntry, want, wantYear, mediaType)
		// 低于阈值直接淘汰；分数低于当前最优也淘汰（严格更高才替换，并列走下面决胜）。
		if score < matchThreshold || score < bestScore {
			continue
		}
		seasonYear := 0
		if hit.Year != nil {
			seasonYear = *hit.Year
		}
		if score == bestScore && best != nil {
			// 并列：只有两边都是季数条目才按「更新的季」决胜，其余维持先到者。
			// 代表评分要可用：hasRating 在 scoreCandidate 里有 +2，没评分的季分数
			// 严格更低，根本进不了这里——并列只发生在「都有/都没有评分」的季之间。
			if !best.isSeason || !hit.IsSeasonEntry || bestSeasonYear >= seasonYear {
				continue
			}
		}
		bestScore = score
		bestSeasonYear = seasonYear
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
		best.isSeason = hit.IsSeasonEntry
	}
	return best
}

func pickSuggest(items []LookupResult, want string, wantYear *int, mediaType string) *LookupResult {
	var best *LookupResult
	bestScore := 0.0
	for _, item := range items {
		score := scoreCandidate(item.DoubanTitle, item.DoubanYear, item.IsTV, false, false, want, wantYear, mediaType)
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
		s.devf("  清洗后片名为空，跳过查询")
		return nil, nil
	}

	attempted, failed := 0, 0
	var errs []error

	if !s.douban.htmlSearchBlocked() {
		attempted++
		hits, err := s.douban.search(ctx, query)
		if err != nil {
			s.devf("  search 失败：%v", err)
			failed++
			errs = append(errs, err)
		} else {
			s.devHits("search", hits, query, year, mediaType)
			if best := pickSearch(hits, query, year, mediaType); best != nil {
				return best, nil
			}
		}
	} else {
		s.devf("  search 处于风控冷却期，直接跳过")
	}

	attempted++
	hits, err := s.douban.searchRexxar(ctx, query)
	if err != nil {
		s.devf("  rexxar 失败：%v", err)
		failed++
		errs = append(errs, err)
	} else {
		s.devHits("rexxar", hits, query, year, mediaType)
		if best := pickSearch(hits, query, year, mediaType); best != nil {
			return best, nil
		}
	}

	attempted++
	sugs, err := s.douban.suggest(ctx, query)
	if err != nil {
		s.devf("  suggest 失败：%v", err)
		failed++
		errs = append(errs, err)
	} else {
		s.devSuggestHits(sugs, query, year, mediaType)
		if best := pickSuggest(sugs, query, year, mediaType); best != nil {
			// suggest 不带评分，用条目详情补上（同时校正年份与电影/剧集）。
			s.devf("  suggest 命中，读取豆瓣条目详情 %s 补评分", best.DoubanID)
			if detail, derr := s.douban.subjectDetail(ctx, best.DoubanID); derr == nil && detail != nil {
				return detail, nil
			}
			return best, nil
		}
	}

	if attempted > 0 && failed == attempted {
		return nil, errors.Join(errs...)
	}
	return nil, nil
}
