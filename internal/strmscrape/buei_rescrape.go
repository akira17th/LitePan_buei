package strmscrape

import (
	"context"
	"strings"

	"litepan/internal/domain"
)

// buei:emby-library —— 「重刮」：沿用当前 NFO 里的 TMDB ID 同步重新刮削。

// RescrapeItem 同步重新刮削单个项目（沿用已有 TMDB ID），用于 Emby 媒体库的项目目录弹窗。
func (s *Service) RescrapeItem(ctx context.Context, taskID int64, itemID string) (*Item, error) {
	if taskID <= 0 || strings.TrimSpace(itemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	return s.runSyncScrape(ctx, taskID, func(ctx context.Context) (*Item, string, error) {
		root, g, err := s.prepareRematchTarget(ctx, taskID, itemID)
		if err != nil {
			return nil, "", err
		}
		mediaType := resolveWorkMediaType(g)
		meta, ok := readWorkNFOMeta(g, mediaType)
		if _, manual := readManualComplete(g); manual && (!ok || strings.TrimSpace(meta.TMDBID) == "") {
			clearManualComplete(g)
			s.upsertIndexItem(ctx, taskID, root, g)
			item := buildItem(taskID, root, g)
			return &item, workDisplayName(g), nil
		}
		if !ok || strings.TrimSpace(meta.TMDBID) == "" {
			return nil, "", domain.Errorf(domain.CodeValidation, "缺少 TMDB ID，请先做 TMDB 匹配")
		}
		if s.newTMDBClient() == nil {
			return nil, "", domain.Errorf(domain.CodeValidation, "未配置 TMDB API Key")
		}
		display := strings.TrimSpace(meta.Title)
		if display == "" {
			display = workDisplayName(g)
		}
		updated, err := s.applyRematch(ctx, RematchRequest{
			StrmTaskID: taskID,
			ItemID:     itemID,
			TMDBID:     meta.TMDBID,
			MediaType:  mediaType,
			Title:      meta.Title,
			Year:       meta.Year,
		}, root, g, mediaType, s.overwriteForMatch(true))
		if err != nil {
			return nil, display, err
		}
		return updated, display, nil
	})
}

// runSyncScrape 统一处理同步刮削的锁、进度与错误记录。
// run 返回刮削结果、用于提示的名称与错误。
func (s *Service) runSyncScrape(ctx context.Context, taskID int64, run func(context.Context) (*Item, string, error)) (*Item, error) {
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中，请稍后再试")
	}
	defer s.operationMu.Unlock()
	releaseFiles := func() {}
	if s.strm != nil {
		var ok bool
		releaseFiles, ok = s.strm.TryBeginTaskFileOperation(taskID)
		if !ok {
			return nil, domain.Errorf(domain.CodeValidation, "该 STRM 任务正在运行，请稍后再试")
		}
	}
	defer releaseFiles()

	item, display, err := run(ctx)
	message := "已重新刮削：" + display
	if err != nil {
		message = "重新刮削失败：" + display
	}
	s.setProgress(func(p *Progress) {
		p.Running = false
		p.CurrentItemID = ""
		if err != nil {
			p.Failed++
			p.Error = err.Error()
			p.Message = message
			return
		}
		p.Done++
		p.ItemRevision++
		p.UpdatedItem = item
		p.Message = message
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}
