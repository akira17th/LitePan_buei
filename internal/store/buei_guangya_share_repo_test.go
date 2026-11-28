package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/store"
)

func newGuangyaShareTestDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Memory: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestGuangyaShareLinkRepoCRUD(t *testing.T) {
	ctx := context.Background()
	db := newGuangyaShareTestDB(t)
	repo := store.NewGuangyaShareLinkRepo(db)

	firstTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	saved, err := repo.Create(ctx, &domain.GuangyaShareLink{
		ShareID:    "1969_abc",
		URL:        "https://www.guangyapan.com/s/1969_abc",
		Title:      "测试分享",
		Owner:      "光鸭用户",
		LastUsedAt: firstTime,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.ID <= 0 || saved.Title != "测试分享" || !saved.LastUsedAt.Equal(firstTime) {
		t.Fatalf("unexpected saved link: %+v", saved)
	}
	if got, err := repo.Get(ctx, saved.ID); err != nil || got.ShareID != "1969_abc" {
		t.Fatalf("get: %+v, %v", got, err)
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
	if err := repo.UpdateDestination(ctx, saved.ID, dest); err != nil {
		t.Fatalf("update destination: %v", err)
	}
	got, err := repo.Get(ctx, saved.ID)
	if err != nil || got.LastDestination == nil {
		t.Fatalf("destination was not persisted: %+v, %v", got, err)
	}
	gotDest := got.LastDestination
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
	if err := repo.UpdateBrowsePath(ctx, saved.ID, browsePath); err != nil {
		t.Fatalf("update browse path: %v", err)
	}
	got, err = repo.Get(ctx, saved.ID)
	if err != nil || len(got.LastBrowsePath) != 2 || got.LastBrowsePath[1].Name != "剧集" {
		t.Fatalf("browse path was not persisted: %+v, %v", got, err)
	}

	replaceRule := domain.GuangyaShareRenameReplace{
		Enabled: true,
		Find:    "\\s+",
		Rules: []domain.GuangyaShareRenameRule{
			{Find: "\\s+", ReplaceWith: " "},
			{Find: "2160p", ReplaceWith: ""},
		},
	}
	if err := repo.UpdateRenameReplace(ctx, saved.ID, replaceRule); err != nil {
		t.Fatalf("update replace rule: %v", err)
	}
	got, err = repo.Get(ctx, saved.ID)
	if err != nil || !got.RenameReplace.Enabled || got.RenameReplace.Find != "\\s+" {
		t.Fatalf("replace rule was not persisted: %+v, %v", got, err)
	}
	if rules := got.RenameReplace.NormalizeRules(); len(rules) != 2 || rules[1].Find != "2160p" {
		t.Fatalf("multi replace rules were not persisted: %+v", got.RenameReplace)
	}

	if err := repo.Touch(ctx, saved.ID); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, err = repo.Get(ctx, saved.ID)
	if err != nil {
		t.Fatalf("get after touch: %v", err)
	}
	if !got.LastUsedAt.After(firstTime) {
		t.Fatalf("last used not updated: %+v", got.LastUsedAt)
	}

	if err := repo.Delete(ctx, saved.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, saved.ID); err == nil {
		t.Fatal("expected error getting deleted link")
	} else {
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.CodeNotFound {
			t.Fatalf("unexpected get error: %v", err)
		}
	}
	if err := repo.Delete(ctx, saved.ID); err == nil {
		t.Fatal("expected error deleting missing link")
	} else {
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.CodeNotFound {
			t.Fatalf("unexpected delete error: %v", err)
		}
	}

	if err := repo.UpdateDestination(ctx, 99999, dest); err == nil {
		t.Fatal("expected error updating destination for missing link")
	} else {
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.CodeNotFound {
			t.Fatalf("unexpected destination update error: %v", err)
		}
	}
}

func TestGuangyaShareLinkRepoAllowsDuplicateShareID(t *testing.T) {
	ctx := context.Background()
	db := newGuangyaShareTestDB(t)
	repo := store.NewGuangyaShareLinkRepo(db)
	transfers := store.NewGuangyaShareTransferredRepo(db)

	first, err := repo.Create(ctx, &domain.GuangyaShareLink{
		ShareID: "1969_dup", URL: "https://example.com/first", Title: "第一条",
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := repo.Create(ctx, &domain.GuangyaShareLink{
		ShareID: "1969_dup", URL: "https://example.com/second", Title: "第二条",
	})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("duplicate share should create distinct links: %+v %+v", first, second)
	}
	links, err := repo.List(ctx)
	if err != nil || len(links) != 2 {
		t.Fatalf("duplicate links not persisted: %+v, %v", links, err)
	}

	dest := &domain.GuangyaShareDestination{AccountID: 2, ParentID: "first-dir", Title: "第一条目标", IsSeries: true}
	if err := repo.UpdateDestination(ctx, first.ID, dest); err != nil {
		t.Fatalf("update first destination: %v", err)
	}
	firstGot, _ := repo.Get(ctx, first.ID)
	secondGot, _ := repo.Get(ctx, second.ID)
	if firstGot.LastDestination == nil || firstGot.LastDestination.Title != "第一条目标" {
		t.Fatalf("first destination missing: %+v", firstGot.LastDestination)
	}
	if secondGot.LastDestination != nil {
		t.Fatalf("second destination should stay empty: %+v", secondGot.LastDestination)
	}

	if err := transfers.UpsertMany(ctx, first.ID, []*domain.GuangyaShareTransferredFile{{FileID: "f1", OriginalName: "01.mp4"}}); err != nil {
		t.Fatalf("insert first transfer: %v", err)
	}
	firstFiles, _ := transfers.ListByLink(ctx, first.ID)
	secondFiles, _ := transfers.ListByLink(ctx, second.ID)
	if len(firstFiles) != 1 || len(secondFiles) != 0 {
		t.Fatalf("transfer history leaked between duplicates: first=%+v second=%+v", firstFiles, secondFiles)
	}

	if err := repo.Delete(ctx, first.ID); err != nil {
		t.Fatalf("delete first: %v", err)
	}
	links, _ = repo.List(ctx)
	if len(links) != 1 || links[0].ID != second.ID {
		t.Fatalf("unexpected links after deleting first: %+v", links)
	}
	firstFiles, _ = transfers.ListByLink(ctx, first.ID)
	if len(firstFiles) != 0 {
		t.Fatalf("first transfer history should be deleted: %+v", firstFiles)
	}
}

func TestGuangyaShareLinkRepoUpdateIdentity(t *testing.T) {
	ctx := context.Background()
	db := newGuangyaShareTestDB(t)
	repo := store.NewGuangyaShareLinkRepo(db)
	saved, err := repo.Create(ctx, &domain.GuangyaShareLink{
		ShareID: "1969_old", URL: "https://www.guangyapan.com/s/1969_old",
		Title: "旧标题", Owner: "旧用户",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	dest := &domain.GuangyaShareDestination{
		AccountID: 2, ParentID: "dir-1", BasePath: "/媒体", Title: "剧名 (2026)",
		Season: 1, IsSeries: true, AutoStrm: true, AutoScrape: true, RefreshEmby: true,
		EmbyConfigID: "emby-1", EmbyMode: "library", EmbyLibraryID: "lib-1",
	}
	if err := repo.UpdateDestination(ctx, saved.ID, dest); err != nil {
		t.Fatalf("update destination: %v", err)
	}
	rule := domain.GuangyaShareRenameReplace{Enabled: true, Find: "old", ReplaceWith: "new"}
	if err := repo.UpdateRenameReplace(ctx, saved.ID, rule); err != nil {
		t.Fatalf("update rename: %v", err)
	}
	if err := repo.UpdateBrowsePath(ctx, saved.ID, []domain.GuangyaShareBrowseCrumb{{ID: "d1", Name: "目录", Path: "/目录"}}); err != nil {
		t.Fatalf("update browse path: %v", err)
	}
	transfers := store.NewGuangyaShareTransferredRepo(db)
	if err := transfers.UpsertMany(ctx, saved.ID, []*domain.GuangyaShareTransferredFile{{FileID: "f1", OriginalName: "01.mp4"}}); err != nil {
		t.Fatalf("insert transferred file: %v", err)
	}

	updated, err := repo.UpdateIdentity(ctx, saved.ID, "1969_new", "https://www.guangyapan.com/s/1969_new", "pwd", "新标题", "新用户", true)
	if err != nil {
		t.Fatalf("update identity: %v", err)
	}
	if updated.ID != saved.ID || updated.ShareID != "1969_new" || updated.Title != "新标题" || updated.Owner != "新用户" || updated.Password != "pwd" {
		t.Fatalf("unexpected identity: %+v", updated)
	}
	if updated.LastDestination == nil || updated.LastDestination.Title != "剧名 (2026)" || !updated.LastDestination.AutoStrm {
		t.Fatalf("destination not preserved: %+v", updated.LastDestination)
	}
	if !updated.RenameReplace.Enabled || updated.RenameReplace.Find != "old" {
		t.Fatalf("rename rule not preserved: %+v", updated.RenameReplace)
	}
	if len(updated.LastBrowsePath) != 0 {
		t.Fatalf("browse path should be cleared: %+v", updated.LastBrowsePath)
	}
	if updated.CreatedAt.IsZero() || updated.UpdatedAt.IsZero() || updated.LastUsedAt.IsZero() {
		t.Fatalf("timestamps missing: %+v", updated)
	}
	oldFiles, err := transfers.ListByLink(ctx, saved.ID)
	if err != nil {
		t.Fatalf("list old transferred files: %v", err)
	}
	if len(oldFiles) != 0 {
		t.Fatalf("old transferred files should be cleared: %+v", oldFiles)
	}
}

func TestGuangyaShareTransferredRepo(t *testing.T) {
	ctx := context.Background()
	db := newGuangyaShareTestDB(t)
	links := store.NewGuangyaShareLinkRepo(db)
	transfers := store.NewGuangyaShareTransferredRepo(db)
	saved, err := links.Create(ctx, &domain.GuangyaShareLink{
		ShareID: "1969_transfer", URL: "https://example.com/s/1", Title: "转存测试",
	})
	if err != nil {
		t.Fatalf("create link: %v", err)
	}

	err = transfers.UpsertMany(ctx, saved.ID, []*domain.GuangyaShareTransferredFile{
		{FileID: "f1", OriginalName: "01.mp4", NewName: "剧名-S01E01-01.mp4", AccountID: 2, TargetParentID: "dir-1"},
		{FileID: "f2", OriginalName: "02.mp4", NewName: "剧名-S01E02-02.mp4", AccountID: 2, TargetParentID: "dir-1"},
	})
	if err != nil {
		t.Fatalf("upsert transfers: %v", err)
	}
	files, err := transfers.ListByLink(ctx, saved.ID)
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

	// 同一收藏 + file_id 更新而非新增。
	if err := transfers.UpsertMany(ctx, saved.ID, []*domain.GuangyaShareTransferredFile{
		{FileID: "f1", OriginalName: "01.mp4", NewName: "01.mp4", AccountID: 0, Status: domain.GuangyaShareFileDiscarded},
	}); err != nil {
		t.Fatalf("update transfer: %v", err)
	}
	files, _ = transfers.ListByLink(ctx, saved.ID)
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
	if err := links.Delete(ctx, saved.ID); err != nil {
		t.Fatalf("delete link: %v", err)
	}
	files, err = transfers.ListByLink(ctx, saved.ID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("transfers should be cascade-deleted, got %+v", files)
	}
}

func TestGuangyaShareLinkRepoFixedOrder(t *testing.T) {
	ctx := context.Background()
	db := newGuangyaShareTestDB(t)
	repo := store.NewGuangyaShareLinkRepo(db)
	var firstID int64
	for _, shareID := range []string{"first", "second", "third"} {
		saved, err := repo.Create(ctx, &domain.GuangyaShareLink{ShareID: shareID, URL: "https://example.com/" + shareID})
		if err != nil {
			t.Fatalf("create %s: %v", shareID, err)
		}
		if shareID == "first" {
			firstID = saved.ID
		}
	}
	// 最近使用时间变化不应改变固定收藏顺序。
	if err := repo.Touch(ctx, firstID); err != nil {
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

// buei:ingest-update —— 收藏更新时间默认不设置，保存 / 清空都要能读回。
func TestGuangyaShareLinkRepoUpdateSchedule(t *testing.T) {
	ctx := context.Background()
	db := newGuangyaShareTestDB(t)
	repo := store.NewGuangyaShareLinkRepo(db)

	saved, err := repo.Create(ctx, &domain.GuangyaShareLink{ShareID: "1969_sched", Title: "更新时间测试"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.UpdateSchedule.IsSet() {
		t.Fatalf("新收藏不应设置更新时间：%+v", saved.UpdateSchedule)
	}

	want := domain.IngestUpdateSchedule{Weekday: 3, Hour: 21, Minute: 5}
	if err := repo.UpdateSchedule(ctx, saved.ID, want); err != nil {
		t.Fatalf("update schedule: %v", err)
	}
	got, err := repo.Get(ctx, saved.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.UpdateSchedule != want {
		t.Fatalf("更新时间未保存：期望 %+v，实际 %+v", want, got.UpdateSchedule)
	}

	if err := repo.UpdateSchedule(ctx, saved.ID, domain.IngestUpdateSchedule{Weekday: domain.IngestUpdateNone}); err != nil {
		t.Fatalf("clear schedule: %v", err)
	}
	cleared, err := repo.Get(ctx, saved.ID)
	if err != nil {
		t.Fatalf("get cleared: %v", err)
	}
	if cleared.UpdateSchedule.IsSet() || cleared.UpdateSchedule.Weekday != domain.IngestUpdateNone {
		t.Fatalf("更新时间应被清空：%+v", cleared.UpdateSchedule)
	}

	if err := repo.UpdateSchedule(ctx, 0, want); err == nil {
		t.Fatal("收藏 ID 为 0 时应报错")
	}
}
