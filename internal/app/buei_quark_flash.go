// #region buei:quark ingest flash to guangya
package app

import (
	"context"
	"path/filepath"

	"litepan/internal/core/driverexec"
	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/internal/playback"
)

// bueiQuarkFlashDeps 是「夸克入库 → 秒传到光鸭」的适配层：
// 下载走播放服务（夸克直链），秒传走目标驱动实现的 driver.LocalFlashUploader。
type bueiQuarkFlashDeps struct {
	exec     *driverexec.Executor
	playback *playback.Service
	dataDir  string
}

func newBueiQuarkFlashDeps(exec *driverexec.Executor, pb *playback.Service, dataDir string) *bueiQuarkFlashDeps {
	return &bueiQuarkFlashDeps{exec: exec, playback: pb, dataDir: dataDir}
}

func (d *bueiQuarkFlashDeps) TempDir() string {
	return filepath.Join(d.dataDir, "quark_ingest_flash")
}

func (d *bueiQuarkFlashDeps) Download(ctx context.Context, accountID int64, fileID, destPath string, onProgress func(done, total int64)) (int64, error) {
	if d.playback == nil {
		return 0, domain.Errorf(domain.CodeInternal, "播放服务未就绪")
	}
	// 与「跨盘普传」共用同一段下载实现（分段并发 + 单请求兼容），见 playback.DownloadToFile。
	return d.playback.DownloadToFile(ctx, accountID, fileID, destPath, onProgress)
}

func (d *bueiQuarkFlashDeps) FlashUpload(ctx context.Context, accountID int64, localPath, fileName, parentID string) error {
	if d.exec == nil {
		return domain.Errorf(domain.CodeInternal, "驱动执行器未就绪")
	}
	return d.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		uploader, ok := drv.(driver.LocalFlashUploader)
		if !ok {
			return domain.Errorf(domain.CodeNotImplement, "目标账号驱动不支持秒传")
		}
		_, err := uploader.FlashUploadLocalFile(ctx, driver.FlashUploadRequest{
			LocalPath: localPath,
			FileName:  fileName,
			ParentID:  parentID,
		})
		return err
	})
}

// #endregion buei:quark ingest flash to guangya
