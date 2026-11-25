package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

// buei:emby-library —— 媒体项目的 STRM 目录 / 网盘目录对照浏览。

type bueiEmbyLibraryProjectEntryDTO struct {
	Name       string `json:"name"`
	IsDir      bool   `json:"is_dir"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at,omitempty"`
}

type bueiEmbyLibraryProjectFilesDTO struct {
	ItemID       string                           `json:"item_id"`
	ItemName     string                           `json:"item_name"`
	Path         string                           `json:"path"`
	StrmEntries  []bueiEmbyLibraryProjectEntryDTO `json:"strm_entries"`
	CloudEntries []bueiEmbyLibraryProjectEntryDTO `json:"cloud_entries"`
	StrmError    string                           `json:"strm_error,omitempty"`
	CloudError   string                           `json:"cloud_error,omitempty"`
}

func (h *Handler) bueiEmbyLibraryProjectFiles(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil || h.strm == nil || h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	taskID, itemID, err := bueiParseStrmItemKey(r.URL.Query().Get("item_id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	task, err := h.strm.GetTask(r.Context(), taskID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if task == nil {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "STRM 任务不存在"))
		return
	}
	items, err := bueiListAllStrmItems(r.Context(), h.strmScrape, task.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	var item *strmscrape.Item
	for i := range items {
		if items[i].ID == itemID {
			item = &items[i]
			break
		}
	}
	if item == nil {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "STRM 媒体项目不存在"))
		return
	}

	sub := embyarchive.SplitArchivePath(r.URL.Query().Get("path"))
	relative := append(embyarchive.SplitArchivePath(item.RelDir), sub...)
	strmRoot := strm.TaskOutputDir(h.strmDir, strm.TaskRelDir(task.GroupDir, task.OutputFolder))
	strmDir, err := embyarchive.ProjectPath(strmRoot, relative)
	if err != nil {
		writeErr(w, err)
		return
	}
	result := bueiEmbyLibraryProjectFilesDTO{
		ItemID:   fmt.Sprintf("%d:%s", task.ID, item.ID),
		ItemName: embyarchive.ItemDisplayName(*item),
		Path:     strings.Join(sub, "/"),
	}
	// 两侧目录各自容错：一侧不存在时仍可浏览另一侧。
	strmEntries, err := bueiReadProjectLocalDir(strmDir)
	if err != nil {
		result.StrmEntries = []bueiEmbyLibraryProjectEntryDTO{}
		result.StrmError = err.Error()
	} else {
		result.StrmEntries = strmEntries
	}
	cloudEntries, cloudErr := h.buei.EmbyArchive.BrowseCloudDir(r.Context(), task.AccountID, task.ParentID, relative)
	if cloudErr != nil {
		result.CloudEntries = []bueiEmbyLibraryProjectEntryDTO{}
		result.CloudError = cloudErr.Error()
	} else {
		result.CloudEntries = bueiProjectCloudEntries(cloudEntries)
	}
	writeOK(w, result)
}

func bueiReadProjectLocalDir(dir string) ([]bueiEmbyLibraryProjectEntryDTO, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.Errorf(domain.CodeNotFound, "目录不存在：%s", filepath.ToSlash(dir))
		}
		return nil, domain.Wrap(domain.CodeDriverError, err)
	}
	out := make([]bueiEmbyLibraryProjectEntryDTO, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		item := bueiEmbyLibraryProjectEntryDTO{
			Name:       entry.Name(),
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		}
		if item.IsDir {
			item.Size = 0
		}
		out = append(out, item)
	}
	return out, nil
}

func bueiProjectCloudEntries(items []domain.FileItem) []bueiEmbyLibraryProjectEntryDTO {
	out := make([]bueiEmbyLibraryProjectEntryDTO, 0, len(items))
	for _, item := range items {
		entry := bueiEmbyLibraryProjectEntryDTO{
			Name:  item.Name,
			IsDir: item.IsDir,
			Size:  item.Size,
		}
		if !item.ModTime.IsZero() {
			entry.ModifiedAt = item.ModTime.UTC().Format(time.RFC3339)
		}
		if entry.IsDir {
			entry.Size = 0
		}
		out = append(out, entry)
	}
	return out
}
