package embyarchive

import (
	"context"
	"fmt"
	"strings"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/strmscrape"
)

// 自动归档规则（写死，不做配置）：
//  1. 剧集必须已完结（TVState == ended）；电影不需要这一条。
//  2. 任一 Emby 用户已看完该项目。
//
// 两个规则同时满足才会归档。
func (s *Service) runRule(ctx context.Context, rule *domain.BueiEmbyArchiveRule, run *domain.BueiEmbyArchiveRun) error {
	libraries, err := s.emby.ListLibraries(ctx, rule.ConfigID)
	if err != nil {
		return err
	}
	archiveLibrary, ok := libraryByID(libraries, rule.ArchiveLibraryID)
	if !ok {
		return domain.Errorf(domain.CodeNotFound, "归档媒体库不存在：%s", rule.ArchiveLibraryName)
	}
	run.ArchiveLibraryName = archiveLibrary.Name
	tasks, err := s.strm.ListTasks(ctx)
	if err != nil {
		return err
	}

	warnings := make([]string, 0, 4)
	touchedSourceTasks := make([]int64, 0, 4)
	touchedTargetTasks := make([]int64, 0, 4)
	touchedLibraries := make([]string, 0, 4)
	remember := func(list []int64, id int64) []int64 {
		for _, existing := range list {
			if existing == id {
				return list
			}
		}
		return append(list, id)
	}
	rememberLibrary := func(list []string, id string) []string {
		for _, existing := range list {
			if existing == id {
				return list
			}
		}
		return append(list, id)
	}

	for _, source := range rule.SourceLibraries {
		if err := ctx.Err(); err != nil {
			return err
		}
		sourceLibrary, ok := libraryByID(libraries, source.ID)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("来源媒体库「%s」已不存在", source.Name))
			continue
		}
		sourceTasks := MatchStrmTasks(tasks, sourceLibrary.Name)
		if len(sourceTasks) == 0 {
			warnings = append(warnings, fmt.Sprintf("「%s」没有匹配的 STRM 任务", sourceLibrary.Name))
			continue
		}
		// 先重建索引，确保新增或删除的项目进入本轮判断；刷新失败时仍按现有索引继续。
		if err := s.refreshIndexes(ctx, taskIDs(sourceTasks)...); err != nil {
			warnings = append(warnings, fmt.Sprintf("「%s」刷新 STRM 索引失败：%v", sourceLibrary.Name, err))
		}
		rawItems, err := s.emby.ListLibraryItemsRaw(ctx, rule.ConfigID, sourceLibrary.ID)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("「%s」读取 Emby 项目失败：%v", sourceLibrary.Name, err))
			continue
		}
		userStatuses, err := s.emby.ListLibraryItemUserStatuses(ctx, rule.ConfigID, rawItems)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("「%s」读取用户观看状态失败：%v", sourceLibrary.Name, err))
			continue
		}

		for _, sourceTask := range sourceTasks {
			if err := ctx.Err(); err != nil {
				return err
			}
			targetTask, err := selectArchiveTask(tasks, archiveLibrary.Name, sourceTask)
			if err != nil {
				warnings = append(warnings, err.Error())
				continue
			}
			items, err := ListAllStrmItems(ctx, s.scrape, sourceTask.ID)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("读取任务「%s」项目失败：%v", sourceTask.Name, err))
				continue
			}
			used := make([]bool, len(rawItems))
			for _, item := range items {
				if err := ctx.Err(); err != nil {
					return err
				}
				if item.MediaType != strmscrape.MediaTypeMovie && item.MediaType != strmscrape.MediaTypeTV {
					continue
				}
				run.Scanned++
				// 平铺 STRM 没有独立项目文件夹，无法按文件夹归档，直接跳过。
				if strings.TrimSpace(item.RelDir) == "" {
					run.Skipped++
					continue
				}
				embyID, ok := matchEmbyItem(item, rawItems, used)
				if !ok {
					run.Skipped++
					continue
				}
				if item.MediaType == strmscrape.MediaTypeTV && item.TVState != strmscrape.TVStateEnded {
					run.Skipped++
					continue
				}
				if !anyUserPlayed(userStatuses[embyID]) {
					run.Skipped++
					continue
				}

				result, err := s.archiveWithPlan(ctx, archivePlan{
					sourceLibrary:  sourceLibrary,
					archiveLibrary: archiveLibrary,
					targetTask:     targetTask,
					targetRelative: TargetRelativePath(sourceTask.Path, targetTask.Path, SplitArchivePath(item.RelDir)),
				}, ArchiveRequest{
					ConfigID:         rule.ConfigID,
					SourceLibraryID:  sourceLibrary.ID,
					ArchiveLibraryID: archiveLibrary.ID,
					SourceTask:       sourceTask,
					Item:             item,
					SkipRefresh:      true,
				})
				if err != nil {
					run.Failed++
					appendRunItem(run, domain.BueiEmbyArchiveRunItem{
						Name:          ItemDisplayName(item),
						MediaType:     item.MediaType,
						SourceLibrary: sourceLibrary.Name,
						SourceTask:    sourceTask.Name,
						Result:        domain.BueiArchiveRunItemFailed,
						Message:       err.Error(),
					})
					continue
				}
				run.Archived++
				appendRunItem(run, domain.BueiEmbyArchiveRunItem{
					Name:          result.ItemName,
					MediaType:     item.MediaType,
					SourceLibrary: sourceLibrary.Name,
					SourceTask:    sourceTask.Name,
					TargetPath:    result.TargetPath,
					Result:        domain.BueiArchiveRunItemArchived,
				})
				touchedSourceTasks = remember(touchedSourceTasks, sourceTask.ID)
				touchedTargetTasks = remember(touchedTargetTasks, targetTask.ID)
				touchedLibraries = rememberLibrary(touchedLibraries, sourceLibrary.ID)
				touchedLibraries = rememberLibrary(touchedLibraries, archiveLibrary.ID)
			}
		}
	}

	// 批量归档只在最后统一刷新，避免每个项目都触发一次索引与媒体库刷新。
	// 有项目归档后立即刷新：源任务索引 → 归档任务索引 → 源媒体库 → 归档媒体库。
	if run.Archived > 0 {
		refreshIDs := append([]int64{}, touchedSourceTasks...)
		refreshIDs = append(refreshIDs, touchedTargetTasks...)
		warnings = append(warnings, s.refreshIndexesTolerant(ctx, refreshIDs)...)
		if len(touchedLibraries) > 0 {
			if err := RefreshLibraries(ctx, s.emby, rule.ConfigID, touchedLibraries...); err != nil {
				warnings = append(warnings, fmt.Sprintf("刷新 Emby 媒体库失败：%v", err))
			}
		}
	}
	if len(warnings) > 0 {
		run.Message = joinMessage("", strings.Join(warnings, "；"))
	}
	return nil
}

// refreshIndexesTolerant 逐个刷新任务索引，单个任务失败不影响其余任务。
func (s *Service) refreshIndexesTolerant(ctx context.Context, taskIDs []int64) []string {
	seen := make(map[int64]struct{}, len(taskIDs))
	warnings := make([]string, 0, 2)
	for _, taskID := range taskIDs {
		if taskID <= 0 {
			continue
		}
		if _, ok := seen[taskID]; ok {
			continue
		}
		seen[taskID] = struct{}{}
		if _, err := s.scrape.RefreshIndex(ctx, taskID, strmscrape.ItemListQuery{
			Offset: 0,
			Limit:  bueiEmbyItemListPageLimit,
			Sort:   bueiEmbyItemListDefaultSort,
		}); err != nil {
			warnings = append(warnings, fmt.Sprintf("刷新 STRM 任务索引失败（任务 ID %d）：%v", taskID, err))
		}
	}
	return warnings
}

func taskIDs(tasks []*domain.StrmTask) []int64 {
	out := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		if task != nil && task.ID > 0 {
			out = append(out, task.ID)
		}
	}
	return out
}

// matchEmbyItem 在同名/同 TMDB 的候选里取匹配分数最高且未被占用的 Emby 项目。
func matchEmbyItem(item strmscrape.Item, rawItems []map[string]any, used []bool) (string, bool) {
	bestIndex, bestScore := -1, 0
	for index, raw := range rawItems {
		if index < len(used) && used[index] {
			continue
		}
		score := EmbyMatchScore(item, raw)
		if score > bestScore {
			bestIndex, bestScore = index, score
		}
	}
	if bestIndex < 0 {
		return "", false
	}
	if bestIndex < len(used) {
		used[bestIndex] = true
	}
	id := strings.TrimSpace(RawString(rawItems[bestIndex], "Id"))
	return id, id != ""
}

func anyUserPlayed(statuses []embyproxy.LibraryUserItemStatus) bool {
	for _, status := range statuses {
		if status.Played {
			return true
		}
	}
	return false
}
