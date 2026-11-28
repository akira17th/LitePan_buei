package doubanscore

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// buei:douban-override —— 海报墙卡片「齿轮」弹窗的手动编辑项：
//   - 豆瓣 ID：作品唯一的豆瓣 ID 字段（名称匹配得到的与手动填写的都是同一个），
//     非空时查询豆瓣评分直接读该条目，不再按片名搜索；清空则回到名称匹配；
//   - 豆瓣评分：手动指定评分（0–10，最多一位小数），展示与 NFO 都以它为准；
//   - 不再查询：打开后每日自动查询跳过该项目，手动「查询 / 重查」仍然有效。

// OverrideRequest 是一次手动编辑请求。
type OverrideRequest struct {
	StrmTaskID  int64
	ItemID      string
	DoubanID    string
	Rating      *float64
	NoAutoQuery bool
}

// SaveOverride 保存手动编辑项；评分变更时同步写入作品 NFO 并刷新关联媒体库。
func (s *Service) SaveOverride(ctx context.Context, req OverrideRequest) error {
	itemID := strings.TrimSpace(req.ItemID)
	if req.StrmTaskID <= 0 || itemID == "" {
		return domain.Errorf(domain.CodeValidation, "缺少 STRM 任务或项目")
	}
	doubanID := strings.TrimSpace(req.DoubanID)
	if doubanID != "" && !isDoubanSubjectID(doubanID) {
		return domain.Errorf(domain.CodeValidation, "豆瓣 ID 只能填写条目数字 ID")
	}
	rating := req.Rating
	if rating != nil {
		v := math.Round(*rating*10) / 10
		if math.IsNaN(v) || v < 0 || v > 10 {
			return domain.Errorf(domain.CodeValidation, "豆瓣评分需要在 0 到 10 之间")
		}
		rating = &v
	}
	override := domain.BueiDoubanScoreOverride{
		DoubanID:     doubanID,
		ManualRating: rating,
		NoAutoQuery:  req.NoAutoQuery,
	}
	if err := s.scores.SaveOverride(ctx, req.StrmTaskID, itemID, override); err != nil {
		return err
	}
	if rating == nil {
		return nil
	}
	s.applyManualRating(ctx, req.StrmTaskID, itemID, *rating, doubanID)
	return nil
}

// applyManualRating 把手动评分写进作品 NFO，并刷新任务关联的 Emby 媒体库。
// 失败只记警告：覆盖项已经保存，不影响界面上的评分。
func (s *Service) applyManualRating(ctx context.Context, taskID int64, itemID string, rating float64, doubanID string) {
	if s.ratings != nil {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		err := s.ratings.UpdateItemRatings(writeCtx, taskID, []strmscrape.ItemRating{{
			ItemID:   itemID,
			Rating:   rating,
			DoubanID: doubanID,
		}})
		cancel()
		if err != nil {
			s.log.Warn("写入手动评分到 NFO 失败", "task_id", taskID, "item_id", itemID, "err", err)
			return
		}
	}
	refresher := newLibraryRefresher(s.emby, s.libs, s.log)
	refresher.load(ctx)
	refresher.collectTask(taskID)
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	refresher.flush(refreshCtx)
}

// isDoubanSubjectID 判断是否是豆瓣条目数字 ID（如 1292052）。
func isDoubanSubjectID(raw string) bool {
	if raw == "" || len(raw) > 20 {
		return false
	}
	if _, err := strconv.ParseInt(raw, 10, 64); err != nil {
		return false
	}
	return true
}
