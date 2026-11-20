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
	userDir := bueiEmbyUserInfoDir(project)
	if _, err := os.Stat(filepath.Join(userDir, "小明.json")); err != nil {
		t.Fatalf("应按用户名保存 JSON：%v", err)
	}
	if _, err := os.Stat(filepath.Join(userDir, "8f3e6c9a-opaque-id.json")); !os.IsNotExist(err) {
		t.Fatalf("不应使用用户 ID 作为文件名，err=%v", err)
	}
}
