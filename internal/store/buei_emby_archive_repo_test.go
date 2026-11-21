package store_test

import (
	"context"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/store"
)

func TestBueiEmbyArchiveRuleEnabled(t *testing.T) {
	ctx := context.Background()
	db := newGuangyaShareTestDB(t)
	repo := store.NewBueiEmbyArchiveRuleRepo(db)

	saved, err := repo.Save(ctx, &domain.BueiEmbyArchiveRule{
		ArchiveLibraryID:   "arch-movie",
		ArchiveLibraryName: "归档 · 电影",
		IntervalHours:      1,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !saved.Enabled {
		t.Fatal("启用状态未保存")
	}

	disabled, err := repo.Save(ctx, &domain.BueiEmbyArchiveRule{
		ArchiveLibraryID:   "arch-movie",
		ArchiveLibraryName: "归档 · 电影",
		IntervalHours:      1,
		Enabled:            false,
	})
	if err != nil {
		t.Fatalf("save disabled: %v", err)
	}
	if disabled.Enabled {
		t.Fatal("关闭状态未保存")
	}
}

func TestBueiEmbyArchiveRunPruneByTrigger(t *testing.T) {
	ctx := context.Background()
	db := newGuangyaShareTestDB(t)
	repo := store.NewBueiEmbyArchiveRunRepo(db)

	first, err := repo.Create(ctx, &domain.BueiEmbyArchiveRun{ArchiveLibraryID: "arch", Trigger: domain.BueiArchiveTriggerManual, Status: domain.BueiArchiveRunSuccess})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := repo.Create(ctx, &domain.BueiEmbyArchiveRun{ArchiveLibraryID: "arch", Trigger: domain.BueiArchiveTriggerManual, Status: domain.BueiArchiveRunSuccess})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := repo.Create(ctx, &domain.BueiEmbyArchiveRun{ArchiveLibraryID: "arch", Trigger: domain.BueiArchiveTriggerAuto, Status: domain.BueiArchiveRunSuccess}); err != nil {
		t.Fatalf("create auto: %v", err)
	}

	if err := repo.PruneByTrigger(ctx, "arch", domain.BueiArchiveTriggerManual, second.ID); err != nil {
		t.Fatalf("prune: %v", err)
	}
	runs, err := repo.ListByLibrary(ctx, "arch", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("手动/自动各保留一条，实际 %d 条：%+v", len(runs), runs)
	}
	for _, run := range runs {
		if run.Trigger == domain.BueiArchiveTriggerManual && run.ID != second.ID {
			t.Fatalf("手动记录应只保留最新一条（%d），实际 %d", second.ID, run.ID)
		}
		if run.Trigger == domain.BueiArchiveTriggerManual && run.ID == first.ID {
			t.Fatalf("旧的手动记录应被删除")
		}
	}
}
