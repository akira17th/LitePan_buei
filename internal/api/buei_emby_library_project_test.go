package api

import (
	"os"
	"path/filepath"
	"testing"

	"litepan/internal/domain"
)

func TestBueiReadProjectLocalDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Season 01"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "剧集.strm")
	if err := os.WriteFile(file, []byte("http://example.com/a.mkv"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := bueiReadProjectLocalDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("目录项数量异常: %#v", entries)
	}
	byName := make(map[string]bueiEmbyLibraryProjectEntryDTO, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	if season, ok := byName["Season 01"]; !ok || !season.IsDir || season.Size != 0 {
		t.Fatalf("文件夹条目异常: %#v", season)
	}
	if strm, ok := byName["剧集.strm"]; !ok || strm.IsDir || strm.Size == 0 || strm.ModifiedAt == "" {
		t.Fatalf("文件条目异常: %#v", strm)
	}
}

func TestBueiReadProjectLocalDirMissing(t *testing.T) {
	_, err := bueiReadProjectLocalDir(filepath.Join(t.TempDir(), "不存在"))
	if err == nil {
		t.Fatal("目录不存在时应返回错误")
	}
	appErr, ok := domain.AsAppError(err)
	if !ok || appErr.Code != domain.CodeNotFound {
		t.Fatalf("错误码异常: %v", err)
	}
}

func TestBueiProjectCloudEntriesDropsDirSize(t *testing.T) {
	entries := bueiProjectCloudEntries([]domain.FileItem{
		{Name: "Season 01", IsDir: true, Size: 4096},
		{Name: "S01E01.mkv", IsDir: false, Size: 1024},
	})
	if len(entries) != 2 || entries[0].Size != 0 || entries[1].Size != 1024 {
		t.Fatalf("网盘条目转换异常: %#v", entries)
	}
}
