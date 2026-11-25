package embyarchive

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

func TestMediaBaseKey(t *testing.T) {
	cases := []struct{ name, want string }{
		{"aaaa.strm", "aaaa"},
		{"aaaa.nfo", "aaaa"},
		{"aaaa-thumb.jpg", "aaaa"},
		{"AAAA-Poster.JPG", "aaaa"},
		{"aaaa.zh.srt", "aaaa"},
		{"aaaa.chs.ass", "aaaa"},
		{"aaaa.mkv", "aaaa"},
		{"Show.S01E01.1080p.mkv", "show.s01e01.1080p"},
		{"剧集 S01E01.strm", "剧集 s01e01"},
	}
	for _, tc := range cases {
		if got := MediaBaseKey(tc.name); got != tc.want {
			t.Fatalf("MediaBaseKey(%q) = %q，期望 %q", tc.name, got, tc.want)
		}
	}
}

func TestSelectProjectTargets(t *testing.T) {
	entries := []string{"aaaa.mkv", "aaaa.strm", "aaaa.nfo", "aaaa-thumb.jpg", "aaaa-fanart.jpg", "aaaa.zh.srt", "bbbb.strm", "bbbb.nfo"}

	// 勾选视频：本侧连带同组文件。
	keys := associatedKeys(map[string]bool{"aaaa.mkv": true}, IsVideoFileName)
	got := selectProjectTargets(entries, map[string]bool{"aaaa.mkv": true}, keys)
	sort.Strings(got)
	want := []string{"aaaa-fanart.jpg", "aaaa-thumb.jpg", "aaaa.mkv", "aaaa.nfo", "aaaa.strm", "aaaa.zh.srt"}
	if len(got) != len(want) {
		t.Fatalf("选中视频应连带关联文件：%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("选中视频应连带关联文件：%v", got)
		}
	}

	// 勾选 .strm：同样连带同组文件。
	keys = associatedKeys(map[string]bool{"aaaa.strm": true}, IsSTRMFileName)
	got = selectProjectTargets(entries, map[string]bool{"aaaa.strm": true}, keys)
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("选中 STRM 应连带关联文件：%v", got)
	}

	// 勾选其它类型：只删除自己，不产生关联键。
	keys = associatedKeys(map[string]bool{"aaaa.nfo": true}, IsSTRMFileName)
	if len(keys) != 0 {
		t.Fatalf("非主文件不应产生关联键：%v", keys)
	}
	got = selectProjectTargets(entries, map[string]bool{"aaaa.nfo": true}, keys)
	if len(got) != 1 || got[0] != "aaaa.nfo" {
		t.Fatalf("非主文件只删除自己：%v", got)
	}
}

func newProjectDeleteService(t *testing.T, localFiles []string, cloudItems []domain.FileItem) (*Service, string, *fakeFiles, *fakeScrape, *domain.StrmTask) {
	t.Helper()
	strmDir := t.TempDir()
	task := &domain.StrmTask{ID: 9, Name: "剧集", AccountID: 3, ParentID: "root", Path: "/媒体/剧集", OutputFolder: "剧集"}
	projectDir := filepath.Join(strm.TaskOutputDir(strmDir, strm.TaskRelDir(task.GroupDir, task.OutputFolder)), "某剧")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range localFiles {
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := &fakeFiles{tree: map[string][]domain.FileItem{
		"root": {{ID: "p1", Name: "某剧", IsDir: true}},
		"p1":   cloudItems,
	}}
	scrape := &fakeScrape{}
	svc := New(Options{
		Files:   files,
		Strm:    &fakeStrm{tasks: []*domain.StrmTask{task}},
		Scrape:  scrape,
		Emby:    &fakeEmby{libraries: []embyproxy.Library{{ID: "lib-tv", Name: "剧集"}}},
		StrmDir: strmDir,
	})
	return svc, projectDir, files, scrape, task
}

func TestDeleteProjectEntriesExpandsRelatedFilesForSTRM(t *testing.T) {
	svc, projectDir, files, scrape, task := newProjectDeleteService(t,
		[]string{"aaaa.strm", "aaaa.nfo", "aaaa-thumb.jpg", "bbbb.strm"},
		[]domain.FileItem{
			{ID: "c0", Name: "aaaa.mkv"},
			{ID: "c1", Name: "aaaa.strm"},
			{ID: "c2", Name: "aaaa.nfo"},
			{ID: "c3", Name: "aaaa-thumb.jpg"},
			{ID: "c4", Name: "bbbb.mkv"},
			{ID: "c5", Name: "bbbb.strm"},
		})

	result, err := svc.DeleteProjectEntries(context.Background(), ProjectDeleteRequest{
		LibraryID: "lib-tv",
		Task:      task,
		Item:      strmscrape.Item{ID: "9:item", RelDir: "某剧"},
		Names:     []string{"aaaa.strm"},
		Side:      "strm",
	})
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	sort.Strings(files.deletedIDs)
	if len(files.deletedIDs) != 4 || files.deletedIDs[0] != "c0" || files.deletedIDs[3] != "c3" {
		t.Fatalf("勾选 .strm 时应连带删除网盘同组文件: %v", files.deletedIDs)
	}
	for _, name := range []string{"aaaa.strm", "aaaa.nfo", "aaaa-thumb.jpg"} {
		if _, err := os.Stat(filepath.Join(projectDir, name)); !os.IsNotExist(err) {
			t.Fatalf("STRM 侧应连带删除关联文件: %s (%v)", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(projectDir, "bbbb.strm")); err != nil {
		t.Fatalf("无关文件不应被删除: %v", err)
	}
	if result.ProjectRemoved {
		t.Fatalf("项目里仍有 strm，不应删除整个项目: %#v", result)
	}
	if !result.IndexRefresh || !result.EmbyRefresh || len(scrape.refreshed) != 1 {
		t.Fatalf("删除后应刷新索引与媒体库: %#v", result)
	}
}

func TestDeleteProjectEntriesExpandsRelatedFilesForVideo(t *testing.T) {
	svc, projectDir, files, _, task := newProjectDeleteService(t,
		[]string{"aaaa.strm", "aaaa.nfo", "aaaa-thumb.jpg"},
		[]domain.FileItem{
			{ID: "c0", Name: "aaaa.mkv"},
			{ID: "c1", Name: "aaaa.strm"},
			{ID: "c2", Name: "aaaa.nfo"},
			{ID: "c3", Name: "aaaa-thumb.jpg"},
		})

	_, err := svc.DeleteProjectEntries(context.Background(), ProjectDeleteRequest{
		LibraryID: "lib-tv",
		Task:      task,
		Item:      strmscrape.Item{ID: "9:item", RelDir: "某剧"},
		Names:     []string{"aaaa.mkv"},
		Side:      "cloud",
	})
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	// 网盘连带删除同组文件后，本地已无 strm，整个项目文件夹也会被清理。
	sort.Strings(files.deletedIDs)
	if len(files.deletedIDs) != 5 || files.deletedIDs[0] != "c0" || files.deletedIDs[3] != "c3" || files.deletedIDs[4] != "p1" {
		t.Fatalf("选中视频时应连带网盘关联文件: %v", files.deletedIDs)
	}
	// 勾选视频时本地 STRM 侧同样连带删除同组文件。
	for _, name := range []string{"aaaa.strm", "aaaa.nfo", "aaaa-thumb.jpg"} {
		if _, err := os.Stat(filepath.Join(projectDir, name)); !os.IsNotExist(err) {
			t.Fatalf("勾选视频时应连带删除本地关联文件: %s (%v)", name, err)
		}
	}
}

func TestDeleteProjectEntriesRemovesWholeProjectWhenNoSTRMLeft(t *testing.T) {
	strmDir := t.TempDir()
	task := &domain.StrmTask{ID: 11, Name: "电影", AccountID: 3, ParentID: "root", Path: "/媒体/电影", OutputFolder: "电影"}
	projectDir := filepath.Join(strm.TaskOutputDir(strmDir, strm.TaskRelDir(task.GroupDir, task.OutputFolder)), "某片 (2024)")
	if err := os.MkdirAll(filepath.Join(projectDir, "Extras"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"某片 (2024).strm", "某片 (2024).nfo", "某片 (2024)-poster.jpg"} {
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files := &fakeFiles{tree: map[string][]domain.FileItem{
		"root": {{ID: "p1", Name: "某片 (2024)", IsDir: true}},
		"p1": {
			{ID: "c1", Name: "某片 (2024).strm"},
			{ID: "c2", Name: "某片 (2024).nfo"},
			{ID: "c3", Name: "某片 (2024)-poster.jpg"},
		},
	}}
	scrape := &fakeScrape{}
	svc := New(Options{
		Files:   files,
		Strm:    &fakeStrm{tasks: []*domain.StrmTask{task}},
		Scrape:  scrape,
		Emby:    &fakeEmby{libraries: []embyproxy.Library{{ID: "lib-movie", Name: "电影"}}},
		StrmDir: strmDir,
	})

	result, err := svc.DeleteProjectEntries(context.Background(), ProjectDeleteRequest{
		LibraryID: "lib-movie",
		Task:      task,
		Item:      strmscrape.Item{ID: "11:item", RelDir: "某片 (2024)"},
		Names:     []string{"某片 (2024).strm"},
		Side:      "strm",
	})
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if !result.ProjectRemoved {
		t.Fatalf("项目内已没有 strm，应删除整个项目文件夹: %#v", result)
	}
	if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
		t.Fatalf("本地项目文件夹应被完全删除: %v", err)
	}
	// 勾选 .strm 会连带删除网盘同组文件；项目里已无 strm 时整个项目文件夹一并删除。
	sort.Strings(files.deletedIDs)
	want := []string{"c1", "c2", "c3", "p1"}
	if len(files.deletedIDs) != len(want) {
		t.Fatalf("网盘删除项异常: %v", files.deletedIDs)
	}
	for i := range want {
		if files.deletedIDs[i] != want[i] {
			t.Fatalf("网盘删除项异常: %v", files.deletedIDs)
		}
	}
}

func TestIsVideoFileName(t *testing.T) {
	cases := map[string]bool{
		"影片.mkv": true, "A.MP4": true, "b.ts": true, "c.iso": true,
		"d.nfo": false, "e-thumb.jpg": false, "f.srt": false, "g": false,
	}
	for name, want := range cases {
		if got := IsVideoFileName(name); got != want {
			t.Fatalf("IsVideoFileName(%q) = %v，期望 %v", name, got, want)
		}
	}
}

func TestDeleteProjectSTRMsOnlyRemovesSTRM(t *testing.T) {
	strmDir := t.TempDir()
	task := &domain.StrmTask{ID: 13, Name: "剧集", AccountID: 3, ParentID: "root", Path: "/媒体/剧集", OutputFolder: "剧集"}
	projectDir := filepath.Join(strm.TaskOutputDir(strmDir, strm.TaskRelDir(task.GroupDir, task.OutputFolder)), "某剧")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"多余.strm", "正常.strm", "正常.nfo"} {
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scrape := &fakeScrape{}
	svc := New(Options{
		Files:   &fakeFiles{},
		Strm:    &fakeStrm{tasks: []*domain.StrmTask{task}},
		Scrape:  scrape,
		Emby:    &fakeEmby{libraries: []embyproxy.Library{{ID: "lib-tv", Name: "剧集"}}},
		StrmDir: strmDir,
	})
	result, err := svc.DeleteProjectSTRMs(context.Background(), ProjectSTRMDeleteRequest{
		LibraryID: "lib-tv",
		Task:      task,
		Item:      strmscrape.Item{ID: "13:item", RelDir: "某剧"},
		Names:     []string{"多余.strm"},
	})
	if err != nil {
		t.Fatalf("删除多余 STRM 失败: %v", err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != "多余.strm" {
		t.Fatalf("删除结果异常: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "多余.strm")); !os.IsNotExist(err) {
		t.Fatalf("多余 STRM 应被删除: %v", err)
	}
	for _, name := range []string{"正常.strm", "正常.nfo"} {
		if _, err := os.Stat(filepath.Join(projectDir, name)); err != nil {
			t.Fatalf("其它文件不应被删除: %s (%v)", name, err)
		}
	}
	if !result.IndexRefresh || !result.EmbyRefresh || len(scrape.refreshed) != 1 {
		t.Fatalf("删除后应刷新索引与媒体库: %#v", result)
	}
}

func TestDeleteProjectEntriesOtherFilesOnlyDeleteOwnSide(t *testing.T) {
	svc, projectDir, files, _, task := newProjectDeleteService(t,
		[]string{"aaaa.strm", "aaaa.nfo", "aaaa-thumb.jpg"},
		[]domain.FileItem{
			{ID: "c0", Name: "aaaa.mkv"},
			{ID: "c1", Name: "aaaa.strm"},
			{ID: "c2", Name: "aaaa.nfo"},
			{ID: "c3", Name: "aaaa-thumb.jpg"},
		})

	// 从 STRM 侧勾选 nfo：只删本地 nfo，网盘完全不动。
	if _, err := svc.DeleteProjectEntries(context.Background(), ProjectDeleteRequest{
		LibraryID: "lib-tv",
		Task:      task,
		Item:      strmscrape.Item{ID: "9:item", RelDir: "某剧"},
		Names:     []string{"aaaa.nfo"},
		Side:      "strm",
	}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if len(files.deletedIDs) != 0 {
		t.Fatalf("STRM 侧删 nfo 不应动网盘: %v", files.deletedIDs)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "aaaa.nfo")); !os.IsNotExist(err) {
		t.Fatalf("本地 nfo 应被删除: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "aaaa.strm")); err != nil {
		t.Fatalf("本地 strm 不应被删除: %v", err)
	}

	// 从网盘侧勾选 nfo：只删网盘 nfo，本地完全不动。
	if _, err := svc.DeleteProjectEntries(context.Background(), ProjectDeleteRequest{
		LibraryID: "lib-tv",
		Task:      task,
		Item:      strmscrape.Item{ID: "9:item", RelDir: "某剧"},
		Names:     []string{"aaaa.nfo"},
		Side:      "cloud",
	}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if len(files.deletedIDs) != 1 || files.deletedIDs[0] != "c2" {
		t.Fatalf("网盘侧删 nfo 只应删该文件: %v", files.deletedIDs)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "aaaa.strm")); err != nil {
		t.Fatalf("本地文件不应被删除: %v", err)
	}
}
