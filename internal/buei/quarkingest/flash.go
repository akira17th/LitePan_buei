// #region buei:quark ingest flash to guangya
package quarkingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

// flashManifestName 是每个收藏临时目录下的下载清单文件。
const flashManifestName = ".buei-flash.json"

// FlashDeps 是「秒传到光鸭」需要的外部能力；未注入时该模式不可用。
type FlashDeps interface {
	// TempDir 返回存放下载文件的临时根目录，每个收藏一个子目录。
	TempDir() string
	// Download 把夸克文件完整下载到 destPath，返回写入字节数；onProgress 可为空。
	Download(ctx context.Context, accountID int64, fileID, destPath string, onProgress func(done, total int64)) (int64, error)
	// FlashUpload 只做秒传；未命中时返回包装了 driver.ErrFlashMiss 的错误。
	FlashUpload(ctx context.Context, accountID int64, localPath, fileName, parentID string) error
}

// SetFlash 注入秒传依赖。
func (s *Service) SetFlash(deps FlashDeps) {
	if s == nil {
		return
	}
	s.flash = deps
}

// flashStagedFile 是清单里一个源文件对应的本地文件信息。
type flashStagedFile struct {
	FileName     string    `json:"file_name"`
	OriginalName string    `json:"original_name"`
	Size         int64     `json:"size"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// flashManifest 记录某个收藏的临时目录名与已下载文件（key 为夸克文件 id）。
type flashManifest struct {
	Folder    string                     `json:"folder"`
	Files     map[string]flashStagedFile `json:"files"`
	UpdatedAt time.Time                  `json:"updated_at"`
}

// StagedFlashFile 是对外的「已下载到临时目录」标记。
type StagedFlashFile struct {
	FileID       string
	FileName     string
	OriginalName string
	Size         int64
	UpdatedAt    time.Time
}

// flashRootDir 返回某个收藏的临时根目录：按账号 + 收藏固定，避免不同批次的文件散落。
func (s *Service) flashRootDir(accountID, favoriteID int64) (string, error) {
	if s.flash == nil {
		return "", domain.Errorf(domain.CodeInternal, "秒传入库服务未就绪")
	}
	root := strings.TrimSpace(s.flash.TempDir())
	if root == "" {
		return "", domain.Errorf(domain.CodeInternal, "秒传临时目录不可用")
	}
	return filepath.Join(root, fmt.Sprintf("%d_%d", accountID, favoriteID)), nil
}

// FlashStaged 返回收藏内已下载到临时目录的文件；本地文件已被删除的条目会被清理。
func (s *Service) FlashStaged(_ context.Context, accountID, favoriteID int64) ([]StagedFlashFile, error) {
	if accountID <= 0 || favoriteID <= 0 {
		return nil, nil
	}
	dir, err := s.flashRootDir(accountID, favoriteID)
	if err != nil {
		return nil, err
	}
	manifest, err := readFlashManifest(dir)
	if err != nil || manifest == nil || len(manifest.Files) == 0 {
		return nil, nil
	}
	fileDir := filepath.Join(dir, manifest.Folder)
	out := make([]StagedFlashFile, 0, len(manifest.Files))
	changed := false
	for fileID, entry := range manifest.Files {
		info, statErr := os.Stat(filepath.Join(fileDir, entry.FileName))
		if statErr != nil || info.IsDir() {
			delete(manifest.Files, fileID)
			changed = true
			continue
		}
		out = append(out, StagedFlashFile{
			FileID:       fileID,
			FileName:     entry.FileName,
			OriginalName: entry.OriginalName,
			Size:         info.Size(),
			UpdatedAt:    entry.UpdatedAt,
		})
	}
	if changed {
		_ = writeFlashManifest(dir, manifest)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FileID < out[j].FileID })
	return out, nil
}

// ingestFlash 是「秒传到光鸭」的入库路径：
// 逐个文件下载到临时目录（文件名即重命名结果）→ 仅秒传写入光鸭 → 成功的立即删除本地文件。
// 未命中秒传时立即中止，保留临时目录与清单；下次入库可直接复用已下载的文件。
func (s *Service) ingestFlash(
	ctx context.Context,
	in IngestInput,
	files []IngestFile,
	targetPath []string,
	progressID string,
	asyncPending *bool,
) (out *Result, err error) {
	if s.flash == nil {
		return nil, domain.Errorf(domain.CodeInternal, "秒传入库服务未就绪")
	}
	if in.FlashAccountID <= 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请选择秒传目标光鸭账号")
	}
	target, err := s.accounts.Get(ctx, in.FlashAccountID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, domain.Errorf(domain.CodeNotFound, "光鸭账号不存在")
	}
	if target.DriverType != "guangya" {
		return nil, domain.Errorf(domain.CodeValidation, "秒传目标必须是光鸭云盘账号")
	}
	if !target.IsActive {
		return nil, domain.Errorf(domain.CodeValidation, "秒传目标账号已停用")
	}

	rootDir, err := s.flashRootDir(in.AccountID, in.FavoriteID)
	if err != nil {
		return nil, err
	}
	manifest, err := readFlashManifest(rootDir)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		manifest = &flashManifest{}
	}
	if manifest.Files == nil {
		manifest.Files = map[string]flashStagedFile{}
	}
	fileDir, err := prepareFlashDir(rootDir, manifest, flashSourceFolderName(in))
	if err != nil {
		return nil, err
	}
	// 全部成功才清理；中途失败保留目录，便于下一次直接复用已下载文件。
	keepDir := true
	defer func() {
		if !keepDir {
			_ = os.RemoveAll(rootDir)
		}
	}()

	s.progress.setPhase(progressID, PhaseDownload)
	targetParentID, err := s.ensureTargetFolder(ctx, in.FlashAccountID, in.ParentID, targetPath)
	if err != nil {
		return nil, err
	}

	// 可取消的下载上下文：前端点「取消」会中断正在传输的连接。
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	s.progress.registerCancel(progressID, cancelRun)
	defer s.progress.clearCancel(progressID)

	result := &Result{}
	// 先登记全部待处理文件，文件列表可立即显示进度条。
	queued := make(map[string]string, len(files))
	for _, file := range files {
		name := strings.TrimSpace(file.NewName)
		if name == "" {
			name = strings.TrimSpace(file.OriginalName)
		}
		if name != "" {
			queued[file.FileID] = name
		}
	}
	s.progress.queueFiles(progressID, queued)

	// 已秒传成功的源文件 id：入库后连同本地临时文件一起清理。
	flashedIDs := make([]string, 0, len(files))

	// 中断（未命中/失败）时：只要有文件已秒传成功，就继续执行后续联动动作，
	// 并按「部分成功」返回（提示文案通过 miss_message 带给前端，弹窗里同时保留提示）。
	abort := func(cause error) (*Result, error) {
		if result.Flashed > 0 {
			s.deleteFlashedSources(ctx, in.AccountID, in.SourceParentID, flashedIDs)
			*asyncPending = s.runIngestFollowUps(ctx, in, in.FlashAccountID, targetParentID, targetPath, progressID, result)
			result.Missed++
			result.MissMessage = cause.Error()
			s.logger().Info("夸克入库部分成功，已秒传的文件继续执行联动动作",
				"flashed", result.Flashed, "error", cause)
			return result, nil
		}
		return nil, cause
	}

	for _, file := range files {
		name := strings.TrimSpace(file.NewName)
		if name == "" {
			name = strings.TrimSpace(file.OriginalName)
		}
		if name == "" {
			return nil, domain.Errorf(domain.CodeValidation, "文件缺少可用名称，无法秒传")
		}
		if s.progress.isCanceled(progressID) {
			s.progress.setFileStage(progressID, file.FileID, FileStageFailed, "已取消入库")
			return nil, s.flashCanceled(rootDir)
		}
		dest := filepath.Join(fileDir, name)

		reused := s.reuseStagedFile(rootDir, manifest, file, dest)
		if reused {
			s.progress.setFileStage(progressID, file.FileID, FileStageDownloaded, "")
		}
		if !reused {
			s.progress.setPhase(progressID, PhaseDownload)
			s.progress.setDownload(progressID, file.FileID, name, 0, 0)
			size, err := s.flash.Download(runCtx, in.AccountID, file.FileID, dest, func(done, total int64) {
				s.progress.setDownload(progressID, file.FileID, name, done, total)
			})
			if err != nil {
				if s.progress.isCanceled(progressID) {
					s.progress.setFileStage(progressID, file.FileID, FileStageFailed, "已取消入库")
					return nil, s.flashCanceled(rootDir)
				}
				s.progress.setFileStage(progressID, file.FileID, FileStageFailed, "下载失败")
				s.logger().Warn("夸克入库下载文件失败", "account_id", in.AccountID, "file_id", file.FileID, "name", name, "error", err)
				return abort(domain.Wrap(domain.CodeDriverError, fmt.Errorf("下载「%s」失败：%w", name, err)))
			}
			manifest.Files[file.FileID] = flashStagedFile{
				FileName:     name,
				OriginalName: strings.TrimSpace(file.OriginalName),
				Size:         size,
				UpdatedAt:    time.Now().UTC(),
			}
			manifest.UpdatedAt = time.Now().UTC()
			_ = writeFlashManifest(rootDir, manifest)
			s.progress.setFileStage(progressID, file.FileID, FileStageDownloaded, "")
		}
		if s.progress.isCanceled(progressID) {
			s.progress.setFileStage(progressID, file.FileID, FileStageFailed, "已取消入库")
			return nil, s.flashCanceled(rootDir)
		}

		s.progress.setPhase(progressID, PhaseFlash)
		if err := s.flash.FlashUpload(ctx, in.FlashAccountID, dest, name, targetParentID); err != nil {
			if errors.Is(err, driver.ErrFlashMiss) {
				// 保留临时文件与清单：源文件已打上「已下载」标记，下次入库无需重新下载。
				s.progress.setFileStage(progressID, file.FileID, FileStageFailed, "未命中秒传，已中止入库")
				return abort(domain.Errorf(
					domain.CodeValidation,
					"文件「%s」未命中光鸭秒传，已中止入库；已下载的文件保留在 %s，下次入库无需重新下载",
					name, fileDir,
				))
			}
			s.progress.setFileStage(progressID, file.FileID, FileStageFailed, "秒传失败")
			s.logger().Warn("夸克入库秒传到光鸭失败", "account_id", in.FlashAccountID, "name", name, "error", err)
			return abort(domain.Wrap(domain.CodeDriverError, fmt.Errorf("秒传「%s」失败：%w", name, err)))
		}
		if name != strings.TrimSpace(file.OriginalName) {
			result.Renamed++
		}
		result.Flashed++
		flashedIDs = append(flashedIDs, file.FileID)
		s.progress.setFileStage(progressID, file.FileID, FileStageFlashed, "")
		// 已秒传成功的文件立即删除本地副本，避免下次重复秒传。
		_ = os.Remove(dest)
		delete(manifest.Files, file.FileID)
		manifest.UpdatedAt = time.Now().UTC()
		_ = writeFlashManifest(rootDir, manifest)
	}
	keepDir = false
	result.Moved = result.Flashed

	// 入库完成：删除收藏文件夹中的源文件（本地临时文件已在秒传成功后逐个删除）。
	s.deleteFlashedSources(ctx, in.AccountID, in.SourceParentID, flashedIDs)

	*asyncPending = s.runIngestFollowUps(ctx, in, in.FlashAccountID, targetParentID, targetPath, progressID, result)

	s.logger().Info("夸克入库（秒传到光鸭）完成",
		"account_id", in.AccountID,
		"guangya_account_id", in.FlashAccountID,
		"target", targetParentID,
		"count", result.Flashed,
	)
	return result, nil
}

// reuseStagedFile 尝试复用上次已下载的文件；文件名/目录变化时就地改名，不重新下载。
func (s *Service) reuseStagedFile(rootDir string, manifest *flashManifest, file IngestFile, dest string) bool {
	entry, ok := manifest.Files[file.FileID]
	if !ok {
		return false
	}
	src := filepath.Join(rootDir, manifest.Folder, entry.FileName)
	info, err := os.Stat(src)
	if err != nil || info.IsDir() {
		delete(manifest.Files, file.FileID)
		_ = writeFlashManifest(rootDir, manifest)
		return false
	}
	if src != dest {
		if err := os.Rename(src, dest); err != nil {
			if copyErr := copyLocalFile(src, dest); copyErr != nil {
				delete(manifest.Files, file.FileID)
				_ = writeFlashManifest(rootDir, manifest)
				return false
			}
			_ = os.Remove(src)
		}
	}
	entry.FileName = filepath.Base(dest)
	entry.Size = info.Size()
	entry.UpdatedAt = time.Now().UTC()
	manifest.Files[file.FileID] = entry
	manifest.UpdatedAt = time.Now().UTC()
	_ = writeFlashManifest(rootDir, manifest)
	return true
}

// prepareFlashDir 保证「收藏源文件夹同名」子目录存在；文件夹改名时同步改名已下载目录。
func prepareFlashDir(rootDir string, manifest *flashManifest, folder string) (string, error) {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return "", domain.Wrap(domain.CodeInternal, err)
	}
	if manifest.Folder != "" && manifest.Folder != folder {
		oldDir := filepath.Join(rootDir, manifest.Folder)
		if err := os.Rename(oldDir, filepath.Join(rootDir, folder)); err != nil && !os.IsNotExist(err) {
			// 改名失败（例如目标目录已存在）时丢弃旧清单，按未下载处理，不影响入库。
			manifest.Files = map[string]flashStagedFile{}
			_ = os.RemoveAll(oldDir)
		}
	}
	manifest.Folder = folder
	manifest.UpdatedAt = time.Now().UTC()
	dir := filepath.Join(rootDir, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", domain.Wrap(domain.CodeInternal, err)
	}
	_ = writeFlashManifest(rootDir, manifest)
	return dir, nil
}

// flashSourceFolderName 取夸克收藏源文件夹名作为临时子目录名；缺失时回落到目标标题。
func flashSourceFolderName(in IngestInput) string {
	for _, raw := range []string{in.SourceFolderName, in.Title} {
		if name := sanitizeFolderName(raw); name != "" {
			return name
		}
	}
	return "files"
}

// deleteFlashedSources 删除已秒传入库的夸克源文件；失败只记日志，不影响入库结果。
func (s *Service) deleteFlashedSources(ctx context.Context, accountID int64, parentID string, fileIDs []string) {
	if s == nil || s.files == nil || len(fileIDs) == 0 {
		return
	}
	// 清理动作不跟随请求取消，避免客户端断开后留下源文件。
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.files.DeleteFiles(cleanupCtx, accountID, fileIDs, parentID); err != nil {
		s.logger().Warn("夸克入库后删除源文件失败",
			"account_id", accountID, "parent_id", parentID, "count", len(fileIDs), "error", err)
		return
	}
	s.logger().Info("夸克入库后已删除源文件",
		"account_id", accountID, "parent_id", parentID, "count", len(fileIDs))
}

// flashCanceled 处理前端取消：删除临时文件并返回可展示的错误。
func (s *Service) flashCanceled(rootDir string) error {
	_ = os.RemoveAll(rootDir)
	s.logger().Info("夸克入库已取消，已清理临时文件", "dir", rootDir)
	return domain.Errorf(domain.CodeValidation, "已取消入库，临时文件已删除")
}

// CancelIngest 请求终止进行中的入库（目前用于秒传下载阶段）。
func (s *Service) CancelIngest(progressID string) bool {
	if s == nil || s.progress == nil {
		return false
	}
	return s.progress.cancel(progressID)
}

func flashManifestPath(rootDir string) string {
	return filepath.Join(rootDir, flashManifestName)
}

func readFlashManifest(rootDir string) (*flashManifest, error) {
	raw, err := os.ReadFile(flashManifestPath(rootDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, domain.Wrap(domain.CodeInternal, err)
	}
	var manifest flashManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		// 清单损坏时按“没有下载过”处理，避免阻塞入库。
		return &flashManifest{}, nil
	}
	if manifest.Files == nil {
		manifest.Files = map[string]flashStagedFile{}
	}
	return &manifest, nil
}

func writeFlashManifest(rootDir string, manifest *flashManifest) error {
	if manifest == nil {
		return nil
	}
	if manifest.Files == nil {
		manifest.Files = map[string]flashStagedFile{}
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return domain.Wrap(domain.CodeInternal, err)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return domain.Wrap(domain.CodeInternal, err)
	}
	if err := os.WriteFile(flashManifestPath(rootDir), raw, 0o644); err != nil {
		return domain.Wrap(domain.CodeInternal, err)
	}
	return nil
}

func copyLocalFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dest)
		return err
	}
	return out.Close()
}

// #endregion buei:quark ingest flash to guangya
