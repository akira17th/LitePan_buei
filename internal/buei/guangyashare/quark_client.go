package guangyashare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"litepan/internal/domain"
	"litepan/internal/httpx"
)

// 夸克公开分享接口：分享页（pan.quark.cn）用 pwd_id + 提取码换 stoken，
// 再按 stoken 列目录。这两个接口匿名可访问，转存才需要账号 Cookie（由驱动完成）。
const (
	quarkWebOrigin  = "https://pan.quark.cn"
	quarkAPIBase    = "https://drive-pc.quark.cn/1/clouddrive"
	quarkUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	quarkAPIVersion = "1"

	pathQuarkShareToken  = "/share/sharepage/token"
	pathQuarkShareDetail = "/share/sharepage/detail"

	quarkSharePageSize = 100
	// 夸克分享页接口风控比光鸭严，串行请求间隔放大一些。
	quarkRequestGap   = 150 * time.Millisecond
	maxQuarkSharePage = 100
)

type quarkEnvelope struct {
	Status   int             `json:"status"`
	Code     int             `json:"code"`
	Message  string          `json:"message"`
	Data     json.RawMessage `json:"data"`
	Metadata json.RawMessage `json:"metadata"`
}

type quarkEntry struct {
	FID        string `json:"fid"`
	FileName   string `json:"file_name"`
	Dir        bool   `json:"dir"`
	Size       int64  `json:"size"`
	UpdatedAt  int64  `json:"updated_at"`
	ShareToken string `json:"share_fid_token"`
}

type quarkShareDetailData struct {
	List  []quarkEntry `json:"list"`
	Share struct {
		Title string `json:"title"`
	} `json:"share"`
}

type quarkShareClient struct {
	http    *http.Client
	baseURL string

	mu          sync.Mutex
	lastRequest time.Time
}

func newQuarkShareClient() *quarkShareClient {
	return &quarkShareClient{
		http:    httpx.NewClient(httpx.ClientOptions{Timeout: 30 * time.Second}),
		baseURL: quarkAPIBase,
	}
}

// shareToken 用分享 ID + 提取码换取访问令牌。
func (c *quarkShareClient) shareToken(ctx context.Context, shareID, password string) (string, error) {
	var data struct {
		Stoken string `json:"stoken"`
	}
	if err := c.do(ctx, http.MethodPost, pathQuarkShareToken, nil, map[string]any{
		"pwd_id":   strings.TrimSpace(shareID),
		"passcode": strings.TrimSpace(password),
	}, &data); err != nil {
		return "", err
	}
	token := strings.TrimSpace(data.Stoken)
	if token == "" {
		return "", domain.Errorf(domain.CodeDriverError, "夸克未返回分享访问令牌")
	}
	return token, nil
}

// listShareFiles 拉取分享内某个目录的全部内容（自动翻页），同时返回分享标题。
func (c *quarkShareClient) listShareFiles(ctx context.Context, token, shareID, parentID string) ([]shareEntry, string, error) {
	fid := strings.TrimSpace(parentID)
	if fid == "" {
		fid = "0"
	}
	var (
		all   []shareEntry
		title string
	)
	for page := 1; page <= maxQuarkSharePage; page++ {
		query := url.Values{
			"pwd_id":        {strings.TrimSpace(shareID)},
			"stoken":        {token},
			"pdir_fid":      {fid},
			"force":         {"0"},
			"_page":         {strconv.Itoa(page)},
			"_size":         {strconv.Itoa(quarkSharePageSize)},
			"_fetch_banner": {"0"},
			"_fetch_share":  {"1"},
			"_fetch_total":  {"1"},
			"_sort":         {"file_type:asc,updated_at:desc"},
		}
		var data quarkShareDetailData
		if err := c.do(ctx, http.MethodGet, pathQuarkShareDetail, query, nil, &data); err != nil {
			return nil, "", err
		}
		if title == "" {
			title = strings.TrimSpace(data.Share.Title)
		}
		for _, entry := range data.List {
			all = append(all, toQuarkShareEntry(entry))
		}
		if len(data.List) < quarkSharePageSize {
			break
		}
	}
	return all, title, nil
}

func toQuarkShareEntry(entry quarkEntry) shareEntry {
	name := strings.TrimSpace(entry.FileName)
	if name == "" {
		name = entry.FID
	}
	modTime := time.Time{}
	if entry.UpdatedAt > 0 {
		modTime = time.UnixMilli(entry.UpdatedAt)
	}
	return shareEntry{
		ID:      strings.TrimSpace(entry.FID),
		Name:    name,
		Size:    entry.Size,
		IsDir:   entry.Dir,
		ModTime: modTime,
		Token:   strings.TrimSpace(entry.ShareToken),
	}
}

func (c *quarkShareClient) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if err := c.wait(ctx); err != nil {
		return err
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("pr", "ucpro")
	query.Set("fr", "pc")
	query.Set("api_ver", quarkAPIVersion)

	req, err := httpx.NewJSONRequest(ctx, method, c.baseURL+path, query, body)
	if err != nil {
		return domain.Wrap(domain.CodeInternal, err)
	}
	httpx.SetHeaders(req, map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "zh-CN,zh;q=0.9",
		"Origin":          quarkWebOrigin,
		"Referer":         quarkWebOrigin + "/",
		"User-Agent":      quarkUserAgent,
	})

	resp, data, err := httpx.Execute(c.http, req, httpx.DefaultReadLimit)
	if err != nil {
		return domain.Errorf(domain.CodeDriverError, "夸克分享接口请求失败：%v", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return domain.Errorf(domain.CodeRateLimited, "夸克接口请求过于频繁，请稍后重试")
	}

	var env quarkEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		if resp.StatusCode >= 400 {
			return domain.Errorf(domain.CodeDriverError, "夸克分享 HTTP %d：%s", resp.StatusCode, httpx.Truncate(data, 300))
		}
		return domain.Errorf(domain.CodeDriverError, "夸克分享返回格式异常：%v", err)
	}
	if resp.StatusCode >= 400 || env.Code != 0 || env.Status >= 400 {
		return mapQuarkShareError(resp.StatusCode, env.Status, env.Code, env.Message)
	}
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return domain.Errorf(domain.CodeDriverError, "夸克分享数据解析失败：%v", err)
		}
	}
	return nil
}

// wait 串行化公开接口请求并保持最小间隔。
func (c *quarkShareClient) wait(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if remaining := quarkRequestGap - time.Since(c.lastRequest); remaining > 0 {
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	c.lastRequest = time.Now()
	return nil
}

func mapQuarkShareError(httpStatus, status, code int, msg string) error {
	msg = strings.TrimSpace(msg)
	text := strings.ToLower(msg)
	switch {
	case httpStatus == http.StatusTooManyRequests || status == http.StatusTooManyRequests ||
		strings.Contains(msg, "频繁") || strings.Contains(msg, "限流") || strings.Contains(text, "too many"):
		return domain.Errorf(domain.CodeRateLimited, "夸克接口请求过于频繁，请稍后重试")
	case code == 41010 || strings.Contains(msg, "提取码") || strings.Contains(text, "passcode"):
		return domain.Errorf(domain.CodeValidation, "提取码错误")
	case strings.Contains(text, "stoken") || strings.Contains(msg, "访问令牌"):
		return domain.Errorf(domain.CodeValidation, "分享访问令牌已失效，请重新解析分享链接")
	case code == 41004 || code == 41006 || code == 41008 || code == 41014 || code == 41016 ||
		code == 14001 || code == 14004 ||
		strings.Contains(msg, "分享不存在") || strings.Contains(msg, "分享已失效") ||
		strings.Contains(msg, "分享已过期") || strings.Contains(msg, "已取消分享"):
		return domain.Errorf(domain.CodeNotFound, "分享不存在或已失效")
	case httpStatus == http.StatusUnauthorized || httpStatus == http.StatusForbidden ||
		status == http.StatusUnauthorized || status == http.StatusForbidden ||
		code == 31001 || code == 31003:
		return domain.Errorf(domain.CodePermissionDenied, "夸克分享需要登录后访问，请稍后重试")
	}
	if msg == "" {
		msg = "未知错误"
	}
	if code == 0 {
		code = status
	}
	return domain.Errorf(domain.CodeDriverError, "夸克分享接口错误(%d)：%s", code, msg)
}

// isQuarkPasscodeError 判断错误是否由提取码引起。
func isQuarkPasscodeError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "提取码")
}
