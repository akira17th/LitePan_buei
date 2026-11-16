package guangyashare

import (
	"net/url"
	"regexp"
	"strings"

	"litepan/internal/domain"
)

// 光鸭分享链接形如：
//
//	https://www.guangyapan.com/s/1969341399784386635_al18HJA7s7gOUCjJ
//
// 其中路径最后一段是完整 shareId（数字 ID + 下划线 + 用户标识）。部分分享会带提取码，
// 可能出现在链接 query（?code=xxxx）或分享口令文本中（提取码：xxxx）。

var (
	shareURLCandidate = regexp.MustCompile(`(?i)https?://[^\s<>"']*guangyapan\.com[^\s<>"']*`)
	shareIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{6,128}$`)
	shareIDInText     = regexp.MustCompile(`(?i)(?:/s/|/share/|/link/|/download/)([A-Za-z0-9_-]{6,128})`)
	passwordPatterns  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)[(（]?\s*(?:提取码|访问码|验证码|密码|口令)\s*[:：]\s*([A-Za-z0-9]{4,12})\s*[)）]?`),
		regexp.MustCompile(`(?i)\b(?:code|pwd|passcode|password)\s*[:=]\s*([A-Za-z0-9]{4,12})\b`),
	}
)

// LinkInfo 是从用户输入中解析出的分享链接信息。
type LinkInfo struct {
	ShareID string
	// Password 是链接或口令文本里自带的提取码，可能为空。
	Password string
	// URL 是识别出的分享链接，可能为空（用户只粘贴了分享 ID）。
	URL string
}

// ParseShareLink 从分享链接/分享口令中提取 shareId 与提取码。
func ParseShareLink(input string) (LinkInfo, error) {
	text := strings.TrimSpace(input)
	if text == "" {
		return LinkInfo{}, domain.Errorf(domain.CodeValidation, "请粘贴光鸭分享链接")
	}

	info := LinkInfo{}
	rawURL := ""
	if match := shareURLCandidate.FindString(text); match != "" {
		rawURL = strings.TrimRight(match, ".,;，。；、）)】]")
	} else if strings.HasPrefix(strings.ToLower(text), "http://") || strings.HasPrefix(strings.ToLower(text), "https://") {
		rawURL = strings.Fields(text)[0]
	}
	info.URL = rawURL

	if rawURL != "" {
		if parsed, err := url.Parse(rawURL); err == nil {
			info.ShareID = shareIDFromURL(parsed)
			if info.Password == "" {
				info.Password = passwordFromQuery(parsed.Query())
			}
			if info.ShareID == "" && parsed.Fragment != "" {
				if fragment, ferr := url.Parse(parsed.Fragment); ferr == nil {
					if info.ShareID == "" {
						info.ShareID = shareIDFromPath(fragment.Path)
					}
					if info.Password == "" {
						info.Password = passwordFromQuery(fragment.Query())
					}
				}
			}
		}
	}

	if info.ShareID == "" {
		if match := shareIDInText.FindStringSubmatch(text); len(match) > 1 {
			info.ShareID = strings.TrimSpace(match[1])
		}
	}
	if info.ShareID == "" {
		// 兼容用户只粘贴分享 ID 的情况；避免把长文本里的普通英文单词误判为 ID。
		fields := strings.FieldsFunc(text, func(r rune) bool {
			return r == ' ' || r == '\n' || r == '\t' || r == '\r' || r == '，' || r == ',' || r == '；' || r == ';'
		})
		if len(fields) <= 2 {
			for _, field := range fields {
				candidate := strings.Trim(field, "\"'()（）[]【】<>《》")
				if shareIDPattern.MatchString(candidate) && strings.ContainsAny(candidate, "_-") {
					info.ShareID = candidate
					break
				}
			}
		}
	}

	if info.Password == "" {
		for _, pattern := range passwordPatterns {
			if match := pattern.FindStringSubmatch(text); len(match) > 1 {
				info.Password = strings.TrimSpace(match[1])
				break
			}
		}
	}

	if info.ShareID == "" || !shareIDPattern.MatchString(info.ShareID) {
		return LinkInfo{}, domain.Errorf(domain.CodeValidation, "未能识别光鸭分享链接，请粘贴完整分享链接")
	}
	return info, nil
}

func shareIDFromURL(parsed *url.URL) string {
	if id := shareIDFromQuery(parsed.Query()); id != "" {
		return id
	}
	return shareIDFromPath(parsed.Path)
}

func shareIDFromQuery(query url.Values) string {
	for _, key := range []string{"shareId", "share_id", "sid", "id"} {
		if value := strings.TrimSpace(query.Get(key)); value != "" && shareIDPattern.MatchString(value) {
			return value
		}
	}
	return ""
}

func shareIDFromPath(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		segment := strings.TrimSpace(segments[i])
		if segment == "" {
			continue
		}
		if shareIDPattern.MatchString(segment) {
			return segment
		}
	}
	return ""
}

func passwordFromQuery(query url.Values) string {
	for _, key := range []string{"code", "pwd", "passcode", "accessCode", "password", "extractCode"} {
		if value := strings.TrimSpace(query.Get(key)); value != "" {
			return value
		}
	}
	return ""
}
