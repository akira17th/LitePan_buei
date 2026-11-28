package doubanscore

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"litepan/internal/domain"
)

// buei:run-log —— 每日自动查询的执行日志：记录各任务「待查 / 成功 / 失败 / 原因」。
// 存 config key（buei_douban_score_run_log），每次自动查询结束整体覆盖——只保留最近一次。

// TaskRunStat 是单个 STRM 任务在本轮自动查询中的统计。
type TaskRunStat struct {
	TaskID   int64    `json:"task_id"`
	TaskName string   `json:"task_name"`
	Pending  int      `json:"pending"`           // 本次待查询条数
	Success  int      `json:"success"`           // 查询成功（含豆瓣无评分的正常应答）条数
	Failed   int      `json:"failed"`            // 查询失败（风控/限流/超时/网络等）条数
	Reasons  []string `json:"reasons,omitempty"` // 失败原因（按出现次数从多到少）
	// Items 是本轮真正查过的媒体项目（名称 + 结果 + 失败原因），弹窗按任务展示明细。
	Items []RunLogItem `json:"items,omitempty"`
}

// RunLogItem 是本次自动查询中的一个媒体项目。
type RunLogItem struct {
	Title  string `json:"title"`
	State  string `json:"state"`            // success | failed
	Detail string `json:"detail,omitempty"` // 成功：豆瓣结果；失败：具体原因
}

// RunLog 是一轮自动查询的整体日志。
type RunLog struct {
	FinishedAt string        `json:"finished_at"` // RFC3339
	DurationMS int64         `json:"duration_ms"` // 整轮耗时
	Tasks      []TaskRunStat `json:"tasks"`       // 各任务统计（待查条数为 0 的任务不记）
	Total      int           `json:"total"`       // 全部待查询条数
	Success    int           `json:"success"`     // 全部成功条数
	Failed     int           `json:"failed"`      // 全部失败条数
}

const runLogConfigKey = "buei_douban_score_run_log"

// runLogRecorder 在整轮查询过程中累积各任务统计，结束时落盘。
// 手动查询（弹窗 / 单条重查）不记录——日志只反映每日自动任务。
type runLogRecorder struct {
	started time.Time
	tasks   []taskRunAcc
}

type taskRunAcc struct {
	taskID   int64
	taskName string
	success  int
	failed   int
	reasons  map[string]int
	items    []RunLogItem
}

// beginTask 开始记录一个任务的统计。
func (r *runLogRecorder) beginTask(taskID int64, taskName string) {
	if r == nil {
		return
	}
	r.tasks = append(r.tasks, taskRunAcc{taskID: taskID, taskName: taskName, reasons: map[string]int{}})
}

// recordItem 记一个项目的查询结果（name 是媒体名称，detail 是结果或失败原因）。
func (r *runLogRecorder) recordItem(title string, failed bool, detail string) {
	if r == nil || len(r.tasks) == 0 {
		return
	}
	acc := &r.tasks[len(r.tasks)-1]
	item := RunLogItem{Title: strings.TrimSpace(title), State: "success", Detail: strings.TrimSpace(detail)}
	if failed {
		item.State = "failed"
		acc.failed++
		reason := item.Detail
		if reason == "" {
			reason = "未知错误"
		}
		acc.reasons[reason]++
	} else {
		acc.success++
	}
	acc.items = append(acc.items, item)
}

// finish 生成整轮日志并持久化；待查条数为 0 的任务不出现。
func (r *runLogRecorder) finish(ctx context.Context, configs domain.ConfigRepository, log *slog.Logger) {
	if r == nil || configs == nil {
		return
	}
	runLog := RunLog{
		FinishedAt: time.Now().UTC().Format(time.RFC3339),
		DurationMS: time.Since(r.started).Milliseconds(),
		Tasks:      []TaskRunStat{},
	}
	for _, acc := range r.tasks {
		pending := acc.success + acc.failed
		if pending == 0 {
			continue
		}
		stat := TaskRunStat{
			TaskID:   acc.taskID,
			TaskName: acc.taskName,
			Pending:  pending,
			Success:  acc.success,
			Failed:   acc.failed,
			Items:    acc.items,
		}
		for reason := range acc.reasons {
			stat.Reasons = append(stat.Reasons, reason)
		}
		sortReasonsByCount(acc.reasons, stat.Reasons)
		runLog.Tasks = append(runLog.Tasks, stat)
		runLog.Total += pending
		runLog.Success += acc.success
		runLog.Failed += acc.failed
	}
	raw, err := json.Marshal(runLog)
	if err != nil {
		return
	}
	if err := configs.Set(ctx, runLogConfigKey, string(raw)); err != nil {
		log.Warn("保存豆瓣评分自动查询日志失败", "err", err)
	}
}

func sortReasonsByCount(counts map[string]int, reasons []string) {
	// 按出现次数从多到少稳定排序（插入序保底）。
	for i := 1; i < len(reasons); i++ {
		for j := i; j > 0 && counts[reasons[j]] > counts[reasons[j-1]]; j-- {
			reasons[j], reasons[j-1] = reasons[j-1], reasons[j]
		}
	}
}

// loadRunLog 读取最近一次自动查询日志；没有记录时返回 nil。
func loadRunLog(ctx context.Context, configs domain.ConfigRepository) (*RunLog, error) {
	raw, ok, err := configs.Get(ctx, runLogConfigKey)
	if err != nil {
		return nil, err
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var log RunLog
	if err := json.Unmarshal([]byte(raw), &log); err != nil {
		return nil, nil
	}
	return &log, nil
}
