package strmscrape

import (
	"context"
	"time"

	"litepan/internal/domain"
)

// buei:emby-library —— 生成 STRM 后同步刮削指定目录。

// ScrapeDirs 同步刮削任务内指定目录（相对任务输出根，为空表示整个任务），
// 与后台「开始刮削」共用同一把锁，结束后才返回，便于调用方立刻刷新索引与媒体库。
func (s *Service) ScrapeDirs(ctx context.Context, taskID int64, includeDirs []string) error {
	if taskID <= 0 {
		return domain.Errorf(domain.CodeValidation, "STRM 任务不存在")
	}
	if !s.operationMu.TryLock() {
		return domain.Errorf(domain.CodeValidation, "刮削任务进行中，请稍后再试")
	}
	defer s.operationMu.Unlock()
	releaseFiles := func() {}
	if s.strm != nil {
		var ok bool
		releaseFiles, ok = s.strm.TryBeginTaskFileOperation(taskID)
		if !ok {
			return domain.Errorf(domain.CodeValidation, "该 STRM 任务正在运行，请稍后再试")
		}
	}
	defer releaseFiles()

	s.setProgress(func(p *Progress) {
		p.Running = true
		p.TaskID = taskID
		p.Done = 0
		p.Failed = 0
		p.Error = ""
		p.CurrentItemID = ""
		p.StartedAt = time.Now().Format(time.RFC3339)
		p.Message = "正在刮削新生成的 STRM"
	})
	err := s.run(ctx, RunRequest{StrmTaskID: taskID, IncludeDirs: includeDirs})
	s.setProgress(func(p *Progress) {
		p.Running = false
		p.CurrentItemID = ""
		if err != nil {
			p.Error = err.Error()
			if p.Message == "" {
				p.Message = "刮削失败"
			}
			return
		}
		if p.Message == "" {
			p.Message = "刮削完成"
		}
	})
	return err
}
