package store_test

import (
	"context"
	"testing"

	"litepan/internal/store"
)

// buei:douban-score —— STRM 任务 ↔ Emby 媒体库关联仓储（迁移 0042）。
func TestBueiDoubanTaskLibraryRepo(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Memory: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := store.NewBueiDoubanTaskLibraryRepo(db)

	// 空 Map。
	m, err := repo.Map(ctx)
	if err != nil || len(m) != 0 {
		t.Fatalf("空库 Map = %v, %v", m, err)
	}

	// Set 两条。
	if err := repo.Set(ctx, 1, "lib-a", "剧集库"); err != nil {
		t.Fatalf("Set(1): %v", err)
	}
	if err := repo.Set(ctx, 2, "lib-b", "电影库"); err != nil {
		t.Fatalf("Set(2): %v", err)
	}

	m, err = repo.Map(ctx)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if len(m) != 2 || m[1].LibraryID != "lib-a" || m[1].LibraryName != "剧集库" {
		t.Fatalf("Map 内容不符: %+v", m)
	}
	if m[1].UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt 应有值")
	}

	// 覆盖更新。
	if err := repo.Set(ctx, 1, "lib-c", "新剧集库"); err != nil {
		t.Fatalf("Set(1) 覆盖: %v", err)
	}
	m, _ = repo.Map(ctx)
	if m[1].LibraryID != "lib-c" {
		t.Fatalf("覆盖更新失败: %+v", m[1])
	}

	// 删除。
	if err := repo.Delete(ctx, 1); err != nil {
		t.Fatalf("Delete(1): %v", err)
	}
	m, _ = repo.Map(ctx)
	if _, ok := m[1]; ok {
		t.Fatalf("删除后不应存在: %+v", m)
	}
	if len(m) != 1 {
		t.Fatalf("应剩 1 条: %+v", m)
	}
}
