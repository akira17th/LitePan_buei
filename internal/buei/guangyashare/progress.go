package guangyashare

import (
	"strings"
	"sync"
	"time"
)

// 转存阶段：前端按阶段展示「转存中 → 重命名 → 生成 STRM」的进度。
const (
	GuangyaSharePhaseTransfer = "transfer"
	GuangyaSharePhaseRename   = "rename"
	GuangyaSharePhaseStrm     = "strm"
	GuangyaSharePhaseDone     = "done"
	GuangyaSharePhaseFailed   = "failed"
)

const (
	// progressTTL 控制进度条目的保留时间，超时自动清理，避免无限增长。
	progressTTL = 10 * time.Minute
	// progressMaxEntries 是同时保留的进度条目上限。
	progressMaxEntries = 64
)

// TransferProgress 是一次转存任务的阶段快照。
type TransferProgress struct {
	Phase     string    `json:"phase"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// transferProgressTracker 保存进行中的转存阶段；纯内存即可，进程重启后前端也拿不到结果。
type transferProgressTracker struct {
	mu      sync.Mutex
	entries map[string]TransferProgress
}

func newTransferProgressTracker() *transferProgressTracker {
	return &transferProgressTracker{entries: make(map[string]TransferProgress)}
}

// start 登记一次转存进度，重复的 id 直接覆盖（重试场景）。
func (t *transferProgressTracker) start(id string) {
	if t == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	now := time.Now().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.purgeLocked(now)
	if _, exists := t.entries[id]; !exists && len(t.entries) >= progressMaxEntries {
		t.dropOldestLocked()
	}
	t.entries[id] = TransferProgress{Phase: GuangyaSharePhaseTransfer, UpdatedAt: now}
}

// setPhase 更新阶段；未知 id 直接忽略，避免无进度请求时产生垃圾条目。
func (t *transferProgressTracker) setPhase(id, phase string) {
	if t == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[id]
	if !ok {
		return
	}
	entry.Phase = phase
	entry.UpdatedAt = time.Now().UTC()
	t.entries[id] = entry
}

// finish 结束一次转存进度；errText 非空时标记为失败。
func (t *transferProgressTracker) finish(id, errText string) {
	if t == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	phase := GuangyaSharePhaseDone
	if strings.TrimSpace(errText) != "" {
		phase = GuangyaSharePhaseFailed
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[id] = TransferProgress{
		Phase:     phase,
		Error:     strings.TrimSpace(errText),
		UpdatedAt: time.Now().UTC(),
	}
}

// get 读取阶段快照；过期条目视为不存在。
func (t *transferProgressTracker) get(id string) (TransferProgress, bool) {
	if t == nil {
		return TransferProgress{}, false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return TransferProgress{}, false
	}
	now := time.Now().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.purgeLocked(now)
	entry, ok := t.entries[id]
	if !ok {
		return TransferProgress{}, false
	}
	return entry, true
}

func (t *transferProgressTracker) purgeLocked(now time.Time) {
	for id, entry := range t.entries {
		if now.Sub(entry.UpdatedAt) > progressTTL {
			delete(t.entries, id)
		}
	}
}

func (t *transferProgressTracker) dropOldestLocked() {
	var oldestID string
	var oldest time.Time
	for id, entry := range t.entries {
		if oldestID == "" || entry.UpdatedAt.Before(oldest) {
			oldestID = id
			oldest = entry.UpdatedAt
		}
	}
	if oldestID != "" {
		delete(t.entries, oldestID)
	}
}
