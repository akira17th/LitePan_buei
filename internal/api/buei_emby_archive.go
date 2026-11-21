package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
)

// buei:emby-archive —— 归档媒体库的自动归档配置、立即执行与执行日志。

type bueiEmbyArchiveSourceDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type bueiEmbyArchiveRuleDTO struct {
	ArchiveLibraryID   string                     `json:"archive_library_id"`
	ArchiveLibraryName string                     `json:"archive_library_name"`
	ConfigID           string                     `json:"config_id,omitempty"`
	SourceLibraryIDs   []string                   `json:"source_library_ids"`
	SourceLibraries    []bueiEmbyArchiveSourceDTO `json:"source_libraries"`
	IntervalHours      int                        `json:"interval_hours"`
	Enabled            bool                       `json:"enabled"`
	LastRunAt          string                     `json:"last_run_at,omitempty"`
	NextRunAt          string                     `json:"next_run_at,omitempty"`
}

type bueiEmbyArchiveSourceOptionDTO struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	CollectionType       string `json:"collection_type,omitempty"`
	ClaimedByLibraryID   string `json:"claimed_by_library_id,omitempty"`
	ClaimedByLibraryName string `json:"claimed_by_library_name,omitempty"`
}

type bueiEmbyArchiveRuleViewDTO struct {
	Rule            bueiEmbyArchiveRuleDTO           `json:"rule"`
	SourceLibraries []bueiEmbyArchiveSourceOptionDTO `json:"source_libraries"`
	NextRunAt       string                           `json:"next_run_at,omitempty"`
}

type bueiEmbyArchiveRuleRequest struct {
	ConfigID         string   `json:"config_id"`
	ArchiveLibraryID string   `json:"archive_library_id"`
	SourceLibraryIDs []string `json:"source_library_ids"`
	IntervalHours    int      `json:"interval_hours"`
	// Enabled 缺省时按启用处理。
	Enabled *bool `json:"enabled"`
}

type bueiEmbyArchiveRunRequest struct {
	ConfigID         string `json:"config_id"`
	ArchiveLibraryID string `json:"archive_library_id"`
}

type bueiEmbyArchiveRunItemDTO struct {
	Name          string `json:"name"`
	MediaType     string `json:"media_type,omitempty"`
	SourceLibrary string `json:"source_library,omitempty"`
	SourceTask    string `json:"source_task,omitempty"`
	TargetPath    string `json:"target_path,omitempty"`
	Result        string `json:"result"`
	Message       string `json:"message,omitempty"`
}

type bueiEmbyArchiveRunDTO struct {
	ID                 int64                       `json:"id"`
	ArchiveLibraryID   string                      `json:"archive_library_id"`
	ArchiveLibraryName string                      `json:"archive_library_name"`
	Trigger            string                      `json:"trigger"`
	Status             string                      `json:"status"`
	StartedAt          string                      `json:"started_at,omitempty"`
	FinishedAt         string                      `json:"finished_at,omitempty"`
	Scanned            int                         `json:"scanned"`
	Archived           int                         `json:"archived"`
	Skipped            int                         `json:"skipped"`
	Failed             int                         `json:"failed"`
	Message            string                      `json:"message,omitempty"`
	Items              []bueiEmbyArchiveRunItemDTO `json:"items"`
}

func (h *Handler) bueiEmbyArchiveRuleView(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	libraryID := strings.TrimSpace(r.URL.Query().Get("library_id"))
	if libraryID == "" {
		writeErr(w, domain.Errorf(domain.CodeValidation, "请选择归档媒体库"))
		return
	}
	// summary=1：只读取已保存的配置，不访问 Emby（页面标题展示执行计划用）。
	if r.URL.Query().Get("summary") == "1" {
		rule, err := h.buei.EmbyArchive.RuleSummary(r.Context(), libraryID)
		if err != nil {
			writeErr(w, err)
			return
		}
		nextRunAt := time.Time{}
		if rule != nil {
			nextRunAt = rule.NextRunAt
		}
		writeOK(w, bueiEmbyArchiveRuleViewDTO{
			Rule:            bueiEmbyArchiveRuleDTOFrom(rule),
			SourceLibraries: []bueiEmbyArchiveSourceOptionDTO{},
			NextRunAt:       bueiEmbyArchiveTime(nextRunAt),
		})
		return
	}
	view, err := h.buei.EmbyArchive.RuleView(r.Context(), strings.TrimSpace(r.URL.Query().Get("config_id")), libraryID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiEmbyArchiveRuleViewDTOFrom(view))
}

func (h *Handler) bueiEmbyArchiveRuleSave(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req bueiEmbyArchiveRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	rule, err := h.buei.EmbyArchive.SaveRule(r.Context(), embyarchive.SaveRuleInput{
		ConfigID:         strings.TrimSpace(req.ConfigID),
		ArchiveLibraryID: strings.TrimSpace(req.ArchiveLibraryID),
		SourceLibraryIDs: req.SourceLibraryIDs,
		IntervalHours:    req.IntervalHours,
		Enabled:          req.Enabled,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiEmbyArchiveRuleDTOFrom(rule))
}

func (h *Handler) bueiEmbyArchiveRunNow(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	var req bueiEmbyArchiveRunRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	run, err := h.buei.EmbyArchive.StartRun(r.Context(), strings.TrimSpace(req.ArchiveLibraryID), domain.BueiArchiveTriggerManual)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiEmbyArchiveRunDTOFrom(run))
}

func (h *Handler) bueiEmbyArchiveLogs(w http.ResponseWriter, r *http.Request) {
	if h.buei.EmbyArchive == nil {
		writeErr(w, domain.Errf(domain.CodeNotImplement))
		return
	}
	libraryID := strings.TrimSpace(r.URL.Query().Get("library_id"))
	if libraryID == "" {
		writeErr(w, domain.Errorf(domain.CodeValidation, "请选择归档媒体库"))
		return
	}
	limit := 10
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeErr(w, domain.Errorf(domain.CodeValidation, "limit 参数无效"))
			return
		}
		limit = parsed
	}
	runs, err := h.buei.EmbyArchive.Logs(r.Context(), libraryID, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]bueiEmbyArchiveRunDTO, 0, len(runs))
	for _, run := range runs {
		out = append(out, bueiEmbyArchiveRunDTOFrom(run))
	}
	writeOK(w, map[string]any{"items": out})
}

func bueiEmbyArchiveRuleViewDTOFrom(view embyarchive.RuleView) bueiEmbyArchiveRuleViewDTO {
	options := make([]bueiEmbyArchiveSourceOptionDTO, 0, len(view.SourceLibraries))
	for _, option := range view.SourceLibraries {
		options = append(options, bueiEmbyArchiveSourceOptionDTO{
			ID:                   option.ID,
			Name:                 option.Name,
			CollectionType:       option.CollectionType,
			ClaimedByLibraryID:   option.ClaimedByLibraryID,
			ClaimedByLibraryName: option.ClaimedByLibraryName,
		})
	}
	dto := bueiEmbyArchiveRuleViewDTO{
		Rule:            bueiEmbyArchiveRuleDTOFrom(view.Rule),
		SourceLibraries: options,
		NextRunAt:       bueiEmbyArchiveTime(view.NextRunAt),
	}
	return dto
}

func bueiEmbyArchiveRuleDTOFrom(rule *domain.BueiEmbyArchiveRule) bueiEmbyArchiveRuleDTO {
	if rule == nil {
		return bueiEmbyArchiveRuleDTO{SourceLibraries: []bueiEmbyArchiveSourceDTO{}}
	}
	sources := make([]bueiEmbyArchiveSourceDTO, 0, len(rule.SourceLibraries))
	ids := make([]string, 0, len(rule.SourceLibraries))
	for _, source := range rule.SourceLibraries {
		sources = append(sources, bueiEmbyArchiveSourceDTO{ID: source.ID, Name: source.Name})
		ids = append(ids, source.ID)
	}
	return bueiEmbyArchiveRuleDTO{
		ArchiveLibraryID:   rule.ArchiveLibraryID,
		ArchiveLibraryName: rule.ArchiveLibraryName,
		ConfigID:           rule.ConfigID,
		SourceLibraryIDs:   ids,
		SourceLibraries:    sources,
		IntervalHours:      rule.IntervalHours,
		Enabled:            rule.Enabled,
		LastRunAt:          bueiEmbyArchiveTime(rule.LastRunAt),
		NextRunAt:          bueiEmbyArchiveTime(rule.NextRunAt),
	}
}

func bueiEmbyArchiveRunDTOFrom(run *domain.BueiEmbyArchiveRun) bueiEmbyArchiveRunDTO {
	items := make([]bueiEmbyArchiveRunItemDTO, 0, len(run.Items))
	for _, item := range run.Items {
		items = append(items, bueiEmbyArchiveRunItemDTO{
			Name:          item.Name,
			MediaType:     item.MediaType,
			SourceLibrary: item.SourceLibrary,
			SourceTask:    item.SourceTask,
			TargetPath:    item.TargetPath,
			Result:        item.Result,
			Message:       item.Message,
		})
	}
	return bueiEmbyArchiveRunDTO{
		ID:                 run.ID,
		ArchiveLibraryID:   run.ArchiveLibraryID,
		ArchiveLibraryName: run.ArchiveLibraryName,
		Trigger:            run.Trigger,
		Status:             run.Status,
		StartedAt:          bueiEmbyArchiveTime(run.StartedAt),
		FinishedAt:         bueiEmbyArchiveTime(run.FinishedAt),
		Scanned:            run.Scanned,
		Archived:           run.Archived,
		Skipped:            run.Skipped,
		Failed:             run.Failed,
		Message:            run.Message,
		Items:              items,
	}
}

func bueiEmbyArchiveTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
