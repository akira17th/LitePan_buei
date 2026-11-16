package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/store"
)

func TestGuangyaShareLinkRepoCRUD(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Memory: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := store.NewGuangyaShareLinkRepo(db)
	firstTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	saved, err := repo.Upsert(ctx, &domain.GuangyaShareLink{
		ShareID:    "1969_abc",
		URL:        "https://www.guangyapan.com/s/1969_abc",
		Password:   "",
		Title:      "测试分享",
		Owner:      "光鸭用户",
		LastUsedAt: firstTime,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if saved.ID <= 0 || saved.Title != "测试分享" || !saved.LastUsedAt.Equal(firstTime) {
		t.Fatalf("unexpected saved link: %+v", saved)
	}

	links, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(links) != 1 || links[0].ShareID != "1969_abc" {
		t.Fatalf("unexpected list: %+v", links)
	}

	// 同一 share_id 再次收藏应更新而不是新增。
	secondTime := firstTime.Add(time.Hour)
	saved, err = repo.Upsert(ctx, &domain.GuangyaShareLink{
		ShareID:    "1969_abc",
		URL:        "https://www.guangyapan.com/s/1969_abc?code=abcd",
		Password:   "abcd",
		Title:      "新标题",
		Owner:      "光鸭用户2",
		LastUsedAt: secondTime,
	})
	if err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	if saved.Title != "新标题" || saved.Password != "abcd" || !saved.LastUsedAt.Equal(secondTime) {
		t.Fatalf("unexpected updated link: %+v", saved)
	}
	links, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after update: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected one link after upsert, got %d", len(links))
	}

	if err := repo.Delete(ctx, saved.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	links, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected empty list after delete, got %+v", links)
	}
	if err := repo.Delete(ctx, saved.ID); err == nil {
		t.Fatal("expected error deleting missing link")
	} else {
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.CodeNotFound {
			t.Fatalf("unexpected delete error: %v", err)
		}
	}
}
