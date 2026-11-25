package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

// buei:emby-library —— 项目目录里检测尚未生成的 STRM，确认后生成并自动刮削。

// bueiEmbyLibraryStrmStatusDTO 始终返回待生成 / 待同步元数据 / 多余 STRM 三个计数。
type bueiEmbyLibraryStrmStatusDTO struct {
	PendingStrm     int64    `json:"pending_strm"`
	PendingMetadata int64    `json:"pending_metadata"`
	OrphanStrm      int64    `json:"orphan_strm"`
	OrphanNames     []string `json:"orphan_names,omitempty"`
}

type bueiEmbyLibraryStrmGenerateResultDTO struct {
	Created    int64 `json:"created"`
	Updated    int64 `json:"updated"`
	MediaCount int64 `json:"media_count"`
	// MetadataCreated / MetadataUploaded / MetadataDeleted 是本次同步的元数据文件数量。
	MetadataCreated  int64  `json:"metadata_created"`
	MetadataUploaded int64  `json:"metadata_uploaded"`
	MetadataDeleted  int64  `json:"metadata_deleted"`
	Scraped          bool   `json:"scraped"`
	ScrapeError      string `json:"scrape_error,omitempty"`
	IndexRefresh     bool   `json:"index_refreshed"`
	EmbyRefresh      bool   `json:"emby_refreshed"`
	EmbyError        string `json:"emby_error,omitempty"`
}

// bueiProjectStrmTarget 是项目当前目录在网盘侧的位置与内容，供生成 STRM/状态检测使用。
type bueiProjectStrmTarget struct {
	Task        *domain.StrmTask
	Item        strmscrape.Item
	DirID       string
	STRMDir     string
	CurrentPath string
	Dirs        []string
	Entries     []strm.CurrentDirectoryEntry
}

func (h *Handler) bueiResolveProjectStrmTarget(ctx context.Context, rawItemID, subPath string) (bueiProjectStrmTarget, error) {
	taskID, itemID, err := bueiParseStrmItemKey(rawItemID)
	if err != nil {
		return bueiProjectStrmTarget{}, err
	}
	task, err := h.strm.GetTask(ctx, taskID)
	if err != nil {
		return bueiProjectStrmTarget{}, err
	}
	if task == nil {
		return bueiProjectStrmTarget{}, domain.Errorf(domain.CodeNotFound, "STRM 任务不存在")
	}
	items, err := bueiListAllStrmItems(ctx, h.strmScrape, task.ID)
	if err != nil {
		return bueiProjectStrmTarget{}, err
	}
	var item *strmscrape.Item
	for i := range items {
		if items[i].ID == itemID {
			item = &items[i]
			break
		}
	}
	if item == nil {
		return bueiProjectStrmTarget{}, domain.Errorf(domain.CodeNotFound, "STRM 媒体项目不存在")
	}

	relative := append(embyarchive.SplitArchivePath(item.RelDir), embyarchive.SplitArchivePath(subPath)...)
	dirID, cloudItems, err := h.buei.EmbyArchive.BrowseCloudDirWithID(ctx, task.AccountID, task.ParentID, relative, false)
	if err != nil {
		return bueiProjectStrmTarget{}, err
	}
	entries := make([]strm.CurrentDirectoryEntry, 0, len(cloudItems))
	for _, entry := range cloudItems {
		entries = append(entries, strm.CurrentDirectoryEntry{ID: entry.ID, Name: entry.Name, Size: entry.Size, IsDir: entry.IsDir})
	}
	strmRoot := strm.TaskOutputDir(h.strmDir, strm.TaskRelDir(task.GroupDir, task.OutputFolder))
	strmDir, err := embyarchive.ProjectPath(strmRoot, relative)
	if err != nil {
		return bueiProjectStrmTarget{}, err
	}
	dirs := append(embyarchive.SplitArchivePath(task.Path), relative...)
	return bueiProjectStrmTarget{
		Task:        task,
		Item:        *item,
		DirID:       dirID,
		STRMDir:     strmDir,
		CurrentPath: "/" + strings.Join(dirs, "/"),
		Dirs:        dirs,
		Entries:     entries,
	}, nil
}

// bueiOrphanSTRMNames 找出本地存在、但网盘侧没有对应视频的 .strm 文件。
func bueiOrphanSTRMNames(dir string, cloudEntries []strm.CurrentDirectoryEntry) []string {
	videoKeys := make(map[string]bool, len(cloudEntries))
	for _, entry := range cloudEntries {
		if entry.IsDir || !embyarchive.IsVideoFileName(entry.Name) {
			continue
		}
		if key := embyarchive.MediaBaseKey(entry.Name); key != "" {
			videoKeys[key] = true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0)
	for _, dirEntry := range entries {
		if dirEntry.IsDir() || !strings.EqualFold(filepath.Ext(dirEntry.Name()), ".strm") {
			continue
		}
		key := embyarchive.MediaBaseKey(dirEntry.Name())
		if key == "" || videoKeys[key] {
			continue
		}
		out = append(out, dirEntry.Name())
	}
	sort.Strings(out)
	return out
}

func (h *Handler) bueiEmbyLibraryProjectStrmStatus(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil || h.strm == nil || h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	target, err := h.bueiResolveProjectStrmTarget(r.Context(), r.URL.Query().Get("item_id"), r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	status, err := h.strm.CheckCurrentDirectoryStatus(r.Context(), target.Task.AccountID, target.DirID, target.CurrentPath, target.Dirs, target.Entries)
	if err != nil {
		writeErr(w, err)
		return
	}
	if status.MatchedTaskID != target.Task.ID {
		// 匹配到其它任务时不提示，避免误在别的任务目录里生成。
		writeOK(w, bueiEmbyLibraryStrmStatusDTO{})
		return
	}
	orphans := bueiOrphanSTRMNames(target.STRMDir, target.Entries)
	writeOK(w, bueiEmbyLibraryStrmStatusDTO{
		PendingStrm:     status.PendingStrm,
		PendingMetadata: status.PendingMetadata,
		OrphanStrm:      int64(len(orphans)),
		OrphanNames:     orphans,
	})
}

func (h *Handler) bueiEmbyLibraryProjectStrmCleanup(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil || h.strm == nil || h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req struct {
		ItemID    string   `json:"item_id"`
		Path      string   `json:"path"`
		Names     []string `json:"names"`
		ConfigID  string   `json:"config_id"`
		LibraryID string   `json:"library_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	target, err := h.bueiResolveProjectStrmTarget(r.Context(), req.ItemID, req.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	names := req.Names
	if len(names) == 0 {
		names = bueiOrphanSTRMNames(target.STRMDir, target.Entries)
	}
	result, err := h.buei.EmbyArchive.DeleteProjectSTRMs(r.Context(), embyarchive.ProjectSTRMDeleteRequest{
		ConfigID:  strings.TrimSpace(req.ConfigID),
		LibraryID: strings.TrimSpace(req.LibraryID),
		Task:      target.Task,
		Item:      target.Item,
		Path:      embyarchive.SplitArchivePath(req.Path),
		Names:     names,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"deleted":         len(result.Deleted),
		"deleted_names":   result.Deleted,
		"index_refreshed": result.IndexRefresh,
		"emby_refreshed":  result.EmbyRefresh,
	})
}

func (h *Handler) bueiEmbyLibraryProjectStrmGenerate(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil || h.strm == nil || h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req struct {
		ItemID    string `json:"item_id"`
		Path      string `json:"path"`
		ConfigID  string `json:"config_id"`
		LibraryID string `json:"library_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	target, err := h.bueiResolveProjectStrmTarget(r.Context(), req.ItemID, req.Path)
	if err != nil {
		writeErr(w, err)
		return
	}

	generated, err := h.strm.GenerateCurrentDirectory(r.Context(), target.Task.AccountID, target.DirID, target.CurrentPath, target.Dirs, target.Entries)
	if err != nil {
		writeErr(w, err)
		return
	}
	result := bueiEmbyLibraryStrmGenerateResultDTO{
		Created:          generated.Created,
		Updated:          generated.Updated,
		MediaCount:       generated.MediaCount,
		MetadataCreated:  generated.MetadataCreated,
		MetadataUploaded: generated.MetadataUploaded,
		MetadataDeleted:  generated.MetadataDeleted,
	}
	// 只同步了元数据（没有新 STRM）时不需要刮削，但仍要刷新索引与媒体库。
	metadataChanged := generated.MetadataCreated+generated.MetadataUploaded+generated.MetadataDeleted > 0
	changed := generated.Created+generated.Updated+generated.Deleted > 0 || metadataChanged
	if generated.Created+generated.Updated > 0 {
		// 只刮本次项目目录，避免把整个任务重新刮一遍。
		include := strings.Join(embyarchive.SplitArchivePath(target.Item.RelDir), "/")
		if err := h.strmScrape.ScrapeDirs(r.Context(), target.Task.ID, []string{include}); err != nil {
			result.ScrapeError = err.Error()
		} else {
			result.Scraped = true
		}
	}
	if changed {
		if _, err := h.strmScrape.RefreshIndex(r.Context(), target.Task.ID, strmscrape.ItemListQuery{
			Offset: 0,
			Limit:  200,
			Sort:   strmscrape.ItemListSortTitleAsc,
		}); err != nil {
			writeErr(w, domain.Errorf(domain.CodeDriverError, "STRM 已生成，但刷新 STRM 索引失败：%v", err))
			return
		}
		result.IndexRefresh = true
		if h.embyProxy != nil {
			if err := embyarchive.RefreshLibraries(r.Context(), h.embyProxy, strings.TrimSpace(req.ConfigID), strings.TrimSpace(req.LibraryID)); err != nil {
				result.EmbyError = err.Error()
			} else {
				result.EmbyRefresh = true
			}
		}
	}
	writeOK(w, result)
}
