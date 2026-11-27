package guangyashare

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"litepan/internal/core/driverexec"
	"litepan/internal/domain"
	"litepan/internal/driver"
)

// fakeMagnetDriver 模拟光鸭驱动的离线下载/列目录/建目录能力。
type fakeMagnetDriver struct {
	fakeFolderDriver
	added      []string
	statuses   []string // 每次刷新返回的状态序列
	refreshN   int
	moved      []fakeMagnetMove
	renamed    map[string]string
	deletedIDs []string
}

func (d *fakeMagnetDriver) AddOfflineURLs(_ context.Context, req driver.OfflineURLRequest) ([]driver.OfflineAddResult, error) {
	d.added = append(d.added, req.URLs...)
	// 模拟离线完成后的落盘：文件出现在离线目录里。
	d.children[req.ParentID] = append(d.children[req.ParentID],
		domain.FileItem{ID: "f1", Name: "Rick.and.Morty.S09E05.mkv", Size: 123},
	)
	return []driver.OfflineAddResult{{
		Source:         req.URLs[0],
		ProviderTaskID: "task-1",
		Success:        true,
		Message:        "已提交到光鸭云盘",
	}}, nil
}

func (d *fakeMagnetDriver) RefreshOfflineTasks(context.Context, []driver.OfflineTaskRef) ([]driver.OfflineTaskUpdate, error) {
	d.refreshN++
	status := driver.OfflineStatusRunning
	if len(d.statuses) > 0 {
		idx := d.refreshN - 1
		if idx >= len(d.statuses) {
			idx = len(d.statuses) - 1
		}
		status = d.statuses[idx]
	}
	if status == driver.OfflineStatusFailed {
		return []driver.OfflineTaskUpdate{{
			ProviderTaskID: "task-1", Status: status, Message: "光鸭 API 错误(352)：BT文件不存在",
		}}, nil
	}
	update := driver.OfflineTaskUpdate{ProviderTaskID: "task-1", Status: status}
	if status == driver.OfflineStatusSuccess {
		update.Progress = 100
	}
	return []driver.OfflineTaskUpdate{update}, nil
}

func (d *fakeMagnetDriver) MoveFiles(_ context.Context, fileIDs []string, targetParentID, sourceParentID string) error {
	d.moved = append(d.moved, fakeMagnetMove{fileIDs: fileIDs, targetParentID: targetParentID, sourceParentID: sourceParentID})
	// 模拟真实移动：把文件从源目录搬到目标目录，后续重命名才能找到它们。
	moved := map[string]bool{}
	for _, id := range fileIDs {
		moved[id] = true
	}
	remaining := d.children[sourceParentID][:0]
	for _, item := range d.children[sourceParentID] {
		if moved[item.ID] {
			d.children[targetParentID] = append(d.children[targetParentID], item)
			continue
		}
		remaining = append(remaining, item)
	}
	d.children[sourceParentID] = remaining
	return nil
}

func (d *fakeMagnetDriver) DeleteFiles(_ context.Context, fileIDs []string) error {
	for _, id := range fileIDs {
		for parent, items := range d.children {
			remaining := items[:0]
			for _, item := range items {
				if item.ID == id {
					continue
				}
				remaining = append(remaining, item)
			}
			d.children[parent] = remaining
		}
		d.deletedIDs = append(d.deletedIDs, id)
	}
	return nil
}

func (d *fakeMagnetDriver) RenameFile(_ context.Context, fileID, newName string) error {
	if d.renamed == nil {
		d.renamed = map[string]string{}
	}
	d.renamed[fileID] = newName
	return nil
}

var (
	_ driver.OfflineURLDownloader = (*fakeMagnetDriver)(nil)
	_ driver.OfflineTaskRefresher = (*fakeMagnetDriver)(nil)
	_ driver.Mover                = (*fakeMagnetDriver)(nil)
	_ driver.Renamer              = (*fakeMagnetDriver)(nil)
	_ driver.Deleter              = (*fakeMagnetDriver)(nil)
)

// magnetTestService 构造带磁力能力的测试服务；等待轮询间隔缩短。
func magnetTestService(t *testing.T, drv *fakeMagnetDriver, links *fakeLinkRepo) *Service {
	t.Helper()
	old := magnetResolveInterval
	magnetResolveInterval = time.Millisecond
	t.Cleanup(func() { magnetResolveInterval = old })
	return &Service{
		exec:     driverexec.New(&fakeRenameProvider{drv: drv}, nil),
		accounts: &fakeAccountRepo{account: &domain.Account{ID: 2, DriverType: "guangya", IsActive: true}},
		links:    links,
		log:      slog.Default(),
		progress: newTransferProgressTracker(),
	}
}

const testMagnet = "magnet:?xt=urn:btih:040FB1EE9A2C3416D46F7749A4C53E5B2C34B8A1&dn=Rick.and.Morty.S09E05.mkv"

func TestMagnetInfoHash(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"hex40", testMagnet, "040FB1EE9A2C3416D46F7749A4C53E5B2C34B8A1"},
		{"base32", "magnet:?xt=urn:btih:CKQSGECDACUUVSEMQNPMTYFUXZUULKKE&dn=x", "12A123104300A94AC88C835EC9E0B4BE6945A944"},
		{"missing", "magnet:?dn=x", ""},
		{"not magnet", "https://example.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MagnetInfoHash(tc.in); got != tc.want {
				t.Fatalf("MagnetInfoHash(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseMagnetCreatesFavoriteAndListsFiles(t *testing.T) {
	drv := &fakeMagnetDriver{
		fakeFolderDriver: fakeFolderDriver{children: map[string][]domain.FileItem{}},
		statuses:         []string{driver.OfflineStatusRunning, driver.OfflineStatusSuccess},
	}
	links := newFakeLinkRepo()
	svc := magnetTestService(t, drv, links)

	// 第一次会创建「影视入库 → 磁力离线」两级目录（new-1/new-2）；
	// 离线任务完成后的文件由 fake 的 AddOfflineURLs 写入磁力离线目录。

	res, err := svc.ParseMagnet(context.Background(), ParseMagnetInput{URL: testMagnet})
	if err != nil {
		t.Fatalf("ParseMagnet() error = %v", err)
	}
	if len(drv.added) != 1 || drv.added[0] != testMagnet {
		t.Fatalf("应提交一次离线任务，got %v", drv.added)
	}
	if len(res.Items) != 1 || res.Items[0].ID != "f1" || res.Items[0].Kind != KindVideo {
		t.Fatalf("unexpected items: %+v", res.Items)
	}
	if res.Title != "Rick.and.Morty.S09E05.mkv" {
		t.Fatalf("title = %q", res.Title)
	}
	if res.SavedLinkID == 0 || !isMagnetShareID(res.ShareID) {
		t.Fatalf("应保存磁力收藏: share_id=%q saved=%d", res.ShareID, res.SavedLinkID)
	}
	if links.links[res.SavedLinkID] == nil || links.links[res.SavedLinkID].URL != testMagnet {
		t.Fatalf("收藏应保存原始磁力: %+v", links.links[res.SavedLinkID])
	}
}

func TestParseMagnetFailsWhenOfflineTaskFails(t *testing.T) {
	drv := &fakeMagnetDriver{
		fakeFolderDriver: fakeFolderDriver{children: map[string][]domain.FileItem{}},
		statuses:         []string{driver.OfflineStatusFailed},
	}
	svc := magnetTestService(t, drv, newFakeLinkRepo())

	_, err := svc.ParseMagnet(context.Background(), ParseMagnetInput{URL: testMagnet})
	if err == nil {
		t.Fatal("离线失败应返回错误")
	}
	if want := "光鸭 API 错误(352)：BT文件不存在"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want contains %q", err, want)
	}
}

// 第二次解析同一磁力：直读哈希子目录，不再提交离线任务；账号自动落回离线时用的账号。
func TestParseMagnetSecondCallReadsDirWithoutOffline(t *testing.T) {
	links := newFakeLinkRepo()
	configs := newFakeConfigRepo()
	drv := &fakeMagnetDriver{
		fakeFolderDriver: fakeFolderDriver{children: map[string][]domain.FileItem{}},
		statuses:         []string{driver.OfflineStatusSuccess},
	}
	svc := magnetTestService(t, drv, links)
	svc.configs = configs

	// 第一次：离线成功，fake 在离线目录写入文件。
	if _, err := svc.ParseMagnet(context.Background(), ParseMagnetInput{URL: testMagnet, AccountID: 2}); err != nil {
		t.Fatalf("第一次解析失败: %v", err)
	}
	if len(drv.added) != 1 {
		t.Fatalf("第一次应提交离线任务, added=%v", drv.added)
	}
	if got := configs.values[magnetAccountConfigKey(magnetShareIDPrefix+"040FB1EE9A2C3416D46F7749A4C53E5B2C34B8A1")]; got != "2" {
		t.Fatalf("应记录离线账号, got %q", got)
	}

	// 第二次：目录已有文件 → 直读，不再提交离线任务。
	res, err := svc.ParseMagnet(context.Background(), ParseMagnetInput{URL: testMagnet, SavedID: 1})
	if err != nil {
		t.Fatalf("第二次解析失败: %v", err)
	}
	if len(drv.added) != 1 {
		t.Fatalf("第二次不应重复提交离线任务, added=%v", drv.added)
	}
	if len(res.Items) == 0 {
		t.Fatal("第二次解析应返回目录内容")
	}

	// 子目录浏览：记录的账号（2）应排在最前 —— listMagnetDir 直接可列。
	items, err := svc.listMagnetDir(context.Background(), magnetShareIDPrefix+"040FB1EE9A2C3416D46F7749A4C53E5B2C34B8A1", "f1")
	if err != nil {
		t.Fatalf("listMagnetDir() error = %v", err)
	}
	// fake 中 "f1" 目录不存在 → 空列表但不出错。
	if items == nil {
		t.Fatal("listMagnetDir 应返回空列表而不是 nil")
	}
}

// 删除磁力收藏时连同「来自：云添加/<哈希>」目录一起删除。
func TestDeleteSavedLinkRemovesMagnetDir(t *testing.T) {
	drv := &fakeMagnetDriver{
		fakeFolderDriver: fakeFolderDriver{children: map[string][]domain.FileItem{
			// 「来自：云添加」根目录（new-1）里两个哈希子目录。
			"": {
				{ID: "new-1", Name: "来自：云添加", IsDir: true},
			},
			"new-1": {
				{ID: "hashdir-a", Name: "040FB1EE9A2C3416D46F7749A4C53E5B2C34B8A1", IsDir: true},
				{ID: "hashdir-b", Name: "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF", IsDir: true},
			},
		}},
	}
	links := newFakeLinkRepo()
	links.links[9] = &domain.GuangyaShareLink{
		ID:      9,
		ShareID: magnetShareIDPrefix + "040FB1EE9A2C3416D46F7749A4C53E5B2C34B8A1",
		URL:     testMagnet,
	}
	svc := magnetTestService(t, drv, links)

	if err := svc.DeleteSavedLink(context.Background(), 9); err != nil {
		t.Fatalf("DeleteSavedLink() error = %v", err)
	}
	if len(drv.deletedIDs) != 1 || drv.deletedIDs[0] != "hashdir-a" {
		t.Fatalf("应只删除该磁力的哈希目录，got %v", drv.deletedIDs)
	}
	if _, ok := links.links[9]; ok {
		t.Fatal("收藏记录应被删除")
	}
}

func TestTransferMagnetMovesFilesToTarget(t *testing.T) {
	drv := &fakeMagnetDriver{
		fakeFolderDriver: fakeFolderDriver{children: map[string][]domain.FileItem{
			"root": {{ID: "movies", Name: "电影", IsDir: true}},
			// 离线目录：影视入库/磁力离线 → new-2
			"new-2": {
				{ID: "f1", Name: "Rick.and.Morty.S09E05.mkv", Size: 123},
			},
		}},
	}
	links := newFakeLinkRepo()
	links.links[7] = &domain.GuangyaShareLink{
		ID: 7, ShareID: magnetShareIDPrefix + "040FB1EE9A2C3416D46F7749A4C53E5B2C34B8A1", URL: testMagnet,
	}
	transferred := newFakeTransferredRepo()
	svc := magnetTestService(t, drv, links)
	svc.transfers = transferred

	res, err := svc.Transfer(context.Background(), TransferInput{
		URL:        testMagnet,
		ShareID:    magnetShareIDPrefix + "040FB1EE9A2C3416D46F7749A4C53E5B2C34B8A1",
		SavedID:    7,
		FileIDs:    []string{"f1"},
		AccountID:  2,
		ParentID:   "root",
		TargetPath: []string{"电影", "瑞克和莫蒂"},
		Files: []TransferFile{
			{FileID: "f1", OriginalName: "Rick.and.Morty.S09E05.mkv", NewName: "瑞克和莫蒂-S09E05.mkv"},
		},
		ProgressID: "p1",
	})
	if err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if len(drv.moved) != 1 {
		t.Fatalf("应移动一次文件: %+v", drv.moved)
	}
	move := drv.moved[0]
	if len(move.fileIDs) != 1 || move.fileIDs[0] != "f1" {
		t.Fatalf("unexpected moved ids: %+v", move.fileIDs)
	}
	if move.sourceParentID == "" || move.targetParentID == "" {
		t.Fatalf("源/目标目录不能为空: %+v", move)
	}
	if res.Renamed != 1 {
		t.Fatalf("应重命名一次: %+v", res)
	}
	if !res.Completed {
		t.Fatalf("磁力入库应同步完成（Completed=true），前端才保留弹窗: %+v", res)
	}
	// 转存历史应记录到收藏 7。
	if got := transferred.files[7]; len(got) != 1 || got[0].FileID != "f1" {
		t.Fatalf("unexpected transferred history: %+v", got)
	}
}
