package store_test

import (
	"context"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/store"
)

func TestBueiEmbyLibrarySyncRepoUpsert(t *testing.T) {
	ctx := context.Background()
	db := newGuangyaShareTestDB(t)
	repo := store.NewBueiEmbyLibrarySyncRepo(db)

	first := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := repo.Save(ctx, &domain.BueiEmbyLibrarySync{LibraryID: "lib-1", LibraryName: "电影", LastSyncedAt: first}); err != nil {
		t.Fatalf("save: %v", err)
	}
	record, err := repo.Get(ctx, "lib-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !record.LastSyncedAt.Equal(first) || record.LibraryName != "电影" {
		t.Fatalf("读取结果不一致：%+v", record)
	}

	second := first.Add(6 * time.Hour)
	if err := repo.Save(ctx, &domain.BueiEmbyLibrarySync{LibraryID: "lib-1", LibraryName: "电影（改名）", LastSyncedAt: second}); err != nil {
		t.Fatalf("resave: %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("同一媒体库应只有一条记录：%+v", list)
	}
	if !list[0].LastSyncedAt.Equal(second) || list[0].LibraryName != "电影（改名）" {
		t.Fatalf("覆盖保存失败：%+v", list[0])
	}
	if _, err := repo.Get(ctx, "missing"); err == nil {
		t.Fatal("不存在的媒体库应返回错误")
	}
}
