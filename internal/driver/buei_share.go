package driver

import "context"

// GuangyaShareRestoreRequest 是 buei 定制「光鸭分享收藏」的驱动请求。
type GuangyaShareRestoreRequest struct {
	// AccessToken 由光鸭分享链接换取，与分享链接一一对应。
	AccessToken string
	// FileIDs 是分享内要转存的文件或目录 ID。
	FileIDs []string
	// ParentID 是当前账号内的目标目录 ID，空值表示账号根目录。
	ParentID string
}

// GuangyaShareRestorer 由光鸭驱动实现，用于把分享内容转存到当前账号。
//
// 这是 buei 定制能力，未实现该接口的驱动不应被转存目标选中。
type GuangyaShareRestorer interface {
	// RestoreShare 提交转存任务并尽量等待完成。
	// completed=false 表示任务已提交但等待超时，任务可能仍在服务端执行。
	RestoreShare(ctx context.Context, req GuangyaShareRestoreRequest) (taskID string, completed bool, err error)
}
