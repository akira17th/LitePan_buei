package doubanscore

import (
	"context"

	"litepan/internal/strmscrape"
)

// ScoresByTask 返回某任务已保存的豆瓣评分视图（项目 ID → 视图），供其它界面复用。
// 传入 items 用于按 STRM 刮削日期判断 NeedsQuery；没有评分记录的项目不会出现在结果里。
func (s *Service) ScoresByTask(ctx context.Context, strmTaskID int64, items []strmscrape.Item) (map[string]ScoreView, error) {
	out := make(map[string]ScoreView)
	if strmTaskID <= 0 || len(items) == 0 {
		return out, nil
	}
	scores, err := s.scores.ListByTask(ctx, strmTaskID)
	if err != nil {
		return nil, err
	}
	raw := make(map[string]strmscrape.Item, len(items))
	for _, item := range items {
		raw[item.ID] = item
	}
	now := s.now()
	for _, score := range scores {
		if score == nil {
			continue
		}
		item, ok := raw[score.ItemID]
		if !ok {
			continue
		}
		view := buildWallItem(strmTaskID, "", item, score, now)
		if view.Douban != nil {
			out[score.ItemID] = *view.Douban
		}
	}
	return out, nil
}
