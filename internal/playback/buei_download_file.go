// #region buei:flash-ingest
package playback

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"litepan/internal/domain"
)

// DownloadToFile 把单个文件完整下载到本地路径，等价于「跨盘普传」的下载阶段：
// 已知大小时用分段并发（DownloadOriginal，多连接提速），上游不支持 Range 时退化为单请求；
// 完成后按连续写入区间截断，避免乱序尾部。onProgress 报告已落盘字节与总大小。
func (s *Service) DownloadToFile(
	ctx context.Context,
	accountID int64,
	fileID, destPath string,
	onProgress func(done, total int64),
) (int64, error) {
	if s == nil {
		return 0, domain.Errorf(domain.CodeInternal, "播放服务未就绪")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return 0, domain.Wrap(domain.CodeInternal, err)
	}
	res, err := s.Resolve(ctx, accountID, fileID, "", false, false)
	if err != nil {
		return 0, err
	}
	total := res.File.Size
	if total <= 0 {
		total = res.Link.Size
	}
	file, err := os.Create(destPath)
	if err != nil {
		return 0, domain.Wrap(domain.CodeInternal, err)
	}

	if total > 0 {
		written := int64(0)
		prefix, dlErr := s.DownloadOriginal(ctx, file, accountID, fileID, res, 0, total-1, func(n, contiguous int64) error {
			written += n
			if onProgress != nil {
				if contiguous > written {
					contiguous = written
				}
				onProgress(min(contiguous, total), total)
			}
			return nil
		})
		if dlErr != nil {
			if !errors.Is(dlErr, ErrInvalidRangeResponse) {
				_ = file.Close()
				_ = os.Remove(destPath)
				return written, dlErr
			}
			// 上游不支持 Range：清空后走单请求兼容路径。
			if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
				_ = file.Close()
				_ = os.Remove(destPath)
				return 0, domain.Wrap(domain.CodeInternal, seekErr)
			}
			if truncErr := file.Truncate(0); truncErr != nil {
				_ = file.Close()
				_ = os.Remove(destPath)
				return 0, domain.Wrap(domain.CodeInternal, truncErr)
			}
			writer := &countingWriter{dst: file, total: total, onProgress: onProgress}
			if fallbackErr := s.CopyOriginalFull(ctx, writer, accountID, fileID, res); fallbackErr != nil {
				_ = file.Close()
				_ = os.Remove(destPath)
				return writer.written, fallbackErr
			}
			writer.flush()
			prefix = writer.written
		}
		if truncErr := file.Truncate(prefix); truncErr != nil {
			_ = file.Close()
			_ = os.Remove(destPath)
			return written, domain.Wrap(domain.CodeInternal, truncErr)
		}
		if prefix != total {
			_ = file.Close()
			_ = os.Remove(destPath)
			return prefix, domain.Errorf(domain.CodeDriverError, "下载不完整：已下载 %d 字节，预期 %d 字节", prefix, total)
		}
		if syncErr := file.Sync(); syncErr != nil {
			_ = file.Close()
			return prefix, domain.Wrap(domain.CodeInternal, syncErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return prefix, domain.Wrap(domain.CodeInternal, closeErr)
		}
		if onProgress != nil {
			onProgress(total, total)
		}
		return prefix, nil
	}

	// 未知文件大小：只能单请求下载。
	writer := &countingWriter{dst: file, total: total, onProgress: onProgress}
	if err := s.CopyOriginalFull(ctx, writer, accountID, fileID, res); err != nil {
		_ = file.Close()
		_ = os.Remove(destPath)
		return writer.written, err
	}
	writer.flush()
	if syncErr := file.Sync(); syncErr != nil {
		_ = file.Close()
		return writer.written, domain.Wrap(domain.CodeInternal, syncErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		return writer.written, domain.Wrap(domain.CodeInternal, closeErr)
	}
	return writer.written, nil
}

// countingWriter 统计写入字节并按 300ms 节流回调进度。
type countingWriter struct {
	dst        io.Writer
	written    int64
	total      int64
	onProgress func(done, total int64)
	lastEmit   time.Time
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	w.written += int64(n)
	if w.onProgress != nil {
		now := time.Now()
		if w.lastEmit.IsZero() || now.Sub(w.lastEmit) >= 300*time.Millisecond {
			w.lastEmit = now
			w.onProgress(w.written, w.total)
		}
	}
	return n, err
}

func (w *countingWriter) flush() {
	if w.onProgress != nil {
		w.onProgress(w.written, w.total)
	}
}

// #endregion buei:flash-ingest
