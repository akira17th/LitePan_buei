// #region buei:quark ingest flash to guangya
package quarkingest

import (
	"testing"
	"time"
)

// 速度不能为 0：进度回调很频繁，采样基准不能用每次都刷新的 UpdatedAt。
func TestProgressTrackerComputesDownloadSpeed(t *testing.T) {
	tracker := newProgressTracker()
	tracker.start("p1")
	tracker.setDownload("p1", "f1", "a.mkv", 0, 4000)
	time.Sleep(350 * time.Millisecond)
	tracker.setDownload("p1", "f1", "a.mkv", 2000, 4000)

	progress, ok := tracker.get("p1")
	if !ok {
		t.Fatal("进度条目应存在")
	}
	if progress.Speed <= 0 {
		t.Fatalf("下载速度应大于 0，got %v", progress.Speed)
	}
	if progress.Downloaded != 2000 || progress.Total != 4000 || progress.File != "a.mkv" {
		t.Fatalf("下载进度字段异常: %+v", progress)
	}
}

// 换文件或进度回退时速度要清零重算。
func TestProgressTrackerResetsSpeedOnNewFile(t *testing.T) {
	tracker := newProgressTracker()
	tracker.start("p1")
	tracker.setDownload("p1", "f1", "a.mkv", 0, 4000)
	time.Sleep(350 * time.Millisecond)
	tracker.setDownload("p1", "f1", "a.mkv", 4000, 4000)
	if progress, _ := tracker.get("p1"); progress.Speed <= 0 {
		t.Fatalf("第一个文件应有速度: %+v", progress)
	}

	tracker.setDownload("p1", "f2", "b.mkv", 0, 8000)
	progress, _ := tracker.get("p1")
	if progress.Speed != 0 || progress.Downloaded != 0 || progress.Total != 8000 {
		t.Fatalf("换文件后应重置进度与速度: %+v", progress)
	}
}

// 取消标记与进度共用一个 id，取消后应能查询到。
func TestProgressTrackerCancelFlag(t *testing.T) {
	tracker := newProgressTracker()
	tracker.start("p1")
	if tracker.isCanceled("p1") {
		t.Fatal("初始状态不应是已取消")
	}
	if !tracker.cancel("p1") {
		t.Fatal("取消应命中存在的任务")
	}
	if !tracker.isCanceled("p1") {
		t.Fatal("取消后应返回已取消")
	}
	tracker.clearCancel("p1")
	if tracker.isCanceled("p1") {
		t.Fatal("清理后不应再视为已取消")
	}
	if tracker.cancel("missing") {
		t.Fatal("不存在的任务不应命中取消")
	}
}

// #endregion buei:quark ingest flash to guangya
