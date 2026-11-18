package guangyashare

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"litepan/internal/domain"
	"litepan/internal/httpx"
)

const (
	guangyaWebOrigin = "https://www.guangyapan.com"
	guangyaAPIBase   = "https://api.guangyapan.com"

	pathShareSummary     = "/userres/v1/get_share_summary"
	pathShareAccessToken = "/userres/v1/get_share_access_token"
	pathShareFileList    = "/userres/v1/get_share_page_files_list"

	sharePageSize = 100
	// 公开接口做串行限速，避免解析大分享时触发风控。
	shareRequestGap = 70 * time.Millisecond

	maxSharePagesPerDir = 100
)

// 光鸭分享接口的原始响应结构；对外统一转换为 provider.go 里的 shareEntry。
type guangyaEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type shareSummaryData struct {
	ShareID  string `json:"shareId"`
	Title    string `json:"title"`
	NickName string `json:"nickName"`
	NeedCode bool   `json:"needCode"`
}

type guangyaEntry struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	ResType  int    `json:"resType"`
	CTime    int64  `json:"ctime"`
	UTime    int64  `json:"utime"`
	FileType int    `json:"fileType"`
}

type guangyaListData struct {
	List  []guangyaEntry `json:"list"`
	Total int            `json:"total"`
}

type shareClient struct {
	http    *http.Client
	baseURL string
	did     string

	mu          sync.Mutex
	lastRequest time.Time
}

func newShareClient() *shareClient {
	return &shareClient{
		http:    httpx.NewClient(httpx.ClientOptions{Timeout: 30 * time.Second}),
		baseURL: guangyaAPIBase,
		did:     randomHex(16),
	}
}

// shareSummary 获取分享概要（标题、分享者、是否需要提取码）。
func (c *shareClient) shareSummary(ctx context.Context, shareID string) (*shareSummaryData, error) {
	var data shareSummaryData
	if err := c.post(ctx, pathShareSummary, map[string]any{"shareId": shareID}, &data); err != nil {
		return nil, err
	}
	if strings.TrimSpace(data.ShareID) == "" {
		return nil, domain.Errorf(domain.CodeNotFound, "分享不存在或已失效")
	}
	return &data, nil
}

// shareAccessToken 用分享 ID + 提取码换取访问令牌。
func (c *shareClient) shareAccessToken(ctx context.Context, shareID, password string) (string, error) {
	var data struct {
		AccessToken string `json:"accessToken"`
	}
	if err := c.post(ctx, pathShareAccessToken, map[string]any{
		"shareId": shareID,
		"code":    strings.TrimSpace(password),
	}, &data); err != nil {
		return "", err
	}
	token := strings.TrimSpace(data.AccessToken)
	if token == "" {
		return "", domain.Errorf(domain.CodeDriverError, "光鸭未返回分享访问令牌")
	}
	return token, nil
}

// listShareFiles 拉取分享内某个目录的全部文件（自动翻页）。
func (c *shareClient) listShareFiles(ctx context.Context, accessToken, parentID string) ([]guangyaEntry, error) {
	var all []guangyaEntry
	for page := 0; page < maxSharePagesPerDir; page++ {
		var data guangyaListData
		if err := c.post(ctx, pathShareFileList, map[string]any{
			"accessToken": accessToken,
			"parentId":    parentID,
			"page":        page,
			"pageSize":    sharePageSize,
			"orderBy":     0, // 按文件名，保证分页稳定
			"sortType":    0,
		}, &data); err != nil {
			return nil, err
		}
		if len(data.List) == 0 {
			return all, nil
		}
		all = append(all, data.List...)
		if data.Total > 0 && len(all) >= data.Total {
			return all, nil
		}
		if len(data.List) < sharePageSize {
			return all, nil
		}
	}
	return all, nil
}

func (c *shareClient) post(ctx context.Context, path string, body any, out any) error {
	if err := c.wait(ctx); err != nil {
		return err
	}
	req, err := httpx.NewJSONRequest(ctx, http.MethodPost, c.baseURL+path, nil, body)
	if err != nil {
		return domain.Wrap(domain.CodeInternal, err)
	}
	httpx.SetHeaders(req, c.headers())

	resp, data, err := httpx.Execute(c.http, req, httpx.DefaultReadLimit)
	if err != nil {
		return domain.Errorf(domain.CodeDriverError, "光鸭分享接口请求失败：%v", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return domain.Errorf(domain.CodeRateLimited, "光鸭接口请求过于频繁，请稍后重试")
	}
	if resp.StatusCode != http.StatusOK {
		return domain.Errorf(domain.CodeDriverError, "光鸭分享 HTTP %d：%s", resp.StatusCode, httpx.Truncate(data, 300))
	}

	var env guangyaEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return domain.Errorf(domain.CodeDriverError, "光鸭分享返回格式异常：%v", err)
	}
	if env.Code != 0 {
		return mapShareAPIError(env.Code, env.Msg)
	}
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return domain.Errorf(domain.CodeDriverError, "光鸭分享数据解析失败：%v", err)
		}
	}
	return nil
}

func (c *shareClient) headers() map[string]string {
	return map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "zh-CN,zh;q=0.9",
		"Content-Type":    "application/json",
		"did":             c.did,
		"dt":              "4",
		"Origin":          guangyaWebOrigin,
		"Referer":         guangyaWebOrigin + "/",
		"traceparent":     traceparent(),
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36",
	}
}

// wait 串行化公开接口请求并保持最小间隔。
func (c *shareClient) wait(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if remaining := shareRequestGap - time.Since(c.lastRequest); remaining > 0 {
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

func mapShareAPIError(code int, msg string) error {
	msg = strings.TrimSpace(msg)
	switch code {
	case 200, 201:
		return domain.Errorf(domain.CodeNotFound, "分享链接已失效")
	case 202:
		return domain.Errorf(domain.CodeNotFound, "分享链接已过期")
	case 204:
		return domain.Errorf(domain.CodeValidation, "不能转存自己分享的文件")
	case 209:
		return domain.Errorf(domain.CodeValidation, "提取码错误")
	case 210:
		return domain.Errorf(domain.CodeValidation, "分享访问令牌已失效，请重新解析分享链接")
	case 112:
		return domain.Errorf(domain.CodeValidation, "分享链接格式不正确或分享已失效")
	case 157:
		return domain.Errorf(domain.CodeValidation, "网盘空间不足，无法转存")
	case 212:
		return domain.Errorf(domain.CodeValidation, "转存文件数量超出网盘限制")
	case 354:
		return domain.Errorf(domain.CodeRateLimited, "光鸭接口请求过于频繁，请稍后重试")
	}
	if msg == "" {
		msg = "未知错误"
	}
	return domain.Errorf(domain.CodeDriverError, "光鸭分享接口错误(%d)：%s", code, msg)
}

func randomHex(n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(buf)
}

func traceparent() string {
	return "00-" + randomHex(16) + "-" + randomHex(8) + "-01"
}
