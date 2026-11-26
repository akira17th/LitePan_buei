package api

import (
	"context"

	"litepan/internal/buei/doubanscore"
	"litepan/internal/strmscrape"
)

// buei:douban-score —— 在 STRM 刮削索引项目里补上豆瓣评分，供媒体库卡片、刮削列表展示。
// 评分存在主库 buei_douban_scores，不在索引缓存里，所以这里按任务一次性取回再合并。

// bueiStrmScrapeItemDTO 是带豆瓣评分的索引项目（嵌入原结构，JSON 字段保持平铺）。
type bueiStrmScrapeItemDTO struct {
	strmscrape.Item
	Douban *doubanscore.ScoreView `json:"douban,omitempty"`
}

type bueiStrmScrapeItemListDTO struct {
	Items   []bueiStrmScrapeItemDTO  `json:"items"`
	Total   int                      `json:"total"`
	Offset  int                      `json:"offset"`
	Limit   int                      `json:"limit"`
	HasMore bool                     `json:"has_more"`
	Stats   strmscrape.ItemListStats `json:"stats"`
}

// bueiAttachDoubanScores 给索引结果补上豆瓣评分；豆瓣服务未装配时原样返回。
func (h *Handler) bueiAttachDoubanScores(ctx context.Context, taskID int64, result strmscrape.ItemListResult) any {
	if h.buei.DoubanScore == nil || taskID <= 0 || len(result.Items) == 0 {
		return result
	}
	views, err := h.buei.DoubanScore.ScoresByTask(ctx, taskID, result.Items)
	if err != nil || len(views) == 0 {
		// 评分只是附加信息，读取失败时仍返回索引项目本身。
		return result
	}
	items := make([]bueiStrmScrapeItemDTO, 0, len(result.Items))
	for _, item := range result.Items {
		dto := bueiStrmScrapeItemDTO{Item: item}
		if view, ok := views[item.ID]; ok {
			v := view
			dto.Douban = &v
		}
		items = append(items, dto)
	}
	return bueiStrmScrapeItemListDTO{
		Items:   items,
		Total:   result.Total,
		Offset:  result.Offset,
		Limit:   result.Limit,
		HasMore: result.HasMore,
		Stats:   result.Stats,
	}
}
