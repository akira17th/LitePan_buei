// #region buei:quark ingest flash to guangya
package quarkingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

type fakeMultiAccounts struct {
	byID map[int64]*domain.Account
}

func (f *fakeMultiAccounts) Get(_ context.Context, id int64) (*domain.Account, error) {
	account, ok := f.byID[id]
	if !ok {
		return nil, domain.Errorf(domain.CodeNotFound, "账号不存在")
	}
	return account, nil
}

type fakeFlashDeps struct {
	dir       string
	miss      map[string]bool
	downloads []string
	flashed   []string
	// blockDownload 让下载阻塞到 ctx 被取消，用于测试取消。
	blockDownload bool
	downloading   chan struct{}
}

func (f *fakeFlashDeps) TempDir() string { return f.dir }

func (f *fakeFlashDeps) Download(ctx context.Context, _ int64, fileID, destPath string, onProgress func(done, total int64)) (int64, error) {
	f.downloads = append(f.downloads, fileID)
	if f.downloading != nil {
		select {
		case f.downloading <- struct{}{}:
		default:
		}
	}
	if f.blockDownload {
		if onProgress != nil {
			onProgress(1, 100)
		}
		<-ctx.Done()
		return 0, ctx.Err()
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return 0, err
	}
	data := []byte("content:" + fileID)
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return 0, err
	}
	if onProgress != nil {
		onProgress(0, int64(len(data)))
		onProgress(int64(len(data)), int64(len(data)))
	}
	return int64(len(data)), nil
}

func (f *fakeFlashDeps) FlashUpload(_ context.Context, _ int64, localPath, fileName, _ string) error {
	if _, err := os.Stat(localPath); err != nil {
		return domain.Errorf(domain.CodeDriverError, "秒传前本地文件不存在：%v", err)
	}
	if f.miss[fileName] {
		return fmt.Errorf("%w：%s", driver.ErrFlashMiss, fileName)
	}
	f.flashed = append(f.flashed, fileName)
	return nil
}

func newFlashTestService(t *testing.T) (*Service, *fakeFlashDeps) {
	svc, deps, _ := newFlashTestServiceWithFiles(t)
	return svc, deps
}

func newFlashTestServiceWithFiles(t *testing.T) (*Service, *fakeFlashDeps, *fakeFileOps) {
	t.Helper()
	deps := &fakeFlashDeps{dir: t.TempDir(), miss: map[string]bool{}}
	files := newFakeFileOps()
	svc := New(Options{
		Files: files,
		Accounts: &fakeMultiAccounts{byID: map[int64]*domain.Account{
			7: {ID: 7, Name: "夸克", DriverType: "quark", IsActive: true},
			9: {ID: 9, Name: "光鸭", DriverType: "guangya", IsActive: true},
		}},
		Configs: &fakeConfigStore{},
	})
	svc.SetFlash(deps)
	return svc, deps, files
}

func flashIngestInput(files ...IngestFile) IngestInput {
	return IngestInput{
		AccountID:            7,
		FavoriteID:           3,
		SourceParentID:       "inbox",
		SourceFolderParentID: "share-root",
		SourceFolderName:     "瑞克和莫蒂 S09",
		ParentID:             "guangya-root",
		TargetPath:           []string{"剧名", "Season 01"},
		Title:                "剧名",
		Season:               1,
		IsSeries:             true,
		Files:                files,
		ProgressID:           "test-progress",
		FlashToGuangya:       true,
		FlashAccountID:       9,
	}
}

func TestFlashIngestKeepsTempWhenFlashMissed(t *testing.T) {
	svc, deps := newFlashTestService(t)
	deps.miss["剧名-S01E02.mkv"] = true

	result, err := svc.Ingest(context.Background(), flashIngestInput(
		IngestFile{FileID: "f1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01.mkv"},
		IngestFile{FileID: "f2", OriginalName: "第二集.mkv", NewName: "剧名-S01E02.mkv"},
	))
	if err != nil {
		t.Fatalf("有文件秒传成功时应按部分成功返回，got %v", err)
	}
	if result.Flashed != 1 || result.Missed != 1 || !strings.Contains(result.MissMessage, "未命中光鸭秒传") {
		t.Fatalf("部分成功结果不符合预期: %+v", result)
	}
	if len(deps.flashed) != 1 {
		t.Fatalf("应只秒传第一个文件，got %v", deps.flashed)
	}

	rootDir := filepath.Join(deps.dir, "7_3")
	fileDir := filepath.Join(rootDir, "瑞克和莫蒂 S09")
	// 第二个文件保留（未命中），第一个已秒传成功并删除本地文件。
	if _, statErr := os.Stat(filepath.Join(fileDir, "剧名-S01E02.mkv")); statErr != nil {
		t.Fatalf("未命中文件应保留在临时目录: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(fileDir, "剧名-S01E01.mkv")); !os.IsNotExist(statErr) {
		t.Fatalf("已秒传的文件应删除本地副本, statErr=%v", statErr)
	}

	staged, err := svc.FlashStaged(context.Background(), 7, 3)
	if err != nil {
		t.Fatalf("读取已下载标记失败: %v", err)
	}
	if len(staged) != 1 || staged[0].FileID != "f2" {
		t.Fatalf("已下载标记应只剩未命中的文件，got %+v", staged)
	}
}

// 入库完成后删除源文件：部分成功时只删已秒传的，全部成功时全删。
func TestFlashIngestDeletesSourceFilesAfterIngest(t *testing.T) {
	svc, deps, files := newFlashTestServiceWithFiles(t)
	input := flashIngestInput(
		IngestFile{FileID: "f1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01.mkv"},
		IngestFile{FileID: "f2", OriginalName: "第二集.mkv", NewName: "剧名-S01E02.mkv"},
	)
	if _, err := svc.Ingest(context.Background(), input); err != nil {
		t.Fatalf("入库失败: %v", err)
	}
	if len(files.deletes) != 1 {
		t.Fatalf("应删除一次源文件，got %+v", files.deletes)
	}
	got := files.deletes[0]
	if got.parent != "inbox" {
		t.Fatalf("应在收藏文件夹内删除，got %q", got.parent)
	}
	if len(got.fileIDs) != 2 || got.fileIDs[0] != "f1" || got.fileIDs[1] != "f2" {
		t.Fatalf("应删除已秒传的全部源文件，got %+v", got.fileIDs)
	}
	// 全部成功：本地临时目录整体清理。
	if _, statErr := os.Stat(filepath.Join(deps.dir, "7_3")); !os.IsNotExist(statErr) {
		t.Fatalf("成功入库后应删除临时目录, statErr=%v", statErr)
	}
}

func TestFlashIngestPartialDeletesOnlyFlashedSources(t *testing.T) {
	svc, deps, files := newFlashTestServiceWithFiles(t)
	deps.miss["剧名-S01E02.mkv"] = true
	if _, err := svc.Ingest(context.Background(), flashIngestInput(
		IngestFile{FileID: "f1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01.mkv"},
		IngestFile{FileID: "f2", OriginalName: "第二集.mkv", NewName: "剧名-S01E02.mkv"},
	)); err != nil {
		t.Fatalf("部分成功应返回 nil 错误: %v", err)
	}
	if len(files.deletes) != 1 {
		t.Fatalf("应删除一次源文件，got %+v", files.deletes)
	}
	if ids := files.deletes[0].fileIDs; len(ids) != 1 || ids[0] != "f1" {
		t.Fatalf("只应删除已秒传的源文件，got %+v", ids)
	}
}

func TestFlashIngestReusesStagedFileWithoutDownload(t *testing.T) {
	svc, deps := newFlashTestService(t)
	deps.miss["剧名-S01E01.mkv"] = true

	input := flashIngestInput(
		IngestFile{FileID: "f1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01.mkv"},
	)
	if _, err := svc.Ingest(context.Background(), input); err == nil {
		t.Fatal("首次入库应因未命中秒传而失败")
	}
	if len(deps.downloads) != 1 {
		t.Fatalf("首次应下载一次，got %v", deps.downloads)
	}

	// 第二次：重命名规则变化 + 允许秒传，应复用本地文件并就地改名。
	deps.miss = map[string]bool{}
	deps.downloads = nil
	input.Files = []IngestFile{{FileID: "f1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01-改名.mkv"}}
	result, err := svc.Ingest(context.Background(), input)
	if err != nil {
		t.Fatalf("二次入库失败: %v", err)
	}
	if len(deps.downloads) != 0 {
		t.Fatalf("二次入库不应重新下载，got %v", deps.downloads)
	}
	if result.Flashed != 1 || result.Renamed != 1 {
		t.Fatalf("秒传结果不符合预期: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(deps.dir, "7_3")); !os.IsNotExist(err) {
		t.Fatalf("入库成功后应删除临时目录, statErr=%v", err)
	}
}

func TestFlashIngestCancelDeletesTempAndStops(t *testing.T) {
	svc, deps := newFlashTestService(t)
	deps.blockDownload = true
	deps.downloading = make(chan struct{}, 4)

	errCh := make(chan error, 1)
	go func() {
		_, err := svc.Ingest(context.Background(), flashIngestInput(
			IngestFile{FileID: "f1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01.mkv"},
		))
		errCh <- err
	}()

	// 等下载真正开始后从外部取消（模拟前端悬「取消」按钮）。
	select {
	case <-deps.downloading:
	case <-time.After(2 * time.Second):
		t.Fatal("下载未开始")
	}
	if !svc.CancelIngest("test-progress") {
		t.Fatal("取消应命中进行中的任务")
	}

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "已取消入库") {
			t.Fatalf("取消后应返回已取消错误，got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("取消后入库未结束")
	}

	if _, statErr := os.Stat(filepath.Join(deps.dir, "7_3")); !os.IsNotExist(statErr) {
		t.Fatalf("取消后应删除临时目录, statErr=%v", statErr)
	}
	progress, ok := svc.TransferProgress("test-progress")
	if !ok || progress.Phase != PhaseFailed {
		t.Fatalf("取消后进度应为失败，got %+v ok=%v", progress, ok)
	}
}

func TestFlashIngestRecordsPerFileStages(t *testing.T) {
	svc, deps := newFlashTestService(t)
	deps.miss["剧名-S01E02.mkv"] = true

	_, err := svc.Ingest(context.Background(), flashIngestInput(
		IngestFile{FileID: "f1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01.mkv"},
		IngestFile{FileID: "f2", OriginalName: "第二集.mkv", NewName: "剧名-S01E02.mkv"},
		IngestFile{FileID: "f3", OriginalName: "第三集.mkv", NewName: "剧名-S01E03.mkv"},
	))
	if err != nil {
		t.Fatalf("有文件秒传成功时应按部分成功返回，got %v", err)
	}

	progress, ok := svc.TransferProgress("test-progress")
	if !ok {
		t.Fatal("应保留进度条目")
	}
	if progress.Phase != PhaseDone {
		t.Fatalf("有文件成功时阶段应为完成，got %s", progress.Phase)
	}
	if got := progress.Files["f1"].Stage; got != FileStageFlashed {
		t.Fatalf("f1 应为已秒传，got %s", got)
	}
	if got := progress.Files["f2"].Stage; got != FileStageFailed {
		t.Fatalf("f2 应为失败，got %s", got)
	}
	if msg := progress.Files["f2"].Message; !strings.Contains(msg, "未命中秒传") {
		t.Fatalf("f2 应带未命中提示，got %q", msg)
	}
	if got := progress.Files["f3"].Stage; got != FileStageQueued {
		t.Fatalf("f3 未执行应保持等待，got %s", got)
	}
	if progress.Files["f2"].Name != "剧名-S01E02.mkv" {
		t.Fatalf("单文件应记录本地文件名，got %q", progress.Files["f2"].Name)
	}
}

func TestFlashIngestRunsFollowUpsWhenPartiallyFlashed(t *testing.T) {
	svc, deps := newFlashTestService(t)
	strmAccount := int64(0)
	strmCalls := 0
	svc.SetStrmGenerator(func(_ context.Context, accountID int64, _ string, _ []string) (int64, bool, error) {
		strmCalls++
		strmAccount = accountID
		return 1, true, nil
	})

	// 第二个文件未命中：第一个已秒传成功，仍要继续执行联动动作。
	deps.miss["剧名-S01E02.mkv"] = true
	input := flashIngestInput(
		IngestFile{FileID: "f1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01.mkv"},
		IngestFile{FileID: "f2", OriginalName: "第二集.mkv", NewName: "剧名-S01E02.mkv"},
	)
	input.AutoStrm = true
	result, err := svc.Ingest(context.Background(), input)
	if err != nil {
		t.Fatalf("有文件秒传成功时应按部分成功返回，got %v", err)
	}
	if result.Flashed != 1 || result.Missed != 1 || result.MissMessage == "" {
		t.Fatalf("部分成功结果不符合预期: %+v", result)
	}
	if strmCalls != 1 {
		t.Fatalf("已秒传成功的文件应继续执行联动动作，calls=%d", strmCalls)
	}
	if strmAccount != 9 {
		t.Fatalf("联动动作应使用光鸭账号，got %d", strmAccount)
	}
}

func TestFlashIngestSkipsFollowUpsWhenNothingFlashed(t *testing.T) {
	svc, deps := newFlashTestService(t)
	strmCalls := 0
	svc.SetStrmGenerator(func(context.Context, int64, string, []string) (int64, bool, error) {
		strmCalls++
		return 0, false, nil
	})

	deps.miss["剧名-S01E01.mkv"] = true
	input := flashIngestInput(
		IngestFile{FileID: "f1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01.mkv"},
	)
	input.AutoStrm = true
	if _, err := svc.Ingest(context.Background(), input); err == nil {
		t.Fatal("应返回未命中错误")
	}
	if strmCalls != 0 {
		t.Fatalf("没有文件秒传成功时不应执行联动动作，calls=%d", strmCalls)
	}
}

func TestFlashStagedSkipsMissingLocalFile(t *testing.T) {
	svc, deps := newFlashTestService(t)
	deps.miss["剧名-S01E01.mkv"] = true
	input := flashIngestInput(
		IngestFile{FileID: "f1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01.mkv"},
	)
	if _, err := svc.Ingest(context.Background(), input); err == nil {
		t.Fatal("首次入库应失败")
	}
	if err := os.Remove(filepath.Join(deps.dir, "7_3", "瑞克和莫蒂 S09", "剧名-S01E01.mkv")); err != nil {
		t.Fatalf("删除本地文件失败: %v", err)
	}
	staged, err := svc.FlashStaged(context.Background(), 7, 3)
	if err != nil {
		t.Fatalf("读取标记失败: %v", err)
	}
	if len(staged) != 0 {
		t.Fatalf("本地文件已删除时不应再标记，got %+v", staged)
	}
}

// #endregion buei:quark ingest flash to guangya
