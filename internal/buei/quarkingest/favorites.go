package quarkingest

import (
	"context"
	"strings"

	"litepan/internal/domain"
)

// FavoriteInput 是新增/修改收藏的入参。
type FavoriteInput struct {
	AccountID      int64
	Link           string
	FolderID       string
	ParentFolderID string
	FolderName     string
}

// ListFavorites 返回指定夸克账号保存的收藏。
func (s *Service) ListFavorites(ctx context.Context, accountID int64) ([]*domain.QuarkIngestFavorite, error) {
	if s == nil || s.favorites == nil {
		return nil, domain.Errorf(domain.CodeInternal, "夸克入库收藏服务未初始化")
	}
	if err := s.validateQuarkAccount(ctx, accountID); err != nil {
		return nil, err
	}
	return s.favorites.ListByAccount(ctx, accountID)
}

// CreateFavorite 保存一条分享链接与「来自：分享」下的收藏文件夹。
func (s *Service) CreateFavorite(ctx context.Context, in FavoriteInput) (*domain.QuarkIngestFavorite, error) {
	if s == nil || s.favorites == nil {
		return nil, domain.Errorf(domain.CodeInternal, "夸克入库收藏服务未初始化")
	}
	if err := s.validateQuarkAccount(ctx, in.AccountID); err != nil {
		return nil, err
	}
	favorite, err := normalizeFavorite(in)
	if err != nil {
		return nil, err
	}
	return s.favorites.Create(ctx, favorite)
}

// UpdateFavorite 修改收藏链接与文件夹；收藏 ID 与账号必须匹配。
func (s *Service) UpdateFavorite(ctx context.Context, id int64, in FavoriteInput) (*domain.QuarkIngestFavorite, error) {
	if s == nil || s.favorites == nil {
		return nil, domain.Errorf(domain.CodeInternal, "夸克入库收藏服务未初始化")
	}
	if id <= 0 {
		return nil, domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
	if err := s.validateQuarkAccount(ctx, in.AccountID); err != nil {
		return nil, err
	}
	favorite, err := normalizeFavorite(in)
	if err != nil {
		return nil, err
	}
	favorite.ID = id
	return s.favorites.Update(ctx, favorite)
}

// DeleteFavorite 删除收藏，并一并删除「来自：分享」中对应的收藏文件夹。
func (s *Service) DeleteFavorite(ctx context.Context, id, accountID int64) error {
	if s == nil || s.favorites == nil {
		return domain.Errorf(domain.CodeInternal, "夸克入库收藏服务未初始化")
	}
	if id <= 0 {
		return domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
	if err := s.validateQuarkAccount(ctx, accountID); err != nil {
		return err
	}

	favorites, err := s.favorites.ListByAccount(ctx, accountID)
	if err != nil {
		return err
	}
	var favorite *domain.QuarkIngestFavorite
	for _, item := range favorites {
		if item != nil && item.ID == id {
			favorite = item
			break
		}
	}
	if favorite == nil {
		return domain.Errf(domain.CodeNotFound)
	}

	// 先删网盘文件夹，失败时保留收藏记录，便于用户重试。
	if s.files == nil {
		return domain.Errorf(domain.CodeInternal, "文件服务未初始化")
	}
	if err := s.files.DeleteFiles(ctx, accountID, []string{favorite.FolderID}, favorite.ParentFolderID); err != nil {
		// 收藏文件夹可能已被用户手动删除，此时网盘接口报错也应忽略，继续删除收藏记录。
		if appErr, ok := domain.AsAppError(err); ok && appErr.Code == domain.CodeNotFound {
			s.logger().Warn("夸克入库收藏文件夹已不存在，继续删除收藏记录", "account_id", accountID, "favorite_id", id, "folder_id", favorite.FolderID)
		} else if !s.favoriteFolderExists(ctx, accountID, favorite) {
			s.logger().Warn("夸克入库收藏文件夹已不存在，继续删除收藏记录", "account_id", accountID, "favorite_id", id, "folder_id", favorite.FolderID, "error", err)
		} else {
			s.logger().Warn("删除夸克入库收藏文件夹失败", "account_id", accountID, "favorite_id", id, "folder_id", favorite.FolderID, "error", err)
			return err
		}
	}

	if err := s.favorites.Delete(ctx, id, accountID); err != nil {
		return err
	}
	s.logger().Info("已删除夸克入库收藏及文件夹", "account_id", accountID, "favorite_id", id, "folder_id", favorite.FolderID)
	return nil
}

// favoriteFolderExists 判断收藏文件夹是否还在网盘上。
// 列表本身失败时按「仍存在」处理，避免把网络/认证错误当成不存在而误删收藏记录。
func (s *Service) favoriteFolderExists(ctx context.Context, accountID int64, favorite *domain.QuarkIngestFavorite) bool {
	if s == nil || s.files == nil || favorite == nil {
		return true
	}
	items, err := s.files.List(ctx, accountID, favorite.ParentFolderID, true)
	if err != nil {
		return true
	}
	for _, item := range items {
		if item.ID == favorite.FolderID {
			return true
		}
	}
	return false
}

func (s *Service) validateQuarkAccount(ctx context.Context, accountID int64) error {
	if s == nil || s.accounts == nil {
		return domain.Errorf(domain.CodeInternal, "账号服务未初始化")
	}
	if accountID <= 0 {
		return domain.Errorf(domain.CodeValidation, "账号 ID 不能为空")
	}
	account, err := s.accounts.Get(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil {
		return domain.Errorf(domain.CodeNotFound, "账号不存在")
	}
	if account.DriverType != "quark" {
		return domain.Errorf(domain.CodeValidation, "夸克入库只支持夸克网盘账号")
	}
	if !account.IsActive {
		return domain.Errorf(domain.CodeValidation, "目标账号已停用")
	}
	return nil
}

func normalizeFavorite(in FavoriteInput) (*domain.QuarkIngestFavorite, error) {
	link := strings.TrimSpace(in.Link)
	if link == "" {
		return nil, domain.Errorf(domain.CodeValidation, "收藏链接不能为空")
	}
	folderID := strings.TrimSpace(in.FolderID)
	if folderID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "请选择收藏文件夹")
	}
	folderName := strings.TrimSpace(in.FolderName)
	if folderName == "" {
		return nil, domain.Errorf(domain.CodeValidation, "收藏文件夹名称不能为空")
	}
	return &domain.QuarkIngestFavorite{
		AccountID:      in.AccountID,
		Link:           link,
		FolderID:       folderID,
		ParentFolderID: strings.TrimSpace(in.ParentFolderID),
		FolderName:     folderName,
	}, nil
}
