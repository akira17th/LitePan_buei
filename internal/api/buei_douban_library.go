package api

import (
	"net/http"
	"strconv"
	"time"

	"litepan/internal/domain"
)

func int64ToString(v int64) string {
	return strconv.FormatInt(v, 10)
}

func formatTimeRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// buei:douban-library —— 豆瓣评分任务 ↔ Emby 媒体库关联：
// 在评分筛选项旁选择媒体库并保存；查到豆瓣评分后触发对应媒体库刷库。

type bueiDoubanLibraryDTO struct {
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type bueiDoubanLibrariesResponse struct {
	// Libraries 是 Emby/Jellyfin 的全部媒体库（id + name）。
	Libraries []bueiDoubanLibraryDTO `json:"libraries"`
	// Bindings 是 strm_task_id → 已关联媒体库。
	Bindings map[string]bueiDoubanLibraryDTO `json:"bindings"`
}

type bueiDoubanLibrarySaveRequest struct {
	StrmTaskID int64  `json:"strm_task_id"`
	LibraryID  string `json:"library_id"`
	// LibraryName 冗余存储，仅用于界面回显；删除关联时忽略该字段。
	LibraryName string `json:"library_name"`
}

func (h *Handler) bueiDoubanLibraryList(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.DoubanScore != nil) {
		return
	}
	resp := bueiDoubanLibrariesResponse{
		Libraries: []bueiDoubanLibraryDTO{},
		Bindings:  map[string]bueiDoubanLibraryDTO{},
	}
	// 媒体库列表来自 Emby 反代配置；Emby 没配时列表留空，绑定仍可读写。
	if h.embyProxy != nil {
		libs, err := h.embyProxy.ListLibraries(r.Context())
		if err == nil {
			for _, lib := range libs {
				resp.Libraries = append(resp.Libraries, bueiDoubanLibraryDTO{
					LibraryID:   lib.ID,
					LibraryName: lib.Name,
				})
			}
		}
		// 列表失败不阻断接口：绑定读取与保存不依赖 Emby 在线。
	}
	bindings, err := h.buei.DoubanScore.TaskLibraries(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	for taskID, lib := range bindings {
		resp.Bindings[int64ToString(taskID)] = bueiDoubanLibraryDTO{
			LibraryID:   lib.LibraryID,
			LibraryName: lib.LibraryName,
			UpdatedAt:   formatTimeRFC3339(lib.UpdatedAt),
		}
	}
	writeOK(w, resp)
}

func (h *Handler) bueiDoubanLibrarySave(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.DoubanScore != nil) {
		return
	}
	var req bueiDoubanLibrarySaveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if req.StrmTaskID <= 0 {
		writeErr(w, domain.Errorf(domain.CodeValidation, "strm_task_id 无效"))
		return
	}
	// library_id 为空 = 解除关联。
	if err := h.buei.DoubanScore.SetTaskLibrary(r.Context(), req.StrmTaskID, req.LibraryID, req.LibraryName); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"saved": true})
}
