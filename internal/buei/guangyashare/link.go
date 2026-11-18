package guangyashare

import (
	"net/url"
	"regexp"
	"strings"

	"litepan/internal/domain"
)

// 目前支持两种云盘分享链接：
//
//	光鸭：https://www.guangyapan.com/s/1969341399784386635_al18HJA7s7gOUCjJ
//	夸克：https://pan.quark.cn/s/0e1b2f7f3cd4
//
// 分享 ID 一般位于路径最后一段，也可能出现在 query（?shareId=）或 fragment
// （#/list/share/<id>）中。提取码可能出现在链接 query（?code= / ?pwd=）或分享口令
// 文本里（提取码：xxxx）。

var (
	shareURLCandidate = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
	shareIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{6,128}$`)
	// 夸克分享 ID 是纯十六进制；粘贴纯 ID（没有链接）时据此判断所属网盘。
	quarkBareIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8,64}$`)
	shareIDInText      = regexp.MustCompile(`(?i)(?:/s/|/share/|/link/|/download/)([A-Za-z0-9_-]{6,128})`)
	passwordPatterns   = []*regexp.Regexp{
		regexp.MustCompile(`(?i)[(（]?\s*(?:提取码|访问码|验证码|密码|口令)\s*[:：]\s*([A-Za-z0-9]{4,12})\s*[)）]?`),
		regexp.MustCompile(`(?i)\b(?:code|pwd|passcode|password|access_code|extract_code)\s*[:=]\s*([A-Za-z0-9]{4,12})\b`),
	}
)

// reservedShareSegments 是分享链接路径里不可能是分享 ID 的固定段。
var reservedShareSegments = map[string]struct{}{
	"s": {}, "share": {}, "shares": {}, "link": {}, "links": {}, "list": {},
	"download": {}, "file": {}, "folder": {}, "drive": {}, "my": {}, "home": {},
	"web": {}, "index": {}, "m": {}, "p": {}, "recent": {}, "all": {},
}

// LinkInfo 是从用户输入中解析出的分享链接信息。
type LinkInfo struct {
	// Provider 是识别出的网盘标识（guangya / quark），纯 ID 输入时可能为空。
	Provider string
	ShareID  string
	// Password 是链接或口令文本里自带的提取码，可能为空。
	Password string
	// URL 是识别出的分享链接，可能为空（用户只粘贴了分享 ID）。
	URL string
}

// ParseShareLink 从分享链接/分享口令中提取网盘、shareId 与提取码。
func ParseShareLink(input string) (LinkInfo, error) {
	return parseShareLink(input, "")
}

// parseShareLink 解析分享链接；输入里没有可识别域名时（例如只粘贴了分享 ID），
// 使用 fallbackProvider 作为所属网盘。
func parseShareLink(input, fallbackProvider string) (LinkInfo, error) {
	text := strings.TrimSpace(input)
	if text == "" {
		return LinkInfo{}, domain.Errorf(domain.CodeValidation, "请粘贴云盘分享链接")
	}

	info := LinkInfo{}
	rawURL := ""
	// 优先取能识别出网盘域名的链接，避免口令文本里夹带其它网址时误判。
	candidates := shareURLCandidate.FindAllString(text, -1)
	for _, match := range candidates {
		candidate := strings.TrimRight(match, ".,;，。；、）)】]")
		if parsed, err := url.Parse(candidate); err == nil && providerFromHost(parsed.Hostname()) != "" {
			rawURL = candidate
			break
		}
	}
	if rawURL == "" && len(candidates) > 0 {
		rawURL = strings.TrimRight(candidates[0], ".,;，。；、）)】]")
	} else if rawURL == "" && hasURLPrefix(text) {
		rawURL = strings.Fields(text)[0]
	}
	info.URL = rawURL

	if rawURL != "" {
		if parsed, err := url.Parse(rawURL); err == nil {
			info.Provider = providerFromHost(parsed.Hostname())
			info.ShareID = shareIDFromURL(parsed)
			info.Password = passwordFromQuery(parsed.Query())
			if parsed.Fragment != "" {
				if fragment, ferr := url.Parse(parsed.Fragment); ferr == nil {
					if info.ShareID == "" {
						info.ShareID = shareIDFromPath(fragment.Path)
					}
					if info.Password == "" {
						info.Password = passwordFromQuery(fragment.Query())
					}
				} else if info.ShareID == "" {
					if match := shareIDInText.FindStringSubmatch(parsed.Fragment); len(match) > 1 {
						info.ShareID = strings.TrimSpace(match[1])
					}
				}
			}
		}
	}

	if rawURL != "" && info.Provider == "" {
		return LinkInfo{}, domain.Errorf(domain.CodeValidation, "暂不支持该网盘的分享链接，目前支持光鸭云盘与夸克网盘")
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
				if !shareIDPattern.MatchString(candidate) {
					continue
				}
				switch {
				case strings.ContainsAny(candidate, "_-"):
					info.ShareID = candidate
				case quarkBareIDPattern.MatchString(candidate):
					// 纯十六进制且长度足够，按夸克分享 ID 处理。
					info.ShareID = candidate
					if info.Provider == "" {
						info.Provider = ProviderQuark
					}
				}
				if info.ShareID != "" {
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
		return LinkInfo{}, domain.Errorf(domain.CodeValidation, "未能识别云盘分享链接，请粘贴完整分享链接")
	}
	if info.Provider == "" {
		info.Provider = normalizeProvider(fallbackProvider)
	}
	return info, nil
}

// providerFromHost 依据分享页域名判断网盘；无法识别时返回空串。
func providerFromHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	switch {
	case host == "":
		return ""
	case host == "guangyapan.com" || strings.HasSuffix(host, ".guangyapan.com"):
		return ProviderGuangya
	case host == "quark.cn" || strings.HasSuffix(host, ".quark.cn"):
		return ProviderQuark
	}
	return ""
}

func hasURLPrefix(text string) bool {
	lower := strings.ToLower(text)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func shareIDFromURL(parsed *url.URL) string {
	if id := shareIDFromPath(parsed.Path); id != "" {
		return id
	}
	return shareIDFromQuery(parsed.Query())
}

func shareIDFromQuery(query url.Values) string {
	for _, key := range []string{"shareId", "share_id", "pwd_id", "sid", "id"} {
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
		if _, reserved := reservedShareSegments[strings.ToLower(segment)]; reserved {
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
