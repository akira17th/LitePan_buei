package strm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateTaskRenamesOutputDirectoryWithTaskName(t *testing.T) {
	svc, st := testService(t)
	svc.strmDir = t.TempDir()
	task := createOutputMoveTask(t, svc, st, "电影")

	oldDir := TaskOutputDir(svc.strmDir, TaskRelDir(task.GroupDir, task.OutputFolder))
	oldFile := filepath.Join(oldDir, "movie.strm")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldFile, []byte("https://example.test/movie\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	input := *task
	input.Name = "夸克电视剧"
	// 模拟现有前端提交任务名称变更时仍带着旧的隐藏输出目录。
	input.OutputFolder = task.OutputFolder
	updated, err := svc.UpdateTask(context.Background(), task.ID, &input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != input.Name {
		t.Fatalf("任务名称 = %q，期望 %q", updated.Name, input.Name)
	}
	if updated.OutputFolder != input.Name {
		t.Fatalf("输出目录 = %q，期望跟随任务名称为 %q", updated.OutputFolder, input.Name)
	}

	newFile := filepath.Join(svc.strmDir, task.GroupDir, input.Name, "movie.strm")
	content, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("任务名称变更后未将 STRM 文件移动到新目录：%v", err)
	}
	if string(content) != "https://example.test/movie\n" {
		t.Fatalf("移动后文件内容 = %q", string(content))
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("旧任务输出目录仍存在，得到 err=%v", err)
	}
}
