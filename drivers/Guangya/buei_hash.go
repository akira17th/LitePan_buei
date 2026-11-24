// #region buei:guangya-local-flash
// 光鸭的秒传判定不在上传初始化接口里，而是单独调用 check_can_flash_upload，
// 且指纹不是 MD5/SHA1 整文件摘要，而是官方前端的 gcid/cid 算法：
//   - 按文件大小选择分块大小，逐块 SHA1，把 20 字节摘要拼接后整体再 SHA1，得到 gcid；
//   - 取头部 20KB、size/3 处 20KB、尾部 20KB（文件 < 61440 字节取全文件）拼接后 SHA1，得到 cid。
//
// 参考：官方前端 upload worker（/static/js/async/d.*.js），两端必须保持一致。
package guangya

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/internal/driver/uploadutil"
)

const (
	guangyaHashSampleWindow  = 20480
	guangyaHashSampleMinFile = 61440
)

// guangyaHashChunkSize 与官方前端一致：文件越大分块越大，避免超大文件摘要数组过长。
func guangyaHashChunkSize(size int64) int64 {
	switch {
	case size <= 0x8000000: // 128MB
		return 256 << 10
	case size <= 0x10000000: // 256MB
		return 512 << 10
	case size <= 0x20000000: // 512MB
		return 1 << 20
	default:
		return 2 << 20
	}
}

type guangyaHashRange struct {
	start int64
	end   int64
}

// guangyaHashSampleRanges 返回 cid 采样区间；小文件直接取全文件。
func guangyaHashSampleRanges(size int64) []guangyaHashRange {
	if size < guangyaHashSampleMinFile {
		return []guangyaHashRange{{start: 0, end: size}}
	}
	third := size / 3
	return []guangyaHashRange{
		{start: 0, end: guangyaHashSampleWindow},
		{start: third, end: third + guangyaHashSampleWindow},
		{start: size - guangyaHashSampleWindow, end: size},
	}
}

// computeGuangyaHashes 单次顺序读文件，同时算出 gcid 与 cid（大写 hex）。
func computeGuangyaHashes(ctx context.Context, localPath string) (string, string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return "", "", domain.Wrap(domain.CodeInternal, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", "", domain.Wrap(domain.CodeInternal, err)
	}
	size := info.Size()

	chunkSize := guangyaHashChunkSize(size)
	ranges := guangyaHashSampleRanges(size)
	sampleLen := 0
	for _, r := range ranges {
		if r.end > r.start {
			sampleLen += int(r.end - r.start)
		}
	}
	samples := make([]byte, sampleLen)
	filled := 0

	buf := make([]byte, chunkSize)
	digests := make([]byte, 0, sha1.Size*8)
	var offset int64
	for offset < size {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		chunkLen := chunkSize
		if offset+chunkLen > size {
			chunkLen = size - offset
		}
		if _, err := io.ReadFull(file, buf[:chunkLen]); err != nil {
			return "", "", domain.Wrap(domain.CodeDriverError, err)
		}
		sum := sha1.Sum(buf[:chunkLen])
		digests = append(digests, sum[:]...)
		for _, r := range ranges {
			start, end := offset, offset+chunkLen
			if r.start > start {
				start = r.start
			}
			if r.end < end {
				end = r.end
			}
			if start >= end {
				continue
			}
			filled += copy(samples[filled:], buf[start-offset:end-offset])
		}
		offset += chunkLen
	}
	if filled != sampleLen {
		return "", "", domain.Errorf(domain.CodeDriverError, "光鸭秒传采样数据长度异常：%d/%d", filled, sampleLen)
	}

	gcid := sha1.Sum(digests)
	cid := sha1.Sum(samples)
	return strings.ToUpper(hex.EncodeToString(gcid[:])), strings.ToUpper(hex.EncodeToString(cid[:])), nil
}

// checkCanFlashUploadByHashes 调用官方秒传判定接口；参数与官方前端一致（taskId + gcid + cid）。
func (d *Driver) checkCanFlashUploadByHashes(ctx context.Context, taskID, gcid, cid string) (bool, error) {
	var out flashUploadData
	body := map[string]any{"taskId": taskID, "gcid": gcid, "cid": cid}
	if err := d.apiRequest(ctx, pathCheckFlashUpload, body, &out); err != nil {
		return false, err
	}
	return out.CanFlashUpload, nil
}

// tryLocalFlashUpload 在本地上传前询问光鸭能否秒传。
// 探测失败（含接口报错、限流等）一律视为未命中继续正常上传，避免阻断上传。
func (d *Driver) tryLocalFlashUpload(ctx context.Context, taskID, parentID, fileName, localPath string, fileSize int64, onProgress driver.UploadProgress) (string, bool, error) {
	if strings.TrimSpace(taskID) == "" || fileSize <= 0 {
		return "", false, nil
	}
	uploadutil.NotifyProgress(onProgress, 0, fileSize, "正在计算文件校验值")
	gcid, cid, err := computeGuangyaHashes(ctx, localPath)
	if err != nil {
		return "", false, err
	}
	if gcid == "" || cid == "" {
		return "", false, nil
	}
	canFlash, err := d.checkCanFlashUploadByHashes(ctx, taskID, gcid, cid)
	if err != nil || !canFlash {
		return "", false, nil
	}
	fileID, err := d.waitUploadTaskInfo(ctx, taskID, parentID, fileName, fileSize)
	if err != nil {
		return "", false, err
	}
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return "", false, nil
	}
	uploadutil.NotifyProgress(onProgress, fileSize, fileSize, "秒传成功")
	return fileID, true, nil
}

// FlashUploadLocalFile 只做秒传：创建上传任务 → 用 gcid/cid 询问光鸭 → 命中才落文件。
// 未命中返回 driver.ErrFlashMiss，由调用方决定提示与中止。
func (d *Driver) FlashUploadLocalFile(ctx context.Context, req driver.FlashUploadRequest) (*driver.FlashUploadResult, error) {
	localFile, err := uploadutil.StatLocalFile(strings.TrimSpace(req.LocalPath))
	if err != nil {
		return nil, err
	}
	targetName := strings.TrimSpace(filepath.Base(strings.TrimSpace(req.FileName)))
	if targetName == "" || targetName == "." {
		return nil, domain.Errorf(domain.CodeValidation, "上传文件名不能为空")
	}
	if err := uploadutil.ValidateFileName(targetName); err != nil {
		return nil, err
	}
	parentID := d.resolveParent(req.ParentID)
	tokenData, code, err := d.requestUploadToken(ctx, parentID, targetName, localFile.Size)
	if err != nil {
		return nil, err
	}
	taskID := strings.TrimSpace(tokenData.TaskID)
	if taskID == "" {
		return nil, domain.Errorf(domain.CodeDriverError, "光鸭上传缺少 taskId")
	}

	isFlash := code == 156
	if !isFlash {
		gcid, cid, err := computeGuangyaHashes(ctx, localFile.Path)
		if err != nil {
			return nil, err
		}
		canFlash, err := d.checkCanFlashUploadByHashes(ctx, taskID, gcid, cid)
		if err != nil {
			return nil, err
		}
		if !canFlash {
			return nil, fmt.Errorf("%w：%s", driver.ErrFlashMiss, targetName)
		}
	}

	fileID, err := d.waitUploadTaskInfo(ctx, taskID, parentID, targetName, localFile.Size)
	if err != nil {
		return nil, err
	}
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return nil, fmt.Errorf("%w：%s", driver.ErrFlashMiss, targetName)
	}
	return &driver.FlashUploadResult{FileID: fileID, ParentID: parentID, FileName: targetName, Size: localFile.Size}, nil
}

var _ driver.LocalFlashUploader = (*Driver)(nil)

// #endregion buei:guangya-local-flash
