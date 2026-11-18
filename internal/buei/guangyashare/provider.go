package guangyashare

import (
	"context"
	"strings"
	"time"
)

// 云盘提供方标识。取值与目标账号的 driver_type 一致，前端据此过滤可转存账号。
const (
	ProviderGuangya = "guangya"
	ProviderQuark   = "quark"
)

// providerDisplayNames 是各网盘的中文展示名。
var providerDisplayNames = map[string]string{
	ProviderGuangya: "光鸭云盘",
	ProviderQuark:   "夸克网盘",
}

// normalizeProvider 把未知/空值归一为光鸭，兼容只支持光鸭时的历史收藏记录。
func normalizeProvider(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), ProviderQuark) {
		return ProviderQuark
	}
	return ProviderGuangya
}

// providerDisplayName 返回网盘展示名，用于提示文案。
func providerDisplayName(provider string) string {
	if name, ok := providerDisplayNames[normalizeProvider(provider)]; ok {
		return name
	}
	return "云盘"
}

// shareEntry 是各网盘统一后的分享条目。
type shareEntry struct {
	ID      string
	Name    string
	Size    int64
	IsDir   bool
	ModTime time.Time
	// FileType 是光鸭的文件类型枚举；其它网盘为 0，按扩展名归类。
	FileType int
	// Token 是分享内的文件令牌，夸克转存时必须回传；光鸭为空。
	Token string
}

// shareSummary 是分享概要。
type shareSummary struct {
	ShareID  string
	Title    string
	Owner    string
	NeedCode bool
	// Token 是在概要请求里顺带换到的访问令牌（夸克），避免重复请求。
	Token string
}

// shareListResult 是分享目录列表结果。
type shareListResult struct {
	Items []shareEntry
	// Title 只有部分网盘会在列表接口返回分享标题（夸克），光鸭为空。
	Title string
}

// shareProvider 是各网盘公开分享接口的适配层，只负责
// 「概要 → 访问令牌 → 列目录」；转存由目标账号的驱动完成。
type shareProvider interface {
	// Key 返回网盘标识，与 driver_type 一致。
	Key() string
	// DisplayName 返回中文展示名。
	DisplayName() string
	// Summary 读取分享概要；password 可为空，用于判断是否需要提取码。
	Summary(ctx context.Context, shareID, password string) (*shareSummary, error)
	// AccessToken 用分享 ID + 提取码换取访问令牌。
	AccessToken(ctx context.Context, shareID, password string) (string, error)
	// List 列出分享内某个目录的直接内容。
	List(ctx context.Context, token, shareID, parentID string) (*shareListResult, error)
}

// NormalizeProvider 把空/未知的网盘标识归一为光鸭，供传输层复用。
func NormalizeProvider(raw string) string { return normalizeProvider(raw) }

// ProviderName 返回网盘展示名。
func ProviderName(raw string) string { return providerDisplayName(raw) }
