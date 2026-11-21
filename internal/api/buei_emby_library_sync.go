package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

const (
	bueiEmbyProjectInfoFileName = "emby.json"
	bueiEmbyUserInfoDirName     = "emby_users"
)

type bueiEmbyLibrarySyncRequest struct {
	ConfigID    string `json:"config_id"`
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
}

type bueiEmbyLibrarySyncTaskDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type bueiEmbyLibrarySyncItemDTO struct {
	Key           string                         `json:"key"`
	TotalEpisodes int                            `json:"total_episodes,omitempty"`
	Users         []bueiEmbyLibraryUserStatusDTO `json:"users"`
}

type bueiEmbyLibrarySyncResultDTO struct {
	ConfigID       string                       `json:"config_id"`
	LibraryID      string                       `json:"library_id"`
	LibraryName    string                       `json:"library_name"`
	MatchedTasks   []bueiEmbyLibrarySyncTaskDTO `json:"matched_tasks"`
	Items          []bueiEmbyLibrarySyncItemDTO `json:"items,omitempty"`
	EmbyItems      int                          `json:"emby_items"`
	StrmItems      int                          `json:"strm_items"`
	MatchedItems   int                          `json:"matched_items"`
	SavedItems     int                          `json:"saved_items"`
	UnmatchedItems int                          `json:"unmatched_items"`
	FailedItems    []string                     `json:"failed_items,omitempty"`
}

type bueiEmbyLibrarySyncStateDTO struct {
	MatchedTasks []bueiEmbyLibrarySyncTaskDTO `json:"matched_tasks"`
	Items        []bueiEmbyLibrarySyncItemDTO `json:"items"`
}

type bueiStoredEmbyUserItem struct {
	User              embyproxy.LibraryBrowseUser `json:"user"`
	ItemID            string                      `json:"item_id"`
	ItemName          string                      `json:"item_name,omitempty"`
	ItemType          string                      `json:"item_type,omitempty"`
	Played            bool                        `json:"played"`
	WatchedEpisodes   int                         `json:"watched_episodes,omitempty"`
	RemainingEpisodes int                         `json:"remaining_episodes,omitempty"`
	RemainingKnown    bool                        `json:"remaining_known"`
	TotalEpisodes     int                         `json:"total_episodes,omitempty"`
	UserData          map[string]any              `json:"user_data,omitempty"`
	Raw               map[string]any              `json:"raw,omitempty"`
}

type bueiSyncedProject struct {
	Key           string
	TotalEpisodes int
	Users         []embyproxy.LibraryUserItemStatus
}

func (h *Handler) bueiEmbyLibrarySync(w http.ResponseWriter, r *http.Request) {
	if h.embyProxy == nil || h.strm == nil || h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req bueiEmbyLibrarySyncRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	configID := strings.TrimSpace(req.ConfigID)
	libraryID := strings.TrimSpace(req.LibraryID)
	if libraryID == "" {
		writeErr(w, domain.Errorf(domain.CodeValidation, "请选择 Emby/Jellyfin 媒体库"))
		return
	}

	libraryName, err := embyarchive.ResolveLibraryName(r.Context(), h.embyProxy, configID, libraryID)
	if err != nil {
		writeErr(w, err)
		return
	}
	tasks, err := h.strm.ListTasks(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	matchedTasks := embyarchive.MatchStrmTasks(tasks, libraryName)
	result := bueiEmbyLibrarySyncResultDTO{
		LibraryID:    libraryID,
		LibraryName:  libraryName,
		MatchedTasks: bueiEmbyLibrarySyncTaskDTOs(matchedTasks),
		FailedItems:  []string{},
	}
	if len(matchedTasks) == 0 {
		writeErr(w, domain.Errorf(domain.CodeValidation, "没有找到名称包含“%s”的 STRM 任务", libraryName))
		return
	}

	rawItems, err := h.embyProxy.ListLibraryItemsRaw(r.Context(), configID, libraryID)
	if err != nil {
		writeErr(w, err)
		return
	}
	result.ConfigID = configID
	result.EmbyItems = len(rawItems)
	userStatuses, err := h.embyProxy.ListLibraryItemUserStatuses(r.Context(), configID, rawItems)
	if err != nil {
		writeErr(w, err)
		return
	}

	for _, task := range matchedTasks {
		if err := r.Context().Err(); err != nil {
			writeErr(w, err)
			return
		}
		// 同步前先重建索引，确保已经删除或移动的 STRM 项目不会继续参与匹配。
		if _, err := h.strmScrape.RefreshIndex(r.Context(), task.ID, strmscrape.ItemListQuery{
			Offset: 0,
			Limit:  200,
			Sort:   strmscrape.ItemListSortTitleAsc,
		}); err != nil {
			writeErr(w, err)
			return
		}
		items, err := bueiListAllStrmItems(r.Context(), h.strmScrape, task.ID)
		if err != nil {
			writeErr(w, err)
			return
		}
		result.StrmItems += len(items)
		matched, saved, failed, synced := bueiSyncTaskItems(r.Context(), h.strmDir, task, items, rawItems, userStatuses)
		result.MatchedItems += matched
		result.SavedItems += saved
		result.FailedItems = append(result.FailedItems, failed...)
		result.Items = append(result.Items, bueiSyncedProjectDTOs(synced)...)
	}
	result.UnmatchedItems = result.EmbyItems - result.MatchedItems
	if result.UnmatchedItems < 0 {
		result.UnmatchedItems = 0
	}
	writeOK(w, result)
}

func (h *Handler) bueiEmbyLibrarySyncState(w http.ResponseWriter, r *http.Request) {
	if h.strm == nil || h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	configID := strings.TrimSpace(r.URL.Query().Get("config_id"))
	libraryID := strings.TrimSpace(r.URL.Query().Get("library_id"))
	if libraryID == "" {
		writeErr(w, domain.Errorf(domain.CodeValidation, "请选择 Emby/Jellyfin 媒体库"))
		return
	}
	if h.embyProxy == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	libraryName, err := embyarchive.ResolveLibraryName(r.Context(), h.embyProxy, configID, libraryID)
	if err != nil {
		writeErr(w, err)
		return
	}
	tasks, err := h.strm.ListTasks(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	matchedTasks := embyarchive.MatchStrmTasks(tasks, libraryName)
	items, err := bueiBuildSyncState(r.Context(), h.strmDir, h.strmScrape, matchedTasks)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiEmbyLibrarySyncStateDTO{
		MatchedTasks: bueiEmbyLibrarySyncTaskDTOs(matchedTasks),
		Items:        items,
	})
}

func bueiEmbyLibrarySyncTaskDTOs(tasks []*domain.StrmTask) []bueiEmbyLibrarySyncTaskDTO {
	out := make([]bueiEmbyLibrarySyncTaskDTO, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		out = append(out, bueiEmbyLibrarySyncTaskDTO{ID: task.ID, Name: task.Name})
	}
	return out
}

func bueiListAllStrmItems(ctx context.Context, svc *strmscrape.Service, taskID int64) ([]strmscrape.Item, error) {
	return embyarchive.ListAllStrmItems(ctx, svc, taskID)
}

func bueiSyncTaskItems(ctx context.Context, strmDir string, task *domain.StrmTask, strmItems []strmscrape.Item, rawItems []map[string]any, userStatuses map[string][]embyproxy.LibraryUserItemStatus) (matched, saved int, failed []string, synced []bueiSyncedProject) {
	if task == nil {
		return 0, 0, nil, nil
	}
	used := make([]bool, len(rawItems))
	failed = make([]string, 0)
	synced = make([]bueiSyncedProject, 0)
	for _, item := range strmItems {
		if err := ctx.Err(); err != nil {
			failed = append(failed, embyarchive.ItemDisplayName(item)+": "+err.Error())
			break
		}
		bestIndex, bestScore := -1, 0
		for index, raw := range rawItems {
			if used[index] {
				continue
			}
			score := embyarchive.EmbyMatchScore(item, raw)
			if score > bestScore {
				bestIndex, bestScore = index, score
			}
		}
		if bestIndex < 0 {
			continue
		}
		used[bestIndex] = true
		matched++
		path, err := bueiEmbyProjectInfoPath(strmDir, task, item)
		if err != nil {
			failed = append(failed, embyarchive.ItemDisplayName(item)+": "+err.Error())
			continue
		}
		if err := bueiWriteEmbyProjectInfo(path, rawItems[bestIndex]); err != nil {
			failed = append(failed, embyarchive.ItemDisplayName(item)+": "+err.Error())
			continue
		}
		embyID := strings.TrimSpace(embyarchive.RawString(rawItems[bestIndex], "Id"))
		users := userStatuses[embyID]
		if err := bueiWriteEmbyUserItems(path, users); err != nil {
			failed = append(failed, embyarchive.ItemDisplayName(item)+": "+err.Error())
			continue
		}
		total := embyarchive.RawInt(rawItems[bestIndex]["RecursiveItemCount"])
		for _, user := range users {
			if user.TotalEpisodes > total {
				total = user.TotalEpisodes
			}
		}
		synced = append(synced, bueiSyncedProject{
			Key:           strconv.FormatInt(task.ID, 10) + ":" + item.ID,
			TotalEpisodes: total,
			Users:         users,
		})
		saved++
	}
	return matched, saved, failed, synced
}

func bueiEmbyProjectInfoPath(strmDir string, task *domain.StrmTask, item strmscrape.Item) (string, error) {
	if task == nil {
		return "", domain.Errorf(domain.CodeValidation, "STRM 任务不存在")
	}
	root := strm.TaskOutputDir(strmDir, strm.TaskRelDir(task.GroupDir, task.OutputFolder))
	if root == "" {
		return "", domain.Errorf(domain.CodeValidation, "STRM 输出目录无效")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", domain.Wrap(domain.CodeInternal, err)
	}
	rel := filepath.FromSlash(strings.TrimSpace(item.RelDir))
	if rel == "." {
		rel = ""
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", domain.Errorf(domain.CodeValidation, "STRM 项目目录无效")
	}
	projectDir := filepath.Join(root, rel)
	projectDir, err = filepath.Abs(projectDir)
	if err != nil {
		return "", domain.Wrap(domain.CodeInternal, err)
	}
	within, err := filepath.Rel(root, projectDir)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", domain.Errorf(domain.CodeValidation, "STRM 项目目录越界")
	}
	fileName := bueiEmbyProjectInfoFileName
	if strings.TrimSpace(item.RelDir) == "" && strings.TrimSpace(item.StrmName) != "" {
		stem := strm.SafeName(strm.MediaStem(item.StrmName))
		if stem != "" && stem != "_" {
			fileName = stem + ".emby.json"
		}
	}
	return filepath.Join(projectDir, fileName), nil
}

func bueiWriteEmbyProjectInfo(path string, raw map[string]any) error {
	body, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("编码 Emby 项目信息：%w", err)
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建项目目录：%w", err)
	}
	tmp := path + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("写入 Emby 项目信息：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("保存 Emby 项目信息：%w", err)
	}
	return nil
}

func (h *Handler) bueiResolveEmbyItemID(ctx context.Context, itemKey string) (string, error) {
	itemKey = strings.TrimSpace(itemKey)
	parts := strings.SplitN(itemKey, ":", 2)
	if len(parts) != 2 {
		// 兼容旧版前端传入的 Emby 项目 ID。
		if itemKey == "" {
			return "", domain.Errorf(domain.CodeValidation, "媒体项目 ID 为空")
		}
		return itemKey, nil
	}
	taskID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || taskID <= 0 || strings.TrimSpace(parts[1]) == "" {
		return "", domain.Errorf(domain.CodeValidation, "STRM 媒体项目 ID 无效")
	}
	if h.strm == nil || h.strmScrape == nil {
		return "", domain.Errf(domain.CodeNotImplement)
	}
	task, err := h.strm.GetTask(ctx, taskID)
	if err != nil {
		return "", err
	}
	items, err := bueiListAllStrmItems(ctx, h.strmScrape, taskID)
	if err != nil {
		return "", err
	}
	var target strmscrape.Item
	found := false
	for _, item := range items {
		if item.ID == strings.TrimSpace(parts[1]) {
			target = item
			found = true
			break
		}
	}
	if !found {
		return "", domain.Errorf(domain.CodeNotFound, "STRM 媒体项目不存在")
	}
	path, err := bueiEmbyProjectInfoPath(h.strmDir, task, target)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", domain.Errorf(domain.CodeValidation, "该项目尚未完成 Emby 同步，请先点击“Emby 同步”")
		}
		return "", domain.Wrap(domain.CodeDriverError, err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", domain.Errorf(domain.CodeDriverError, "读取 Emby 项目信息失败")
	}
	embyID := strings.TrimSpace(embyarchive.RawString(raw, "Id"))
	if embyID == "" {
		return "", domain.Errorf(domain.CodeValidation, "Emby 项目信息中缺少项目 ID，请重新同步")
	}
	return embyID, nil
}
