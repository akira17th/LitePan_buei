package quark

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

// buei 定制：夸克分享的转存能力。实现 driver.ShareRestorer，供「后台/辅助工具/
// 云盘分享入库」调用；分享解析与公开列表在 internal/buei/guangyashare 中完成。
const (
	pathShareSave = "/share/sharepage/save"

	// 大目录转存可能持续较久，等待超时后返回 completed=false，由前端提示用户稍后查看。
	shareSaveTimeout      = 2 * time.Minute
	shareSavePollInterval = 700 * time.Millisecond
)

// RestoreShare 把分享内选中的文件/目录转存到当前账号目录。
// 夸克要求按文件回传分享内 token，因此 Entries 里的 Token 不能为空。
func (d *Driver) RestoreShare(ctx context.Context, req driver.ShareRestoreRequest) (string, bool, error) {
	fidList := make([]string, 0, len(req.Entries))
	tokenList := make([]string, 0, len(req.Entries))
	seen := make(map[string]struct{}, len(req.Entries))
	for _, entry := range req.Entries {
		fid := strings.TrimSpace(entry.ID)
		token := strings.TrimSpace(entry.Token)
		if fid == "" || token == "" {
			continue
		}
		if _, ok := seen[fid]; ok {
			continue
		}
		seen[fid] = struct{}{}
		fidList = append(fidList, fid)
		tokenList = append(tokenList, token)
	}
	if len(fidList) == 0 {
		return "", false, domain.Errorf(domain.CodeValidation, "请选择要转存的文件（缺少夸克分享令牌，请重新解析分享）")
	}
	shareID := strings.TrimSpace(req.ShareID)
	accessToken := strings.TrimSpace(req.AccessToken)
	if shareID == "" || accessToken == "" {
		return "", false, domain.Errorf(domain.CodeValidation, "分享信息不完整，请重新解析分享链接")
	}
	parentID := strings.TrimSpace(req.ParentID)
	if parentID == "" {
		parentID = "0"
	}

	var data struct {
		TaskID string `json:"task_id"`
	}
	if _, err := d.apiRequest(ctx, http.MethodPost, pathShareSave, url.Values{}, map[string]any{
		"fid_list":       fidList,
		"fid_token_list": tokenList,
		"to_pdir_fid":    parentID,
		"pwd_id":         shareID,
		"stoken":         accessToken,
		"pdir_fid":       "0",
		"scene":          "link",
	}, &data); err != nil {
		return "", false, describeShareSaveError(err)
	}

	taskID := strings.TrimSpace(data.TaskID)
	if taskID == "" {
		return "", true, nil
	}
	completed, err := d.waitShareSaveTask(ctx, taskID)
	return taskID, completed, err
}

func (d *Driver) waitShareSaveTask(ctx context.Context, taskID string) (bool, error) {
	deadline := time.Now().Add(shareSaveTimeout)
	for attempt := 0; ; attempt++ {
		query := url.Values{"task_id": {taskID}, "retry_index": {strconv.Itoa(attempt)}}
		var data struct {
			Status int `json:"status"`
		}
		if _, err := d.apiRequest(ctx, http.MethodGet, pathTask, query, nil, &data); err != nil {
			return false, err
		}
		switch data.Status {
		case 2:
			return true, nil
		case 3:
			return false, domain.Errorf(domain.CodeDriverError, "夸克转存任务失败（任务状态 %d）", data.Status)
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(shareSavePollInterval):
		}
	}
}

// describeShareSaveError 把夸克业务错误转成更易理解的中文提示。
func describeShareSaveError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "41010") || strings.Contains(msg, "提取码"):
		return domain.Errorf(domain.CodeValidation, "分享提取码已失效，请重新解析分享链接")
	case strings.Contains(msg, "41004") || strings.Contains(msg, "41006") ||
		strings.Contains(msg, "分享不存在") || strings.Contains(msg, "已失效") ||
		strings.Contains(msg, "已过期") || strings.Contains(msg, "已取消"):
		return domain.Errorf(domain.CodeValidation, "分享不存在或已失效")
	case strings.Contains(msg, "空间不足") || strings.Contains(msg, "存储空间"):
		return domain.Errorf(domain.CodeValidation, "网盘空间不足，无法转存")
	case strings.Contains(msg, "已存在") || strings.Contains(msg, "重复"):
		return domain.Errorf(domain.CodeValidation, "目标目录内已存在同名内容")
	case strings.Contains(msg, "频繁") || strings.Contains(msg, "限流"):
		return domain.Errorf(domain.CodeRateLimited, "夸克接口请求过于频繁，请稍后重试")
	case strings.Contains(msg, "Cookie") || strings.Contains(msg, "登录") || strings.Contains(msg, "认证"):
		return domain.Errorf(domain.CodeAuthExpired, "夸克账号登录状态已失效，请重新获取 Cookie")
	}
	return err
}

var _ driver.ShareRestorer = (*Driver)(nil)
