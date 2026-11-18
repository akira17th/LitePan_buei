package guangyashare

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"litepan/internal/core/driverexec"
	"litepan/internal/domain"
	"litepan/internal/driver"
)

type fakeRenameProvider struct{ drv driver.Driver }

func (p *fakeRenameProvider) Get(context.Context, int64) (driver.Driver, error) {
	return p.drv, nil
}

type fakeRenameDriver struct {
	items   []domain.FileItem
	renamed map[string]string
	failOn  map[string]bool
}

func (d *fakeRenameDriver) Config() driver.Config { return driver.Config{Name: "fake"} }
func (d *fakeRenameDriver) GetAddition() any      { return &struct{}{} }
func (d *fakeRenameDriver) Init(context.Context) error {
	return nil
}
func (d *fakeRenameDriver) Drop(context.Context) error {
	return nil
}
func (d *fakeRenameDriver) Ping(context.Context) error {
	return nil
}
func (d *fakeRenameDriver) ListFiles(context.Context, string) ([]domain.FileItem, error) {
	return d.items, nil
}
func (d *fakeRenameDriver) RenameFile(_ context.Context, fileID, newName string) error {
	if d.failOn[fileID] {
		return fmt.Errorf("rename failed")
	}
	d.renamed[fileID] = newName
	return nil
}

var (
	_ driver.Driver  = (*fakeRenameDriver)(nil)
	_ driver.Renamer = (*fakeRenameDriver)(nil)
)

func newRenameTestService(drv *fakeRenameDriver) *Service {
	return &Service{
		exec: driverexec.New(&fakeRenameProvider{drv: drv}, nil),
		log:  slog.Default(),
	}
}

func TestNormalizeTransferFiles(t *testing.T) {
	files := normalizeTransferFiles([]TransferFile{
		{FileID: "f1", OriginalName: " 01.mp4 ", NewName: " 走马上任-S01E01-01.mp4 "},
		{FileID: "f1", OriginalName: "01.mp4", NewName: "重复项.mp4"},
		{FileID: "", OriginalName: "空ID.mp4", NewName: "x.mp4"},
		{FileID: "f2", OriginalName: "02.mp4", NewName: "02.mp4"},
		{FileID: "f3", OriginalName: "03.mp4", NewName: "a/b.mp4"},
		{FileID: "d1", OriginalName: "目录", NewName: "改名目录", IsDir: true},
	})
	if len(files) != 4 {
		t.Fatalf("normalizeTransferFiles() = %#v, want 4 items", files)
	}
	if files[0].FileID != "f1" || files[0].NewName != "走马上任-S01E01-01.mp4" {
		t.Fatalf("unexpected first file: %#v", files[0])
	}
	if files[2].NewName != "a-b.mp4" {
		t.Fatalf("unexpected sanitized name: %#v", files[2])
	}
	if files[3].IsDir != true || files[3].NewName != "目录" {
		t.Fatalf("directories must keep original name: %#v", files[3])
	}
	plan := buildRenamePlan(files)
	if len(plan) != 2 || plan[0].FileName != "01.mp4" || plan[1].FileName != "03.mp4" {
		t.Fatalf("unexpected rename plan: %#v", plan)
	}
}

func TestApplyRenames(t *testing.T) {
	drv := &fakeRenameDriver{
		items: []domain.FileItem{
			{ID: "f1", Name: "01.mp4"},
			{ID: "f2", Name: "02.mp4"},
			{ID: "f3", Name: "03.mp4"},
			{ID: "d1", Name: "剧集", IsDir: true},
		},
		renamed: map[string]string{},
		failOn:  map[string]bool{"f2": true},
	}
	svc := newRenameTestService(drv)
	invalidated := ""
	svc.invalidateDir = func(_ int64, parentID string) { invalidated = parentID }

	renamed, skipped := svc.applyRenames(context.Background(), 1, "dir-1", []RenamePair{
		{FileName: "01.mp4", NewName: "走马上任-S01E01-01.mp4"},
		{FileName: "02.mp4", NewName: "走马上任-S01E02-02.mp4"},
		{FileName: "99.mp4", NewName: "不存在.mp4"},
	})
	if renamed != 1 || skipped != 2 {
		t.Fatalf("applyRenames() = (%d, %d), want (1, 2)", renamed, skipped)
	}
	if drv.renamed["f1"] != "走马上任-S01E01-01.mp4" {
		t.Fatalf("f1 rename = %q", drv.renamed["f1"])
	}
	if _, ok := drv.renamed["d1"]; ok {
		t.Fatal("directories must not be renamed")
	}
	if invalidated != "" {
		t.Fatalf("applyRenames should not touch cache, got %q", invalidated)
	}

	// 全部文件已就位时 renameAfterRestore 立即重命名并失效目录缓存。
	renamed, skipped = svc.renameAfterRestore(context.Background(), 1, "dir-1", []RenamePair{
		{FileName: "03.mp4", NewName: "走马上任-S01E03-03.mp4"},
	})
	if renamed != 1 || skipped != 0 {
		t.Fatalf("renameAfterRestore() = (%d, %d), want (1, 0)", renamed, skipped)
	}
	if invalidated != "dir-1" {
		t.Fatalf("invalidateDir called with %q", invalidated)
	}
}

func TestAllNamesPresent(t *testing.T) {
	plan := []RenamePair{{FileName: "a.mp4"}, {FileName: "a.mp4"}, {FileName: "b.mp4"}}
	if allNamesPresent(map[string]int{"a.mp4": 1, "b.mp4": 1}, plan) {
		t.Fatal("duplicate name should require two occurrences")
	}
	if !allNamesPresent(map[string]int{"a.mp4": 2, "b.mp4": 1}, plan) {
		t.Fatal("all names present should return true")
	}
}

type fakeAccountRepo struct{ account *domain.Account }

func (r *fakeAccountRepo) Create(context.Context, *domain.Account) (int64, error) {
	return 0, nil
}
func (r *fakeAccountRepo) Update(context.Context, *domain.Account) error { return nil }
func (r *fakeAccountRepo) Delete(context.Context, int64) error           { return nil }
func (r *fakeAccountRepo) Get(context.Context, int64) (*domain.Account, error) {
	return r.account, nil
}
func (r *fakeAccountRepo) List(context.Context) ([]*domain.Account, error) {
	return []*domain.Account{r.account}, nil
}
func (r *fakeAccountRepo) SetDefault(context.Context, int64) error { return nil }
func (r *fakeAccountRepo) NameTaken(context.Context, string, int64) (bool, error) {
	return false, nil
}

type fakeTransferDriver struct {
	*fakeRenameDriver
	completed bool
	restored  bool
}

func (d *fakeTransferDriver) RestoreShare(context.Context, driver.GuangyaShareRestoreRequest) (string, bool, error) {
	if !d.restored {
		d.items = append(d.items,
			domain.FileItem{ID: "t1", Name: "01.mp4"},
			domain.FileItem{ID: "t2", Name: "02.mp4"},
		)
		d.restored = true
	}
	return "task-1", d.completed, nil
}

var _ driver.GuangyaShareRestorer = (*fakeTransferDriver)(nil)

func TestTransferAppliesRenames(t *testing.T) {
	shareSvc := newTestService(t, fakeShareAPI(t))
	drv := &fakeTransferDriver{
		fakeRenameDriver: &fakeRenameDriver{
			renamed: map[string]string{},
			failOn:  map[string]bool{},
		},
		completed: true,
	}
	svc := &Service{
		client:   shareSvc.client,
		exec:     driverexec.New(&fakeRenameProvider{drv: drv}, nil),
		accounts: &fakeAccountRepo{account: &domain.Account{ID: 2, DriverType: "guangya", IsActive: true}},
		log:      slog.Default(),
	}
	invalidated := ""
	svc.invalidateDir = func(_ int64, parentID string) { invalidated = parentID }

	res, err := svc.Transfer(context.Background(), TransferInput{
		URL:       "https://www.guangyapan.com/s/" + testShareID,
		ShareID:   testShareID,
		Password:  testPassword,
		FileIDs:   []string{"f1"},
		AccountID: 2,
		ParentID:  "root",
		Files: []TransferFile{
			{FileID: "f1", OriginalName: "01.mp4", NewName: "走马上任-S01E01-01.mp4"},
			{FileID: "f2", OriginalName: "02.mp4", NewName: "走马上任-S01E02-02.mp4"},
		},
	})
	if err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if !res.Completed || res.Renamed != 2 || res.RenameSkipped != 0 || res.RenameScheduled {
		t.Fatalf("unexpected result: %+v", res)
	}
	if drv.renamed["t1"] != "走马上任-S01E01-01.mp4" || drv.renamed["t2"] != "走马上任-S01E02-02.mp4" {
		t.Fatalf("unexpected renamed map: %#v", drv.renamed)
	}
	if invalidated != "root" {
		t.Fatalf("invalidateDir called with %q", invalidated)
	}
}
