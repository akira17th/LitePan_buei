package embyarchive

import (
	"context"
	"strings"

	"litepan/internal/domain"
)

// ProjectPath 把项目根目录与相对路径安全拼接，越界路径会被拒绝。
func ProjectPath(root string, relative []string) (string, error) {
	return safeJoin(root, relative)
}

// BrowseCloudDir 返回项目相对路径对应网盘目录的内容：
// 相对路径为空时返回任务根目录本身，否则逐级解析后列出目标目录。
func (s *Service) BrowseCloudDir(ctx context.Context, accountID int64, rootID string, relative []string) ([]domain.FileItem, error) {
	rootID = strings.TrimSpace(rootID)
	if len(relative) == 0 {
		return s.files.List(ctx, accountID, rootID, false)
	}
	folder, _, err := s.resolveDir(ctx, accountID, rootID, relative)
	if err != nil {
		if appErr, ok := domain.AsAppError(err); ok && appErr.Code == domain.CodeNotFound {
			return nil, domain.Errorf(domain.CodeNotFound, "网盘目录不存在：%s", strings.Join(relative, "/"))
		}
		return nil, err
	}
	if folder == nil || !folder.IsDir {
		return nil, domain.Errorf(domain.CodeValidation, "网盘项目不是文件夹：%s", strings.Join(relative, "/"))
	}
	return s.files.List(ctx, accountID, folder.ID, false)
}
