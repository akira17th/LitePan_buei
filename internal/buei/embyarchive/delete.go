package embyarchive

import (
	"context"
	"os"
	"strings"

	"litepan/internal/domain"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

// DeleteRequest 描述一次媒体项目删除。
type DeleteRequest struct {
	ConfigID  string
	LibraryID string
	Task      *domain.StrmTask
	Item      strmscrape.Item
}

// DeleteResult 是一次删除的结果。
type DeleteResult struct {
	ItemName     string
	TaskID       int64
	TaskName     string
	CloudPath    string
	STRMPath     string
	IndexRefresh bool
	EmbyRefresh  bool
}

// DeleteItem 删除项目对应的网盘文件夹与本地 STRM 文件夹，
// 然后刷新该 STRM 任务索引并触发所属 Emby 媒体库刷库。
func (s *Service) DeleteItem(ctx context.Context, req DeleteRequest) (DeleteResult, error) {
	task := req.Task
	if task == nil {
		return DeleteResult{}, domain.Errorf(domain.CodeValidation, "STRM 任务不存在")
	}
	itemRel := SplitArchivePath(req.Item.RelDir)
	if len(itemRel) == 0 {
		return DeleteResult{}, domain.Errorf(domain.CodeValidation, "当前项目没有对应的媒体文件夹")
	}

	// 删除期间锁住任务，避免扫描/刮削同时改写目录与索引。
	release, err := s.lockTasks(task.ID, task.ID)
	if err != nil {
		return DeleteResult{}, err
	}
	defer release()

	// 先确认网盘目录存在，避免只删掉本地 STRM 文件留下网盘残留。
	folder, parentID, err := s.resolveDir(ctx, task.AccountID, task.ParentID, itemRel)
	if err != nil {
		return DeleteResult{}, err
	}
	if folder == nil || !folder.IsDir {
		return DeleteResult{}, domain.Errorf(domain.CodeValidation, "项目在网盘中不是文件夹：%s", req.Item.RelDir)
	}
	strmPath, err := safeJoin(strm.TaskOutputDir(s.strmDir, strm.TaskRelDir(task.GroupDir, task.OutputFolder)), itemRel)
	if err != nil {
		return DeleteResult{}, err
	}

	if err := s.files.DeleteFiles(ctx, task.AccountID, []string{folder.ID}, parentID); err != nil {
		return DeleteResult{}, err
	}
	if err := os.RemoveAll(strmPath); err != nil {
		return DeleteResult{}, domain.Errorf(domain.CodeDriverError, "网盘文件已删除，但删除 STRM 文件夹失败：%v", err)
	}

	result := DeleteResult{
		ItemName:  folder.Name,
		TaskID:    task.ID,
		TaskName:  task.Name,
		CloudPath: JoinArchivePath(task.Path, req.Item.RelDir),
		STRMPath:  strmPath,
	}
	if err := s.refreshIndexes(ctx, task.ID); err != nil {
		return DeleteResult{}, domain.Errorf(domain.CodeDriverError, "文件已删除，但刷新 STRM 索引失败：%v", err)
	}
	result.IndexRefresh = true
	if err := RefreshLibraries(ctx, s.emby, strings.TrimSpace(req.ConfigID), strings.TrimSpace(req.LibraryID)); err != nil {
		return DeleteResult{}, domain.Errorf(domain.CodeDriverError, "文件已删除，但刷新 Emby 媒体库失败：%v", err)
	}
	result.EmbyRefresh = true
	return result, nil
}
