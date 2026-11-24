// #region buei:flash-upload
package driver

import (
	"context"
	"errors"
)

// ErrFlashMiss 表示目标网盘没有相同内容的文件，无法秒传。
// 调用方可用 errors.Is 判断，从而把“未命中”和“接口失败”区分开。
var ErrFlashMiss = errors.New("未命中秒传")

// FlashUploadRequest 是仅按内容秒传的请求：驱动不会退化成普通上传，未命中即失败。
type FlashUploadRequest struct {
	LocalPath string
	FileName  string
	ParentID  string
}

// FlashUploadResult 是秒传结果。
type FlashUploadResult struct {
	FileID   string
	ParentID string
	FileName string
	Size     int64
}

// LocalFlashUploader 是可选能力：只做秒传（内容命中才落文件）。
type LocalFlashUploader interface {
	FlashUploadLocalFile(ctx context.Context, req FlashUploadRequest) (*FlashUploadResult, error)
}

// #endregion buei:flash-upload
