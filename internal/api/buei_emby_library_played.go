package api

import (
	"net/http"
	"strings"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/strmscrape"
)

// buei:emby-library —— 点击卡片上的用户名，快捷标记“已看完 / 未看”。

type bueiEmbyLibraryPlayedRequest struct {
	ItemID   string `json:"item_id"`
	UserID   string `json:"user_id"`
	Played   bool   `json:"played"`
	ConfigID string `json:"config_id"`
}

type bueiEmbyLibraryPlayedResult struct {
	User bueiEmbyLibraryUserStatusDTO `json:"user"`
}

func (h *Handler) bueiEmbyLibraryUserPlayed(w http.ResponseWriter, r *http.Request) {
	if h.embyProxy == nil || h.strm == nil || h.strmScrape == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req bueiEmbyLibraryPlayedRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		writeErr(w, domain.Errorf(domain.CodeValidation, "缺少用户 ID"))
		return
	}
	taskID, strmItemID, err := bueiParseStrmItemKey(req.ItemID)
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
	var target *strmscrape.Item
	for i := range items {
		if items[i].ID == strmItemID {
			target = &items[i]
			break
		}
	}
	if target == nil {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "STRM 媒体项目不存在"))
		return
	}

	projectPath, err := bueiEmbyProjectInfoPath(h.strmDir, task, *target)
	if err != nil {
		writeErr(w, err)
		return
	}
	raw, users, total, err := bueiReadStoredEmbyProject(projectPath)
	if err != nil {
		writeErr(w, domain.Wrap(domain.CodeDriverError, err))
		return
	}
	if len(raw) == 0 {
		writeErr(w, domain.Errorf(domain.CodeValidation, "该项目尚未完成 Emby 同步，请先点击“Emby 同步”"))
		return
	}
	embyItemID := strings.TrimSpace(embyarchive.RawString(raw, "Id"))
	if embyItemID == "" {
		writeErr(w, domain.Errorf(domain.CodeValidation, "Emby 项目信息中缺少项目 ID，请重新同步"))
		return
	}

	// 先以 Emby 为准修改观看状态；成功后再更新本地同步文件，避免本地与 Emby 不一致。
	if err := h.embyProxy.SetLibraryItemPlayed(r.Context(), strings.TrimSpace(req.ConfigID), userID, embyItemID, req.Played); err != nil {
		writeErr(w, err)
		return
	}
	updated, ok := bueiApplyUserPlayedStatus(users, userID, *target, total, req.Played)
	if !ok {
		writeErr(w, domain.Errorf(domain.CodeValidation, "本地没有该用户的观看记录，请重新同步后重试"))
		return
	}
	if err := bueiWriteEmbyUserItems(projectPath, users); err != nil {
		writeErr(w, domain.Errorf(domain.CodeDriverError, "Emby 观看状态已更新，但保存本地记录失败：%v", err))
		return
	}
	writeOK(w, bueiEmbyLibraryPlayedResult{User: bueiUserStatusDTOs([]embyproxy.LibraryUserItemStatus{*updated})[0]})
}

// bueiApplyUserPlayedStatus 把切换后的观看状态写回用户记录：
// 剧集按总集数换算已看 / 剩余集数，电影用 1/0 表示已看 / 未看。
func bueiApplyUserPlayedStatus(users []embyproxy.LibraryUserItemStatus, userID string, item strmscrape.Item, total int, played bool) (*embyproxy.LibraryUserItemStatus, bool) {
	for i := range users {
		if users[i].UserID != userID {
			continue
		}
		users[i].Played = played
		if item.MediaType == strmscrape.MediaTypeTV {
			if total > 0 {
				users[i].TotalEpisodes = total
				users[i].RemainingKnown = true
				if played {
					users[i].WatchedEpisodes = total
					users[i].RemainingEpisodes = 0
				} else {
					users[i].WatchedEpisodes = 0
					users[i].RemainingEpisodes = total
				}
			}
		} else if played {
			users[i].WatchedEpisodes = 1
		} else {
			users[i].WatchedEpisodes = 0
		}
		return &users[i], true
	}
	return nil, false
}
