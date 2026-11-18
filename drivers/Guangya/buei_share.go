package guangya

import (
	"context"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

// buei 定制：光鸭分享的转存能力。实现 driver.GuangyaShareRestorer，供「后台/辅助工具/
// 光鸭分享」调用；分享解析与公开列表在 internal/buei/guangyashare 中完成。
const (
	pathShareRestore = "/userres/v1/restore_share"

	// 大目录转存可能持续较久，等待超时后返回 completed=false，由前端提示用户稍后查看。
	shareRestoreTimeout      = 2 * time.Minute
	shareRestorePollInterval = 600 * time.Millisecond
)

// RestoreShare 提交分享转存任务并等待结果。
func (d *Driver) RestoreShare(ctx context.Context, req driver.GuangyaShareRestoreRequest) (string, bool, error) {
	ids := normalizeIDs(req.FileIDs)
	if len(ids) == 0 {
		return "", false, domain.Errorf(domain.CodeValidation, "请选择要转存的文件")
	}
	accessToken := strings.TrimSpace(req.AccessToken)
	if accessToken == "" {
		return "", false, domain.Errorf(domain.CodeValidation, "分享访问令牌为空，请重新解析分享链接")
	}

	var data taskData
	if err := d.apiRequest(ctx, pathShareRestore, map[string]any{
		"accessToken": accessToken,
		"fileIds":     ids,
		"parentId":    d.resolveParent(req.ParentID),
	}, &data); err != nil {
		return "", false, describeShareRestoreError(err)
	}

	taskID := strings.TrimSpace(data.TaskID)
	if taskID == "" {
		return "", true, nil
	}
	completed, err := d.waitShareRestore(ctx, taskID)
	return taskID, completed, err
}

func (d *Driver) waitShareRestore(ctx context.Context, taskID string) (bool, error) {
	deadline := time.Now().Add(shareRestoreTimeout)
	for {
		var data taskStatusData
		if err := d.apiRequest(ctx, pathTaskStatus, map[string]any{"taskId": taskID}, &data); err != nil {
			return false, err
		}
		switch data.Status {
		case taskStatusDone:
			return true, nil
		case -1, taskStatusFail:
			return false, domain.Errorf(domain.CodeDriverError, "光鸭转存失败（任务状态 %d）", data.Status)
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		if err := sleepCtx(ctx, shareRestorePollInterval); err != nil {
			return false, err
		}
	}
}

// describeShareRestoreError 把光鸭业务错误码转成更易理解的中文提示。
// 这里只处理转存链路上的常见错误，其余保留驱动原始错误。
func describeShareRestoreError(err error) error {
	ae, ok := domain.AsAppError(err)
	if !ok {
		return err
	}
	msg := ae.Message
	switch {
	case strings.Contains(msg, "204") || strings.Contains(msg, "不能转存自己"):
		return domain.Errorf(domain.CodeValidation, "不能转存自己分享的文件")
	case strings.Contains(msg, "157") || strings.Contains(msg, "空间不足"):
		return domain.Errorf(domain.CodeValidation, "网盘空间不足，无法转存")
	case strings.Contains(msg, "212"):
		return domain.Errorf(domain.CodeValidation, "转存文件数量超出网盘限制")
	case strings.Contains(msg, "210") || strings.Contains(msg, "accessToken"):
		return domain.Errorf(domain.CodeValidation, "分享访问令牌已失效，请重新解析分享链接")
	case strings.Contains(msg, "354") || strings.Contains(msg, "限流"):
		return domain.Errorf(domain.CodeRateLimited, "光鸭接口请求过于频繁，请稍后重试")
	}
	return err
}

var _ driver.GuangyaShareRestorer = (*Driver)(nil)
