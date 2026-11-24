package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/strm"
)

func TestBueiTaskOutputDirExists(t *testing.T) {
	strmDir := t.TempDir()
	task := &domain.StrmTask{ID: 1, Name: "剧集", OutputFolder: "剧集"}
	root := strm.TaskOutputDir(strmDir, strm.TaskRelDir(task.GroupDir, task.OutputFolder))
	if bueiTaskOutputDirExists(strmDir, task) {
		t.Fatalf("目录不存在时应返回 false: %s", root)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("创建输出目录失败: %v", err)
	}
	if !bueiTaskOutputDirExists(strmDir, task) {
		t.Fatal("目录存在时应返回 true")
	}
	if bueiTaskOutputDirExists(strmDir, nil) {
		t.Fatal("任务为空时应返回 false")
	}
}

func TestBueiIsStrmOutputDirMissing(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{domain.Errorf(domain.CodeValidation, "STRM 输出目录不存在：%s", filepath.Join("/tmp", "剧集")), true},
		{domain.Errorf(domain.CodeValidation, "STRM 输出目录无效：/tmp/x"), true},
		{errors.New("其它错误"), false},
	}
	for _, tc := range cases {
		if got := bueiIsStrmOutputDirMissing(tc.err); got != tc.want {
			t.Fatalf("bueiIsStrmOutputDirMissing(%v) = %v，期望 %v", tc.err, got, tc.want)
		}
	}
}
