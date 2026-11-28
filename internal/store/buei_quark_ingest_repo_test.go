package store_test

import (
	"context"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/store"
)

func TestQuarkIngestFavoriteRepoCRUD(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Memory: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := store.NewQuarkIngestFavoriteRepo(db)
	saved, err := repo.Create(ctx, &domain.QuarkIngestFavorite{
		AccountID:      7,
		Link:           "https://pan.quark.cn/s/abc",
		FolderID:       "folder-1",
		ParentFolderID: "inbox",
		FolderName:     "作品",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.ID <= 0 || saved.FolderName != "作品" || saved.ParentFolderID != "inbox" || saved.CreatedAt.IsZero() {
		t.Fatalf("unexpected saved favorite: %+v", saved)
	}

	other, err := repo.Create(ctx, &domain.QuarkIngestFavorite{
		AccountID:      8,
		Link:           "https://pan.quark.cn/s/def",
		FolderID:       "folder-2",
		ParentFolderID: "inbox",
		FolderName:     "其他",
	})
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if other.ID == saved.ID {
		t.Fatal("expected distinct ids")
	}

	list, err := repo.ListByAccount(ctx, 7)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("unexpected list: %+v", list)
	}

	saved.Link = "https://pan.quark.cn/s/updated"
	saved.ParentFolderID = "subfolder"
	saved.FolderName = "新名字"
	updated, err := repo.Update(ctx, saved)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Link != saved.Link || updated.FolderName != "新名字" || updated.ParentFolderID != "subfolder" {
		t.Fatalf("unexpected updated favorite: %+v", updated)
	}

	if err := repo.UpdateFolderName(ctx, saved.ID, "再改名"); err != nil {
		t.Fatalf("update folder name: %v", err)
	}
	list, err = repo.ListByAccount(ctx, 7)
	if err != nil {
		t.Fatalf("list after rename: %v", err)
	}
	if list[0].FolderName != "再改名" {
		t.Fatalf("unexpected folder name: %+v", list[0])
	}

	if err := repo.Delete(ctx, saved.ID, 8); err == nil {
		t.Fatal("expected account mismatch to fail")
	}
	if err := repo.Delete(ctx, saved.ID, 7); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err = repo.ListByAccount(ctx, 7)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %+v", list)
	}
}

// buei:ingest-update —— 夸克收藏的更新时间随收藏一起增删改。
func TestQuarkIngestFavoriteRepoUpdateSchedule(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Memory: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := store.NewQuarkIngestFavoriteRepo(db)
	want := domain.IngestUpdateSchedule{Weekday: 0, Hour: 9, Minute: 30}
	saved, err := repo.Create(ctx, &domain.QuarkIngestFavorite{
		AccountID:      7,
		Link:           "https://pan.quark.cn/s/sched",
		FolderID:       "folder-sched",
		FolderName:     "更新时间测试",
		UpdateSchedule: want,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.UpdateSchedule != want {
		t.Fatalf("更新时间未保存：期望 %+v，实际 %+v", want, saved.UpdateSchedule)
	}

	list, err := repo.ListByAccount(ctx, 7)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].UpdateSchedule != want {
		t.Fatalf("列表未带回更新时间：%+v", list)
	}

	saved.UpdateSchedule = domain.IngestUpdateSchedule{Weekday: domain.IngestUpdateNone}
	updated, err := repo.Update(ctx, saved)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.UpdateSchedule.IsSet() {
		t.Fatalf("更新时间应被清空：%+v", updated.UpdateSchedule)
	}
}
