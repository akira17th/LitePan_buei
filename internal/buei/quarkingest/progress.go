package quarkingest

import (
	"context"
	"strings"
	"sync"
	"time"
)

// 入库阶段：前端按阶段展示「入库 → 重命名 → 生成 STRM」的进度。
const (
	PhaseTransfer = "transfer"
	PhaseRename   = "rename"
	PhaseStrm     = "strm"
	PhaseScrape   = "scrape"
	PhaseEmby     = "emby"
	PhaseDone     = "done"
	PhaseFailed   = "failed"
	// #region buei:quark ingest flash to guangya
	PhaseDownload = "download"
	PhaseFlash    = "flash"
	// #endregion buei:quark ingest flash to guangya
)

const (
	// progressTTL 控制进度条目的保留时间，超时自动清理，避免无限增长。
	progressTTL = 10 * time.Minute
	// progressMaxEntries 是同时保留的进度条目上限。
	progressMaxEntries = 64
)

// Progress 是一次入库任务的阶段快照。
type Progress struct {
	Phase     string    `json:"phase"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	// #region buei:quark ingest flash to guangya
	// 下载阶段的信息：当前文件、已下载/总量（字节）、实时速度（字节/秒）。
	FileID     string  `json:"file_id,omitempty"`
	File       string  `json:"file,omitempty"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	Speed      float64 `json:"speed"`
	// sampleAt / sampleBytes 是速度采样基准（不参与序列化）。
	// 不能用 UpdatedAt 做基准：它每次回调都会刷新，导致永远达不到采样间隔。
	sampleAt    time.Time
	sampleBytes int64
	// Files 是本次入库中各源文件的状态（key 为夸克文件 id），供文件列表逐个展示。
	Files map[string]ProgressFile `json:"files,omitempty"`
	// #endregion buei:quark ingest flash to guangya
}

// #region buei:quark ingest flash to guangya
// ProgressFile 是单个源文件的下载/秒传状态。
type ProgressFile struct {
	// Stage：queued（等待）/ downloading（下载中）/ downloaded（已下载等待秒传）/ flashed（已秒传）/ failed（失败）。
	Stage      string  `json:"stage"`
	Name       string  `json:"name"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	Speed      float64 `json:"speed"`
	Message    string  `json:"message,omitempty"`
}

const (
	FileStageQueued      = "queued"
	FileStageDownloading = "downloading"
	FileStageDownloaded  = "downloaded"
	FileStageFlashed     = "flashed"
	FileStageFailed      = "failed"
)

// #endregion buei:quark ingest flash to guangya

// progressTracker 保存进行中的入库阶段；纯内存即可，进程重启后前端也拿不到结果。
type progressTracker struct {
	mu      sync.Mutex
	entries map[string]Progress
	// #region buei:quark ingest flash to guangya
	// cancels/canceled 支持前端取消进行中的下载与入库。
	cancels  map[string]context.CancelFunc
	canceled map[string]bool
	// #endregion buei:quark ingest flash to guangya
}

func newProgressTracker() *progressTracker {
	return &progressTracker{
		entries: make(map[string]Progress),
		// #region buei:quark ingest flash to guangya
		cancels:  make(map[string]context.CancelFunc),
		canceled: make(map[string]bool),
		// #endregion buei:quark ingest flash to guangya
	}
}

// start 登记一次入库进度，重复的 id 直接覆盖（重试场景）。
func (t *progressTracker) start(id string) {
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
	t.entries[id] = Progress{Phase: PhaseTransfer, UpdatedAt: now}
}

// setPhase 更新阶段；未知 id 直接忽略，避免无进度请求时产生垃圾条目。
func (t *progressTracker) setPhase(id, phase string) {
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

// finish 结束一次入库进度；errText 非空时标记为失败。
func (t *progressTracker) finish(id, errText string) {
	if t == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	phase := PhaseDone
	if strings.TrimSpace(errText) != "" {
		phase = PhaseFailed
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// 结束后保留当前文件与逐文件状态，弹窗里可继续显示 100% / 已秒传 / 提示。
	prev := t.entries[id]
	t.entries[id] = Progress{
		Phase:      phase,
		Error:      strings.TrimSpace(errText),
		UpdatedAt:  time.Now().UTC(),
		FileID:     prev.FileID,
		File:       prev.File,
		Downloaded: prev.Downloaded,
		Total:      prev.Total,
		Files:      prev.Files,
	}
}

// get 读取阶段快照；过期条目视为不存在。
func (t *progressTracker) get(id string) (Progress, bool) {
	if t == nil {
		return Progress{}, false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Progress{}, false
	}
	now := time.Now().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.purgeLocked(now)
	entry, ok := t.entries[id]
	if !ok {
		return Progress{}, false
	}
	return entry, true
}

func (t *progressTracker) purgeLocked(now time.Time) {
	for id, entry := range t.entries {
		if now.Sub(entry.UpdatedAt) > progressTTL {
			delete(t.entries, id)
			// #region buei:quark ingest flash to guangya
			delete(t.cancels, id)
			delete(t.canceled, id)
			// #endregion buei:quark ingest flash to guangya
		}
	}
}

func (t *progressTracker) dropOldestLocked() {
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
		// #region buei:quark ingest flash to guangya
		delete(t.cancels, oldestID)
		delete(t.canceled, oldestID)
		// #endregion buei:quark ingest flash to guangya
	}
}

// #region buei:quark ingest flash to guangya
// queueFiles 登记本次入库涉及的所有源文件（key 为夸克文件 id，value 为本地文件名）。
func (t *progressTracker) queueFiles(id string, files map[string]string) {
	if t == nil || len(files) == 0 {
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
	if entry.Files == nil {
		entry.Files = make(map[string]ProgressFile, len(files))
	}
	for fileID, name := range files {
		entry.Files[fileID] = ProgressFile{Stage: FileStageQueued, Name: name}
	}
	t.entries[id] = entry
}

// setFileStage 更新单个源文件的状态（已下载 / 已秒传 / 失败及提示文案）。
func (t *progressTracker) setFileStage(id, fileID, stage, message string) {
	if t == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" || fileID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[id]
	if !ok {
		return
	}
	if entry.Files == nil {
		entry.Files = map[string]ProgressFile{}
	}
	entry.FileID = fileID
	item := entry.Files[fileID]
	item.Stage = stage
	if message != "" {
		item.Message = message
	}
	if stage == FileStageFlashed || stage == FileStageFailed {
		item.Speed = 0
	}
	entry.Files[fileID] = item
	entry.UpdatedAt = time.Now().UTC()
	t.entries[id] = entry
}

// setDownload 更新下载进度；速度按 0.3s 采样并对相邻值做指数平滑，避免抖动。
func (t *progressTracker) setDownload(id, fileID, file string, done, total int64) {
	if t == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[id]
	if !ok {
		return
	}
	entry.FileID = fileID
	// 换文件或进度回退时重置采样基准。
	if file != entry.File || done < entry.Downloaded {
		entry.File = file
		entry.Downloaded = 0
		entry.Speed = 0
		entry.sampleAt = now
		entry.sampleBytes = 0
	}
	entry.Total = total
	entry.Downloaded = done
	entry.UpdatedAt = now.UTC()
	if elapsed := now.Sub(entry.sampleAt).Seconds(); elapsed >= 0.3 {
		delta := done - entry.sampleBytes
		if delta < 0 {
			delta = 0
		}
		instant := float64(delta) / elapsed
		if entry.Speed > 0 {
			entry.Speed = entry.Speed*0.5 + instant*0.5
		} else {
			entry.Speed = instant
		}
		entry.sampleAt = now
		entry.sampleBytes = done
	}
	// 同步到单文件状态，供文件列表逐个展示进度。
	if fileID != "" {
		if entry.Files == nil {
			entry.Files = map[string]ProgressFile{}
		}
		item := entry.Files[fileID]
		if item.Name == "" {
			item.Name = file
		}
		item.Stage = FileStageDownloading
		item.Downloaded = done
		item.Total = total
		item.Speed = entry.Speed
		if total > 0 && done >= total {
			item.Stage = FileStageDownloaded
		}
		entry.Files[fileID] = item
	}
	t.entries[id] = entry
}

// registerCancel 登记取消回调；同一 id 重复登记时只保留最新的。
func (t *progressTracker) registerCancel(id string, cancel context.CancelFunc) {
	if t == nil || cancel == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cancels[id] = cancel
}

// clearCancel 结束任务后清理取消标记。
func (t *progressTracker) clearCancel(id string) {
	if t == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.cancels, id)
	delete(t.canceled, id)
}

// cancel 标记取消并触发下载上下文中断；返回是否存在进行中的任务。
func (t *progressTracker) cancel(id string) bool {
	if t == nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	t.mu.Lock()
	entry, ok := t.entries[id]
	if !ok {
		t.mu.Unlock()
		return false
	}
	t.canceled[id] = true
	fn := t.cancels[id]
	t.entries[id] = entry
	t.mu.Unlock()
	if fn != nil {
		fn()
	}
	return true
}

// isCanceled 供入库循环检查是否被取消。
func (t *progressTracker) isCanceled(id string) bool {
	if t == nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.canceled[id]
}

// #endregion buei:quark ingest flash to guangya
