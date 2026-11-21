package strmscrape

import (
	"context"
	"strings"

	"litepan/internal/domain"
)

// buei:emby-library —— 从 Emby 媒体库卡片上手动切换剧集「已完结 / 未完结」。

// SetTVStateRequest 切换剧集完结状态的入参。
type SetTVStateRequest struct {
	StrmTaskID int64  `json:"strm_task_id"`
	ItemID     string `json:"item_id"`
	Ended      bool   `json:"ended"`
}

// SetTVState 手动切换剧集完结状态，写回与「刮削页 → 设为完结」一致的标记：
//   - 设为完结：元数据齐全时写 pending=ended（不再自动刮削），否则按“手动完成”处理；
//   - 取消完结：清除手动完成标记，并把 pending 置为 updating，下一轮刮削会继续追更。
//
// 切换后立即更新索引，返回最新的项目信息。
func (s *Service) SetTVState(ctx context.Context, req SetTVStateRequest) (*Item, error) {
	if req.StrmTaskID <= 0 || strings.TrimSpace(req.ItemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中，请稍后再试")
	}
	defer s.operationMu.Unlock()
	_, root, err := s.resolveTask(ctx, req.StrmTaskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, req.ItemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)
	if mediaType != MediaTypeTV {
		return nil, domain.Errorf(domain.CodeValidation, "只有剧集支持切换完结状态")
	}
	if req.Ended {
		if err := markWorkEnded(g, mediaType); err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "设为完结失败：%v", err)
		}
	} else {
		if err := markWorkUpdating(g); err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "取消完结失败：%v", err)
		}
	}
	s.upsertIndexItem(ctx, req.StrmTaskID, root, g)
	item := buildItem(req.StrmTaskID, root, g)
	return &item, nil
}

func markWorkEnded(g workGroup, mediaType string) error {
	if !workHasNFO(g, mediaType) || !workHasPoster(g, mediaType) {
		return writeManualComplete(g, mediaType)
	}
	return markWorkNormal(g, mediaType)
}

func markWorkUpdating(g workGroup) error {
	clearManualComplete(g)
	st, _ := readPendingState(g)
	st.Status = PendingUpdating
	return writePendingState(g, st)
}
