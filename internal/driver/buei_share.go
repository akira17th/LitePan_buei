package driver

import "context"

// ShareRestoreEntry 是分享内要转存的一个条目。
type ShareRestoreEntry struct {
	// ID 是分享内的文件或目录 ID。
	ID string
	// Token 是分享内的文件令牌，夸克转存时必须回传；光鸭为空。
	Token string
}

// ShareRestoreRequest 是 buei 定制「云盘分享入库」的驱动请求。
type ShareRestoreRequest struct {
	// Provider 是分享所属网盘标识（guangya / quark），与驱动类型一致。
	Provider string
	// ShareID 是分享 ID。
	ShareID string
	// AccessToken 由分享链接换取，与分享链接一一对应。
	AccessToken string
	// Entries 是分享内要转存的文件或目录。
	Entries []ShareRestoreEntry
	// ParentID 是当前账号内的目标目录 ID，空值表示账号根目录。
	ParentID string
}

// ShareRestorer 由支持的网盘驱动实现，用于把分享内容转存到当前账号。
//
// 这是 buei 定制能力，未实现该接口的驱动不应被转存目标选中。
type ShareRestorer interface {
	// RestoreShare 提交转存任务并尽量等待完成。
	// completed=false 表示任务已提交但等待超时，任务可能仍在服务端执行。
	RestoreShare(ctx context.Context, req ShareRestoreRequest) (taskID string, completed bool, err error)
}
