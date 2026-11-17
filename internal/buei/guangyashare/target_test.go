package guangyashare

import (
	"context"
	"fmt"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

type fakeFolderDriver struct {
	children map[string][]domain.FileItem
	nextID   int
	created  []string
}

func (d *fakeFolderDriver) Config() driver.Config { return driver.Config{Name: "fake"} }
func (d *fakeFolderDriver) GetAddition() any      { return &struct{}{} }
func (d *fakeFolderDriver) Init(context.Context) error {
	return nil
}
func (d *fakeFolderDriver) Drop(context.Context) error {
	return nil
}
func (d *fakeFolderDriver) Ping(context.Context) error {
	return nil
}

func (d *fakeFolderDriver) ListFiles(_ context.Context, parentID string) ([]domain.FileItem, error) {
	return d.children[parentID], nil
}

func (d *fakeFolderDriver) CreateFolder(_ context.Context, parentID, name string) (*domain.FileItem, error) {
	d.nextID++
	item := domain.FileItem{ID: fmt.Sprintf("new-%d", d.nextID), Name: name, IsDir: true}
	d.children[parentID] = append(d.children[parentID], item)
	d.created = append(d.created, name)
	return &item, nil
}

var (
	_ driver.Driver        = (*fakeFolderDriver)(nil)
	_ driver.FolderCreator = (*fakeFolderDriver)(nil)
)

func TestEnsureTargetFolderReusesAndCreates(t *testing.T) {
	drv := &fakeFolderDriver{children: map[string][]domain.FileItem{
		"root": {
			{ID: "show", Name: "剧名", IsDir: true},
			{ID: "movie", Name: "电影", IsDir: false},
		},
		"show": {
			{ID: "season-1", Name: "Season 01", IsDir: true},
		},
	}}

	got, err := ensureTargetFolder(context.Background(), drv, "root", []string{"剧名", "Season 01"})
	if err != nil {
		t.Fatalf("ensureTargetFolder() error = %v", err)
	}
	if got != "season-1" {
		t.Fatalf("ensureTargetFolder() = %q, want season-1", got)
	}
	if len(drv.created) != 0 {
		t.Fatalf("existing folders should not be recreated, created = %#v", drv.created)
	}

	got, err = ensureTargetFolder(context.Background(), drv, "root", []string{"剧名", "Season 02"})
	if err != nil {
		t.Fatalf("ensureTargetFolder() create error = %v", err)
	}
	if got != "new-1" {
		t.Fatalf("ensureTargetFolder() = %q, want new-1", got)
	}
	if len(drv.created) != 1 || drv.created[0] != "Season 02" {
		t.Fatalf("unexpected created folders: %#v", drv.created)
	}

	if _, err := ensureTargetFolder(context.Background(), drv, "root", []string{"电影"}); err == nil {
		t.Fatal("existing file with the same name should fail")
	}
}
