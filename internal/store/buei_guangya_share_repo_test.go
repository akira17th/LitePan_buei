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
	if links[0].LastDestination != nil {
		t.Fatalf("new link should not have destination: %+v", links[0].LastDestination)
	}

	dest := &domain.GuangyaShareDestination{
		AccountID:     2,
		ParentID:      "dir-1",
		BasePath:      "/媒体/电视剧",
		BaseDirs:      []string{"媒体", "电视剧"},
		Title:         "走马上任 (2026)",
		Season:        1,
		IsSeries:      true,
		AutoStrm:      true,
		AutoScrape:    true,
		ScrapeTaskID:  9,
		RefreshEmby:   true,
		EmbyConfigID:  "emby-1",
		EmbyMode:      "library",
		EmbyLibraryID: "lib-1",
	}
	if err := repo.UpdateDestination(ctx, "1969_abc", dest); err != nil {
		t.Fatalf("update destination: %v", err)
	}
	links, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after destination: %v", err)
	}
	if len(links) != 1 || links[0].LastDestination == nil {
		t.Fatalf("destination was not persisted: %+v", links)
	}
	gotDest := links[0].LastDestination
	if gotDest.AccountID != 2 || gotDest.ParentID != "dir-1" || gotDest.BasePath != "/媒体/电视剧" ||
		gotDest.Title != "走马上任 (2026)" || gotDest.Season != 1 || !gotDest.IsSeries || !gotDest.AutoStrm ||
		!gotDest.AutoScrape || gotDest.ScrapeTaskID != 9 ||
		!gotDest.RefreshEmby || gotDest.EmbyConfigID != "emby-1" || gotDest.EmbyMode != "library" ||
		gotDest.EmbyLibraryID != "lib-1" ||
		len(gotDest.BaseDirs) != 2 || gotDest.BaseDirs[0] != "媒体" || gotDest.BaseDirs[1] != "电视剧" {
		t.Fatalf("unexpected destination: %+v", gotDest)
	}

	browsePath := []domain.GuangyaShareBrowseCrumb{
		{ID: "", Name: "全部文件", Path: ""},
		{ID: "d1", Name: "剧集", Path: "/剧集"},
	}
	if err := repo.UpdateBrowsePath(ctx, "1969_abc", browsePath); err != nil {
		t.Fatalf("update browse path: %v", err)
	}
	links, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after browse path: %v", err)
	}
	if len(links) != 1 || len(links[0].LastBrowsePath) != 2 || links[0].LastBrowsePath[1].Name != "剧集" {
		t.Fatalf("browse path was not persisted: %+v", links)
	}

	replaceRule := domain.GuangyaShareRenameReplace{
		Enabled: true,
		Find:    "\\s+",
		Rules: []domain.GuangyaShareRenameRule{
			{Find: "\\s+", ReplaceWith: " "},
			{Find: "2160p", ReplaceWith: ""},
		},
	}
	if err := repo.UpdateRenameReplace(ctx, "1969_abc", replaceRule); err != nil {
		t.Fatalf("update replace rule: %v", err)
	}
	links, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after replace rule: %v", err)
	}
	if len(links) != 1 || !links[0].RenameReplace.Enabled || links[0].RenameReplace.Find != "\\s+" {
		t.Fatalf("replace rule was not persisted: %+v", links)
	}
	if rules := links[0].RenameReplace.NormalizeRules(); len(rules) != 2 || rules[1].Find != "2160p" {
		t.Fatalf("multi replace rules were not persisted: %+v", links[0].RenameReplace)
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
	if saved.LastDestination == nil || saved.LastDestination.Title != "走马上任 (2026)" {
		t.Fatalf("upsert should preserve destination: %+v", saved.LastDestination)
	}
	if len(saved.LastBrowsePath) != 2 || saved.LastBrowsePath[1].ID != "d1" {
		t.Fatalf("upsert should preserve browse path: %+v", saved.LastBrowsePath)
	}
	if !saved.RenameReplace.Enabled || saved.RenameReplace.ReplaceWith != " " {
		t.Fatalf("upsert should preserve replace rule: %+v", saved.RenameReplace)
	}
	links, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after update: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected one link after upsert, got %d", len(links))
	}

	shareID, err := repo.Delete(ctx, saved.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if shareID != "1969_abc" {
		t.Fatalf("delete share id = %q", shareID)
	}
	links, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected empty list after delete, got %+v", links)
	}
	if _, err := repo.Delete(ctx, saved.ID); err == nil {
		t.Fatal("expected error deleting missing link")
	} else {
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.CodeNotFound {
			t.Fatalf("unexpected delete error: %v", err)
		}
	}

	if err := repo.UpdateDestination(ctx, "missing", dest); err == nil {
		t.Fatal("expected error updating destination for missing link")
	} else {
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.CodeNotFound {
			t.Fatalf("unexpected destination update error: %v", err)
		}
	}
}

func TestGuangyaShareTransferredRepo(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Memory: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	links := store.NewGuangyaShareLinkRepo(db)
	transfers := store.NewGuangyaShareTransferredRepo(db)
	saved, err := links.Upsert(ctx, &domain.GuangyaShareLink{
		ShareID: "1969_transfer", URL: "https://example.com/s/1", Title: "转存测试",
	})
	if err != nil {
		t.Fatalf("upsert link: %v", err)
	}

	err = transfers.UpsertMany(ctx, "1969_transfer", []*domain.GuangyaShareTransferredFile{
		{FileID: "f1", OriginalName: "01.mp4", NewName: "剧名-S01E01-01.mp4", AccountID: 2, TargetParentID: "dir-1"},
		{FileID: "f2", OriginalName: "02.mp4", NewName: "剧名-S01E02-02.mp4", AccountID: 2, TargetParentID: "dir-1"},
	})
	if err != nil {
		t.Fatalf("upsert transfers: %v", err)
	}
	files, err := transfers.ListByShare(ctx, "1969_transfer")
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	if len(files) != 2 || files[0].OriginalName == "" || files[0].NewName == "" {
		t.Fatalf("unexpected transfers: %+v", files)
	}
	for _, file := range files {
		if file.Status != domain.GuangyaShareFileTransferred {
			t.Fatalf("default status = %q, want transferred", file.Status)
		}
	}

	// 同一 share_id + file_id 更新而非新增。
	if err := transfers.UpsertMany(ctx, "1969_transfer", []*domain.GuangyaShareTransferredFile{
		{FileID: "f1", OriginalName: "01.mp4", NewName: "01.mp4", AccountID: 0, Status: domain.GuangyaShareFileDiscarded},
	}); err != nil {
		t.Fatalf("update transfer: %v", err)
	}
	files, _ = transfers.ListByShare(ctx, "1969_transfer")
	if len(files) != 2 {
		t.Fatalf("expected 2 transfers after upsert, got %d", len(files))
	}
	found := false
	for _, file := range files {
		if file.FileID == "f1" && file.Status == domain.GuangyaShareFileDiscarded && file.NewName == "01.mp4" {
			found = true
		}
	}
	if !found {
		t.Fatalf("transfer update not applied: %+v", files)
	}

	// 删除收藏链接后，关联转存历史级联删除。
	if _, err := links.Delete(ctx, saved.ID); err != nil {
		t.Fatalf("delete link: %v", err)
	}
	files, err = transfers.ListByShare(ctx, "1969_transfer")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("transfers should be cascade-deleted, got %+v", files)
	}
}

func TestGuangyaShareLinkRepoFixedOrder(t *testing.T) {
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
	for _, shareID := range []string{"first", "second", "third"} {
		if _, err := repo.Upsert(ctx, &domain.GuangyaShareLink{ShareID: shareID, URL: "https://example.com/" + shareID}); err != nil {
			t.Fatalf("upsert %s: %v", shareID, err)
		}
	}
	// 最近使用时间变化不应改变固定收藏顺序。
	if _, err := repo.Upsert(ctx, &domain.GuangyaShareLink{
		ShareID: "first", URL: "https://example.com/first", LastUsedAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("touch first: %v", err)
	}
	links, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"first", "second", "third"}
	for i, link := range links {
		if link.ShareID != want[i] {
			t.Fatalf("order[%d] = %q, want %q", i, link.ShareID, want[i])
		}
	}
}
