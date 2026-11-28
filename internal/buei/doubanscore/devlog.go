package doubanscore

import (
	"fmt"
	"strconv"
	"strings"

	"litepan/internal/strmscrape"
)

// buei:devlog —— 开发环境（LITEPAN_DEV=1）把豆瓣评分查询过程打到控制台，便于调试。
// 生产环境（Options.DevVerbose 为 false）所有方法都是 no-op，不产生额外日志。

// devf 只在开发环境输出一行查询日志（走 logx 的 system 模块，级别 info）。
func (s *Service) devf(format string, args ...any) {
	if s == nil || !s.devVerbose {
		return
	}
	s.log.Info("[豆瓣评分] " + fmt.Sprintf(format, args...))
}

// devLookupInput 打印一次查询的入参（片名 / 年份 / 类型 / 手动豆瓣 ID）。
func (s *Service) devLookupInput(item strmscrape.Item, manualID string) {
	if s == nil || !s.devVerbose {
		return
	}
	if manualID != "" {
		s.devf("查询[%s]《%s》（%s · %s）→ 手动豆瓣 ID %s", item.ID, item.Title, devYear(item.Year), devKind(item.MediaType), manualID)
		return
	}
	s.devf("查询[%s]《%s》（%s · %s）→ 搜索片名 %q", item.ID, item.Title, devYear(item.Year), devKind(item.MediaType), cleanQueryTitle(item.Title))
}

// devHits 打印一次来源返回的候选与打分，便于确认「为什么匹配到 / 没匹配到它」。
func (s *Service) devHits(source string, hits []searchHit, want string, wantYear *int, mediaType string) {
	if s == nil || !s.devVerbose {
		return
	}
	if len(hits) == 0 {
		s.devf("  %s：没有候选", source)
		return
	}
	parts := make([]string, 0, len(hits))
	for _, hit := range hits {
		score := scoreCandidate(hit.Title, hit.Year, hit.IsTV, hit.Rating != nil, hit.IsSeasonEntry, want, wantYear, mediaType)
		parts = append(parts, fmt.Sprintf("%s(id %s, %s, %s) %.0f 分", hit.Title, hit.ID, devYear(hit.Year), devKindOfTV(hit.IsTV), score))
	}
	s.devf("  %s 候选 %d：%s", source, len(hits), strings.Join(parts, "，"))
}

// devSuggestHits 打印 suggest 来源的候选（没有评分字段，只用来定位条目）。
func (s *Service) devSuggestHits(hits []LookupResult, want string, wantYear *int, mediaType string) {
	if s == nil || !s.devVerbose {
		return
	}
	if len(hits) == 0 {
		s.devf("  suggest：没有候选")
		return
	}
	parts := make([]string, 0, len(hits))
	for _, hit := range hits {
		score := scoreCandidate(hit.DoubanTitle, hit.DoubanYear, hit.IsTV, false, false, want, wantYear, mediaType)
		parts = append(parts, fmt.Sprintf("%s(id %s, %s, %s) %.0f 分", hit.DoubanTitle, hit.DoubanID, devYear(hit.DoubanYear), devKindOfTV(hit.IsTV), score))
	}
	s.devf("  suggest 候选 %d：%s", len(hits), strings.Join(parts, "，"))
}

// devResult 打印查询命中结果（开发环境）。
func (s *Service) devResult(res *LookupResult) {
	if s == nil || !s.devVerbose {
		return
	}
	if res == nil {
		s.devf("  → 没有匹配的豆瓣条目")
		return
	}
	rating := "暂无评分"
	if res.Rating != nil {
		rating = fmt.Sprintf("%.1f 分", *res.Rating)
	}
	s.devf("  → 命中《%s》（id %s，%s，%s，%s，%d 人评价，来源 %s）",
		res.DoubanTitle, res.DoubanID, devYear(res.DoubanYear), devKindOfTV(res.IsTV), rating, res.Votes, res.Source)
}

// devFailure 打印查询失败原因（同时给出原始错误，便于对照归类结果）。
func (s *Service) devFailure(err error) {
	if s == nil || !s.devVerbose || err == nil {
		return
	}
	s.devf("  → 查询失败：%s（原始错误：%v）", DescribeLookupError(err), err)
}

func devYear(year *int) string {
	if year == nil {
		return "年份未知"
	}
	return strconv.Itoa(*year)
}

func devKind(mediaType string) string {
	if mediaType == strmscrape.MediaTypeTV {
		return "剧集"
	}
	return "电影"
}

func devKindOfTV(isTV bool) string {
	if isTV {
		return "剧集"
	}
	return "电影"
}
