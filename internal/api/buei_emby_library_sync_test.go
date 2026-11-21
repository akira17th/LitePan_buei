package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

func TestBueiEmbyProjectInfoPathUsesUniqueFileForFlatStrm(t *testing.T) {
	root := t.TempDir()
	task := &domain.StrmTask{ID: 1, OutputFolder: "电影"}
	path, err := bueiEmbyProjectInfoPath(root, task, strmscrape.Item{RelDir: "", StrmName: "测试电影.strm"})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "电影", "测试电影.emby.json")
	if path != want {
		t.Fatalf("平铺 STRM 信息路径=%q，期望=%q", path, want)
	}
}

func TestBueiWriteEmbyProjectInfoWritesRawJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project", "emby.json")
	raw := map[string]any{"Id": "emby-1", "Name": "测试电影", "ProviderIds": map[string]any{"Tmdb": "123"}}
	if err := bueiWriteEmbyProjectInfo(path, raw); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["Id"] != "emby-1" || got["Name"] != "测试电影" {
		t.Fatalf("写入的 Emby 信息异常：%s", body)
	}
}
