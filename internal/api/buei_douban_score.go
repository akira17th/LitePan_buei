package api

import (
	"net/http"
	"strings"

	"litepan/internal/buei/doubanscore"
)

// buei:douban-score —— 后台「辅助工具 → 豆瓣评分」：
// 按 STRM 任务海报墙展示作品，并把豆瓣评分保存在主库（与 STRM 索引项目关联）。

type bueiDoubanScoreQueryRequest struct {
	StrmTaskID int64    `json:"strm_task_id"`
	ItemIDs    []string `json:"item_ids"`
	AllTasks   bool     `json:"all_tasks"`
	Force      bool     `json:"force"`
}

func (h *Handler) bueiDoubanScoreTasks(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.DoubanScore != nil) {
		return
	}
	tasks, err := h.buei.DoubanScore.Tasks(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"tasks": tasks})
}

func (h *Handler) bueiDoubanScoreItems(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.DoubanScore != nil) {
		return
	}
	taskID, err := strmTaskIDQuery(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	q := r.URL.Query()
	page, err := h.buei.DoubanScore.Items(r.Context(), doubanscore.ItemQuery{
		StrmTaskID: taskID,
		Offset:     parseInt(q.Get("offset")),
		Limit:      parseInt(q.Get("limit")),
		Keyword:    strings.TrimSpace(q.Get("keyword")),
		Filter:     strings.TrimSpace(q.Get("filter")),
		MediaType:  strings.TrimSpace(q.Get("media_type")),
		Sort:       strings.TrimSpace(q.Get("sort")),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, page)
}

func (h *Handler) bueiDoubanScorePlan(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.DoubanScore != nil) {
		return
	}
	taskID, err := strmTaskIDQuery(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	plan, err := h.buei.DoubanScore.QueryPlan(r.Context(), taskID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, plan)
}

func (h *Handler) bueiDoubanScoreState(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.DoubanScore != nil) {
		return
	}
	state, err := h.buei.DoubanScore.State(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, state)
}

func (h *Handler) bueiDoubanScoreQuery(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.DoubanScore != nil) {
		return
	}
	var req bueiDoubanScoreQueryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	progress, err := h.buei.DoubanScore.StartQuery(doubanscore.RunRequest{
		StrmTaskID: req.StrmTaskID,
		ItemIDs:    req.ItemIDs,
		AllTasks:   req.AllTasks,
		Force:      req.Force,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, progress)
}

func (h *Handler) bueiDoubanScoreStop(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.DoubanScore != nil) {
		return
	}
	h.buei.DoubanScore.StopQuery()
	state, err := h.buei.DoubanScore.State(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, state)
}
