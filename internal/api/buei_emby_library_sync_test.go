package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

func TestBueiMatchStrmTasksUsesTaskNameSubstring(t *testing.T) {
	tasks := []*domain.StrmTask{
		{ID: 1, Name: "电影-国产"},
		{ID: 2, Name: "电影-欧美"},
		{ID: 3, Name: "剧集"},
		{ID: 4, Name: ""},
	}
	matched := bueiMatchStrmTasks(tasks, "电影")
	if len(matched) != 2 || matched[0].ID != 1 || matched[1].ID != 2 {
		t.Fatalf("匹配任务异常：%+v", matched)
	}
}

func TestBueiEmbyMatchScorePrefersTMDBIDAndYear(t *testing.T) {
	year := 2024
	item := strmscrape.Item{Title: "测试电影", FolderName: "测试电影 (2024)", MediaType: strmscrape.MediaTypeMovie, TMDBID: "123", Year: &year}
	withTMDB := map[string]any{
		"Id": "emby-1", "Name": "完全不同的名称", "Type": "Movie", "ProductionYear": 2024,
		"ProviderIds": map[string]any{"Tmdb": "123"},
	}
	withoutTMDB := map[string]any{"Id": "emby-2", "Name": "测试电影", "Type": "Movie", "ProductionYear": 2024}
	if got := bueiEmbyMatchScore(item, withTMDB); got <= bueiEmbyMatchScore(item, withoutTMDB) {
		t.Fatalf("TMDB 精确匹配分数异常：with=%d without=%d", got, bueiEmbyMatchScore(item, withoutTMDB))
	}
	wrongType := map[string]any{"Name": "测试电影", "Type": "Series", "ProductionYear": 2024}
	if got := bueiEmbyMatchScore(item, wrongType); got != 0 {
		t.Fatalf("类型不一致不应匹配：%d", got)
	}
}

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
