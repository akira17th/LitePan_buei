package strmscrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/settings"
	"litepan/internal/strm"
)

func TestRunRespectsIncludeDirs(t *testing.T) {
	tmdbStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer tmdbStub.Close()

	strmRoot := t.TempDir()
	taskRoot := filepath.Join(strmRoot, "云影视")
	for _, show := range []string{"目标剧 (2026)", "其它剧 (2020)"} {
		dir := filepath.Join(taskRoot, "电视剧", "国产剧", show, "Season 01")
		mustMkdir(t, dir)
		mustWrite(t, filepath.Join(dir, "S01E01.strm"), "x")
	}
	task := &domain.StrmTask{
		ID:           1,
		Name:         "云影视",
		Path:         "/云影视",
		OutputFolder: "云影视",
		ScanMode:     domain.StrmScanModeIncrementalUpdate,
	}
	strmSvc := strm.NewService(strm.ServiceOptions{Repo: &rematchTaskRepo{task: task}, StrmDir: strmRoot})
	settingsSvc := newScrapeSettings(t, map[string]string{
		settings.KeyMOTmdbAPIKey:  "test",
		settings.KeyMOTmdbAPIHost: tmdbStub.URL,
	})
	svc := New(Options{Strm: strmSvc, Settings: settingsSvc, StrmDir: strmRoot, DataDir: t.TempDir()})

	if err := svc.RunAsync(context.Background(), RunRequest{
		StrmTaskID: 1,
		// IncludeDirs 是相对目录路径，不是逐段拆分后的目录列表。
		IncludeDirs: []string{"电视剧/国产剧/目标剧 (2026)"},
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		progress := svc.GetProgress()
		if !progress.Running {
			if progress.Total != 1 || progress.Failed != 1 {
				t.Fatalf("应只刮本次转存的作品，实际 total=%d failed=%d skipped=%d message=%q",
					progress.Total, progress.Failed, progress.Skipped, progress.Message)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("等待刮削结束超时")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
