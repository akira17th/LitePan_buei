package doubanscore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// douban.go 是豆瓣公开数据的最小抓取客户端。三条来源，按可靠性依次回退：
//  1. 搜索页 search.douban.com/movie/subject_search：评分 + 「剧集」标记都在，
//     但匿名访问容易被风控跳转到 sec.douban.com；
//  2. 移动端 rexxar 接口 m.douban.com/rexxar/api/v2/search/subjects：自带评分，
//     只缺电影/剧集标记（用标题 + 年份兜底）；
//  3. j/subject_suggest + rexxar 条目详情：只有 ID / 年份，靠详情补评分。
//
// 这些接口的字段含义见仓库里的 .agents/skills/douban-cli/。

const (
	doubanSearchBase = "https://search.douban.com"
	doubanMovieBase  = "https://movie.douban.com"
	doubanMobileBase = "https://m.douban.com"

	doubanUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	doubanMobileUA  = "Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.0 Mobile/15E148 Safari/604.1"

	doubanMaxBody     = 4 << 20
	doubanRetries     = 3
	doubanMinInterval = 900 * time.Millisecond
	doubanTimeout     = 30 * time.Second

	// 豆瓣接口分类码：图书 1001、影视 1002、音乐 1003。
	doubanMovieCat = "1002"

	// 被风控拦截后，一段时间内不再碰搜索页，省掉每个条目一次白跑。
	doubanChallengeCooldown = 30 * time.Minute
)

// LookupResult 是一次成功的豆瓣匹配结果。
type LookupResult struct {
	DoubanID    string
	DoubanTitle string
	DoubanYear  *int
	Rating      *float64
	Votes       int
	IsTV        bool
	// Source 记录命中来源：search / rexxar / suggest / detail。
	Source string

	// isSeason 内部决胜标记：命中条目是「第N季」分季条目（不导出、不展示）。
	isSeason bool
}

// errDoubanChallenge 表示搜索页被 302 到 sec.douban.com 的人机校验页。
var errDoubanChallenge = errors.New("豆瓣搜索页被风控校验拦截")

// DescribeLookupError 把查询失败的根因转成给用户看的一句话：
// 未命中任何候选与「接口都被拦」必须区分开，否则风控会被误报成「豆瓣无此条目」。
func DescribeLookupError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "查询失败"
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(msg, errDoubanChallenge.Error()):
		return "豆瓣搜索被风控拦截，请稍后重试"
	case strings.Contains(lower, "sec.douban.com") || strings.Contains(lower, "challenge"):
		return "豆瓣搜索被风控拦截，请稍后重试"
	case strings.Contains(msg, "解析豆瓣"):
		return "豆瓣接口返回异常，可能被风控拦截，请稍后重试"
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate"):
		return "豆瓣接口限流，请稍后重试"
	case strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "client timeout"):
		return "豆瓣请求超时，请稍后重试"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "network is unreachable") || strings.Contains(lower, "connection reset"):
		return "网络连接失败，请检查网络后重试"
	default:
		return "查询失败：" + truncateDesc(msg)
	}
}

// truncateDesc 截断过长的底层错误信息。
func truncateDesc(msg string) string {
	runes := []rune(msg)
	if len(runes) > 120 {
		return string(runes[:120]) + "…"
	}
	return msg
}

type doubanClient struct {
	http     *http.Client
	ua       string
	mobileUA string
	interval time.Duration

	searchBase string
	movieBase  string
	mobileBase string

	mu                 sync.Mutex
	last               time.Time
	searchBlockedUntil time.Time

	// buei:devlog —— 开发环境的 HTTP 调试输出（nil 表示不记录）。
	debugf func(format string, args ...any)
}

func newDoubanClient(interval time.Duration) *doubanClient {
	if interval <= 0 {
		interval = doubanMinInterval
	}
	return &doubanClient{
		http:       &http.Client{Timeout: doubanTimeout},
		ua:         doubanUserAgent,
		mobileUA:   doubanMobileUA,
		interval:   interval,
		searchBase: doubanSearchBase,
		movieBase:  doubanMovieBase,
		mobileBase: doubanMobileBase,
	}
}

func (c *doubanClient) pace(ctx context.Context) error {
	c.mu.Lock()
	wait := c.interval - time.Since(c.last)
	c.last = time.Now()
	c.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *doubanClient) htmlSearchBlocked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Before(c.searchBlockedUntil)
}

func (c *doubanClient) blockHTMLSearch() {
	c.mu.Lock()
	c.searchBlockedUntil = time.Now().Add(doubanChallengeCooldown)
	c.mu.Unlock()
}

type doubanHeaders struct {
	userAgent string
	referer   string
}

func (c *doubanClient) get(ctx context.Context, rawURL string, hdr doubanHeaders) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= doubanRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 800 * time.Millisecond
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		body, retry, err := c.do(ctx, rawURL, hdr)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *doubanClient) do(ctx context.Context, rawURL string, hdr doubanHeaders) ([]byte, bool, error) {
	if err := c.pace(ctx); err != nil {
		return nil, false, err
	}
	ua := strings.TrimSpace(hdr.userAgent)
	if ua == "" {
		ua = c.ua
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if hdr.referer != "" {
		req.Header.Set("Referer", hdr.referer)
	}

	if c.debugf != nil {
		c.debugf("  HTTP GET %s", rawURL)
	}
	started := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		if c.debugf != nil {
			c.debugf("  HTTP 失败（%s）：%v", time.Since(started).Round(time.Millisecond), err)
		}
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if c.debugf != nil {
		host := ""
		if resp.Request != nil && resp.Request.URL != nil {
			host = resp.Request.URL.Host
		}
		c.debugf("  HTTP %d %s（%s）", resp.StatusCode, host, time.Since(started).Round(time.Millisecond))
	}

	// 搜索页被风控时会 302 到 sec.douban.com 的验证页（跟着跳转后是 200 HTML）。
	if resp.Request != nil && resp.Request.URL != nil && strings.Contains(resp.Request.URL.Host, "sec.douban.com") {
		return nil, false, errDoubanChallenge
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("豆瓣返回 http %d", resp.StatusCode)
	case resp.StatusCode == http.StatusForbidden:
		return nil, false, fmt.Errorf("豆瓣返回 403（风控或需要登录）")
	case resp.StatusCode != http.StatusOK:
		return nil, false, fmt.Errorf("豆瓣返回 http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, doubanMaxBody))
	if err != nil {
		return nil, true, err
	}
	return body, false, nil
}

// --- 搜索页（评分 + 剧集标记） ---

type doubanSearchItem struct {
	TplName  string `json:"tpl_name"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Abstract string `json:"abstract"`
	Rating   struct {
		Value float64 `json:"value"`
		Count int     `json:"count"`
	} `json:"rating"`
	Labels []struct {
		Text string `json:"text"`
	} `json:"labels"`
	MoreURL string `json:"more_url"`
}

type doubanSearchData struct {
	Items []doubanSearchItem `json:"items"`
}

// searchHit 是各来源统一后的候选条目。
type searchHit struct {
	ID      string
	Title   string
	Year    *int
	Rating  *float64
	Votes   int
	IsTV    bool
	RawName string
	// IsSeasonEntry 标记这是「XXX 第N季 / Season N」的分季条目。
	IsSeasonEntry bool
	// Source 标记候选来源：search（搜索页）/ rexxar（移动端搜索）。
	Source string
}

func (c *doubanClient) search(ctx context.Context, query string) ([]searchHit, error) {
	if c.htmlSearchBlocked() {
		return nil, errDoubanChallenge
	}
	u := fmt.Sprintf("%s/movie/subject_search?search_text=%s&cat=%s",
		c.searchBase, url.QueryEscape(query), doubanMovieCat)
	body, err := c.get(ctx, u, doubanHeaders{})
	if err != nil {
		if errors.Is(err, errDoubanChallenge) {
			c.blockHTMLSearch()
		}
		return nil, err
	}
	data, err := decodeDoubanData(body)
	if err != nil {
		// 抓不到 window.__DATA__ 基本也是风控页，直接冷却一段时间。
		c.blockHTMLSearch()
		return nil, err
	}
	out := make([]searchHit, 0, len(data.Items))
	for _, item := range data.Items {
		if strings.TrimSpace(item.TplName) != "search_subject" {
			continue
		}
		id := subjectIDFromURL(item.URL)
		if id == "" {
			continue
		}
		title, year := splitTitleYear(item.Title)
		hit := searchHit{
			ID:      id,
			Title:   title,
			Year:    year,
			Votes:   item.Rating.Count,
			IsTV:    labelsContainTV(item),
			RawName: item.Title,
			// 标题带季数后缀 → 多季条目里的某一季。
			IsSeasonEntry: isSeasonEntryTitle(title),
			Source:        "search",
		}
		if item.Rating.Count > 0 && item.Rating.Value > 0 {
			v := item.Rating.Value
			hit.Rating = &v
		}
		out = append(out, hit)
	}
	return out, nil
}

// decodeDoubanData 从搜索页 HTML 里取出 window.__DATA__ 的 JSON 对象。
func decodeDoubanData(body []byte) (doubanSearchData, error) {
	var data doubanSearchData
	html := string(body)
	marker := strings.Index(html, "window.__DATA__")
	if marker < 0 {
		return data, errors.New("豆瓣搜索页结构变化：没找到 window.__DATA__")
	}
	start := strings.IndexAny(html[marker:], "{")
	if start < 0 {
		return data, errors.New("豆瓣搜索页结构变化：window.__DATA__ 不是对象")
	}
	dec := json.NewDecoder(strings.NewReader(html[marker+start:]))
	if err := dec.Decode(&data); err != nil {
		return data, fmt.Errorf("解析豆瓣搜索数据失败: %w", err)
	}
	return data, nil
}

func labelsContainTV(item doubanSearchItem) bool {
	for _, l := range item.Labels {
		if strings.Contains(l.Text, "剧集") {
			return true
		}
	}
	return strings.Contains(item.MoreURL, "is_tv:'1'")
}

// --- 移动端 rexxar 搜索（带评分） ---

type doubanRexxarSearch struct {
	Subjects struct {
		Items []struct {
			Target struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Year   string `json:"year"`
				URI    string `json:"uri"`
				Rating struct {
					Value float64 `json:"value"`
					Count int     `json:"count"`
				} `json:"rating"`
			} `json:"target"`
		} `json:"items"`
	} `json:"subjects"`
}

func (c *doubanClient) searchRexxar(ctx context.Context, query string) ([]searchHit, error) {
	u := fmt.Sprintf("%s/rexxar/api/v2/search/subjects?q=%s&type=movie",
		c.mobileBase, url.QueryEscape(query))
	body, err := c.get(ctx, u, doubanHeaders{
		userAgent: c.mobileUA,
		referer:   c.mobileBase + "/search/",
	})
	if err != nil {
		return nil, err
	}
	return parseRexxarSearch(body)
}

func parseRexxarSearch(body []byte) ([]searchHit, error) {
	var data doubanRexxarSearch
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("解析豆瓣移动端搜索失败: %w", err)
	}
	out := make([]searchHit, 0, len(data.Subjects.Items))
	for _, item := range data.Subjects.Items {
		target := item.Target
		id := strings.TrimSpace(target.ID)
		if id == "" {
			continue
		}
		seasonTitle := cleanDoubanText(target.Title)
		hit := searchHit{
			ID:      id,
			Title:   seasonTitle,
			Year:    atoiPtr(target.Year),
			Votes:   target.Rating.Count,
			RawName: target.Title,
			Source:  "rexxar",
			// 移动端搜索结果没有电影/剧集字段，但 URI 里有：douban://douban.com/tv/{id}。
			IsTV:          strings.Contains(target.URI, "/tv/"),
			IsSeasonEntry: isSeasonEntryTitle(seasonTitle),
		}
		if target.Rating.Count > 0 && target.Rating.Value > 0 {
			v := target.Rating.Value
			hit.Rating = &v
		}
		out = append(out, hit)
	}
	return out, nil
}

// --- 自动补全（ID / 年份 / 集数） ---

type doubanSuggestItem struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	SubTitle string `json:"sub_title"`
	Year     string `json:"year"`
	Episode  string `json:"episode"`
	URL      string `json:"url"`
}

func (c *doubanClient) suggest(ctx context.Context, query string) ([]LookupResult, error) {
	u := fmt.Sprintf("%s/j/subject_suggest?q=%s", c.movieBase, url.QueryEscape(query))
	body, err := c.get(ctx, u, doubanHeaders{})
	if err != nil {
		return nil, err
	}
	var items []doubanSuggestItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("解析豆瓣 suggest 失败: %w", err)
	}
	out := make([]LookupResult, 0, len(items))
	for _, item := range items {
		// 影人（celebrity）不是作品条目，直接跳过，避免同名影人抢走匹配。
		if item.Type == "celebrity" || strings.Contains(item.URL, "/celebrity/") {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = subjectIDFromURL(item.URL)
		}
		if id == "" {
			continue
		}
		out = append(out, LookupResult{
			DoubanID:    id,
			DoubanTitle: cleanDoubanText(item.Title),
			DoubanYear:  atoiPtr(item.Year),
			IsTV:        strings.TrimSpace(item.Episode) != "",
			Source:      "suggest",
		})
	}
	return out, nil
}

// --- 条目详情（可靠的评分来源） ---

type doubanRexxarDetail struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Year          string `json:"year"`
	Type          string `json:"type"`
	IsTV          bool   `json:"is_tv"`
	EpisodesCount int    `json:"episodes_count"`
	Rating        struct {
		Value float64 `json:"value"`
		Count int     `json:"count"`
	} `json:"rating"`
}

// subjectDetail 读取单条豆瓣详情：评分、年份、电影/剧集类型都以它为准。
// 剧集会被 301 到 /rexxar/api/v2/tv/{id}，http.Client 自动跟随。
func (c *doubanClient) subjectDetail(ctx context.Context, id string) (*LookupResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("豆瓣条目 ID 为空")
	}
	u := fmt.Sprintf("%s/rexxar/api/v2/movie/%s", c.mobileBase, id)
	body, err := c.get(ctx, u, doubanHeaders{
		userAgent: c.mobileUA,
		referer:   c.mobileBase + "/movie/subject/" + id + "/",
	})
	if err != nil {
		return nil, err
	}
	return parseRexxarDetail(id, body)
}

func parseRexxarDetail(id string, body []byte) (*LookupResult, error) {
	var data doubanRexxarDetail
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("解析豆瓣条目详情失败: %w", err)
	}
	title := cleanDoubanText(data.Title)
	if title == "" {
		return nil, errors.New("豆瓣条目详情缺少标题")
	}
	detailID := strings.TrimSpace(data.ID)
	if detailID == "" {
		detailID = id
	}
	res := &LookupResult{
		DoubanID:    detailID,
		DoubanTitle: title,
		DoubanYear:  atoiPtr(data.Year),
		Votes:       data.Rating.Count,
		IsTV:        data.IsTV || data.Type == "tv" || data.EpisodesCount > 0,
		Source:      "detail",
	}
	if data.Rating.Count > 0 && data.Rating.Value > 0 {
		v := data.Rating.Value
		res.Rating = &v
	}
	return res, nil
}

func subjectIDFromURL(raw string) string {
	const marker = "/subject/"
	i := strings.Index(raw, marker)
	if i < 0 {
		return ""
	}
	rest := raw[i+len(marker):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	return rest[:end]
}

func atoiPtr(raw string) *int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}
