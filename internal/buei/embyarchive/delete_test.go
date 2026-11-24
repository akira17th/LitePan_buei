package embyarchive

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

type fakeFiles struct {
	tree       map[string][]domain.FileItem
	deletedIDs []string
}

func (f *fakeFiles) List(_ context.Context, _ int64, parentID string, _ bool) ([]domain.FileItem, error) {
	return f.tree[parentID], nil
}

func (f *fakeFiles) CreateFolder(context.Context, int64, string, string) (*domain.FileItem, error) {
	return nil, nil
}

func (f *fakeFiles) MoveFiles(context.Context, int64, []string, string, string) error {
	return nil
}

func (f *fakeFiles) DeleteFiles(_ context.Context, _ int64, fileIDs []string, _ string) error {
	f.deletedIDs = append(f.deletedIDs, fileIDs...)
	return nil
}

type fakeStrm struct{ tasks []*domain.StrmTask }

func (f *fakeStrm) ListTasks(context.Context) ([]*domain.StrmTask, error) { return f.tasks, nil }

func (f *fakeStrm) GetTask(_ context.Context, id int64) (*domain.StrmTask, error) {
	for _, task := range f.tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return nil, domain.Errorf(domain.CodeNotFound, "STRM 任务不存在")
}

func (f *fakeStrm) TryBeginTaskFileOperation(int64) (func(), bool) { return func() {}, true }

type fakeScrape struct{ refreshed []int64 }

func (f *fakeScrape) ListItems(context.Context, int64, strmscrape.ItemListQuery) (strmscrape.ItemListResult, error) {
	return strmscrape.ItemListResult{}, nil
}

func (f *fakeScrape) RefreshIndex(_ context.Context, taskID int64, _ strmscrape.ItemListQuery) (strmscrape.ItemListResult, error) {
	f.refreshed = append(f.refreshed, taskID)
	return strmscrape.ItemListResult{}, nil
}

func TestDeleteItemRemovesCloudAndSTRMThenRefreshes(t *testing.T) {
	strmDir := t.TempDir()
	task := &domain.StrmTask{ID: 7, Name: "剧集", AccountID: 3, ParentID: "root", Path: "/媒体/剧集", OutputFolder: "剧集"}
	strmRel := strm.TaskRelDir(task.GroupDir, task.OutputFolder)
	projectPath := filepath.Join(strm.TaskOutputDir(strmDir, strmRel), "某剧", "S01")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("创建 STRM 目录失败: %v", err)
	}

	files := &fakeFiles{tree: map[string][]domain.FileItem{
		"root": {{ID: "d1", Name: "某剧", IsDir: true}},
		"d1":   {{ID: "d2", Name: "S01", IsDir: true}},
	}}
	scrape := &fakeScrape{}
	svc := New(Options{
		Files:   files,
		Strm:    &fakeStrm{tasks: []*domain.StrmTask{task}},
		Scrape:  scrape,
		Emby:    &fakeEmby{libraries: []embyproxy.Library{{ID: "lib-tv", Name: "剧集"}}},
		StrmDir: strmDir,
	})

	result, err := svc.DeleteItem(context.Background(), DeleteRequest{
		LibraryID: "lib-tv",
		Task:      task,
		Item:      strmscrape.Item{ID: "7:item", RelDir: "某剧/S01"},
	})
	if err != nil {
		t.Fatalf("DeleteItem 返回错误: %v", err)
	}
	if len(files.deletedIDs) != 1 || files.deletedIDs[0] != "d2" {
		t.Fatalf("应删除网盘项目文件夹: %#v", files.deletedIDs)
	}
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("STRM 文件夹应被删除: %v", err)
	}
	if len(scrape.refreshed) != 1 || scrape.refreshed[0] != task.ID {
		t.Fatalf("应刷新对应 STRM 任务索引: %#v", scrape.refreshed)
	}
	if !result.IndexRefresh || !result.EmbyRefresh || result.ItemName != "S01" {
		t.Fatalf("删除结果异常: %#v", result)
	}
}
