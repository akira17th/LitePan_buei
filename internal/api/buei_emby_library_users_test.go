package api

import (
	"os"
	"path/filepath"
	"testing"

	"litepan/internal/embyproxy"
)

func TestBueiWriteEmbyUserItemsUsesReadableUsernameFilename(t *testing.T) {
	project := filepath.Join(t.TempDir(), "movie", bueiEmbyProjectInfoFileName)
	users := []embyproxy.LibraryUserItemStatus{{
		UserID:   "8f3e6c9a-opaque-id",
		UserName: "小明",
		ItemID:   "item-1",
	}}
	if err := bueiWriteEmbyUserItems(project, users); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Dir(project)
	if _, err := os.Stat(filepath.Join(projectDir, "emby_users_小明.json")); err != nil {
		t.Fatalf("应按 emby_users_<用户名>.json 保存：%v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "emby_users")); !os.IsNotExist(err) {
		t.Fatalf("不应再创建 emby_users 目录，err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "emby_users_8f3e6c9a-opaque-id.json")); !os.IsNotExist(err) {
		t.Fatalf("不应使用用户 ID 作为文件名，err=%v", err)
	}

	statuses, _, err := bueiReadEmbyUserItems(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].UserName != "小明" {
		t.Fatalf("读取用户信息异常：%#v", statuses)
	}
}

func TestBueiEmbyUserItemsFlatProjectKeepsProjectPrefix(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "剧集 A.emby.json")
	second := filepath.Join(root, "剧集 B.emby.json")
	if err := bueiWriteEmbyUserItems(first, []embyproxy.LibraryUserItemStatus{{UserName: "小明", ItemID: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := bueiWriteEmbyUserItems(second, []embyproxy.LibraryUserItemStatus{{UserName: "小明", ItemID: "b"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "剧集 A.emby_users_小明.json")); err != nil {
		t.Fatalf("平铺项目应带项目名前缀：%v", err)
	}
	firstUsers, _, err := bueiReadEmbyUserItems(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstUsers) != 1 || firstUsers[0].ItemID != "a" {
		t.Fatalf("平铺项目之间不应互相覆盖：%#v", firstUsers)
	}
}
