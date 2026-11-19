package quarkingest

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"litepan/internal/domain"
)

const (
	maxFlattenFolders = 5000
	maxFlattenFiles   = 20000
)

// FlattenResult 描述一次“平铺目录”的结果。
type FlattenResult struct {
	Moved          int `json:"moved"`
	Renamed        int `json:"renamed"`
	DeletedFolders int `json:"deleted_folders"`
}

type flattenFolder struct {
	ID       string
	Name     string
	ParentID string
	Depth    int
}

type flattenFile struct {
	ID       string
	Name     string
	ParentID string
}

type flattenDeleteGroup struct {
	Depth    int
	ParentID string
	IDs      []string
}

// FlattenFavorite 将收藏文件夹下所有子文件夹中的文件递归移动到收藏文件夹根目录，
// 然后自底向上删除所有子文件夹。文件名冲突时追加 (1)、(2)…
func (s *Service) FlattenFavorite(ctx context.Context, accountID, favoriteID int64) (*FlattenResult, error) {
	if s == nil || s.favorites == nil || s.files == nil {
		return nil, domain.Errorf(domain.CodeInternal, "夸克入库服务未初始化")
	}
	if err := s.validateQuarkAccount(ctx, accountID); err != nil {
		return nil, err
	}
	if favoriteID <= 0 {
		return nil, domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}

	favorites, err := s.favorites.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	var favorite *domain.QuarkIngestFavorite
	for _, item := range favorites {
		if item != nil && item.ID == favoriteID {
			favorite = item
			break
		}
	}
	if favorite == nil {
		return nil, domain.Errf(domain.CodeNotFound)
	}
	folderID := strings.TrimSpace(favorite.FolderID)
	if folderID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "收藏文件夹不能为空")
	}

	folders, files, rootItems, err := s.collectFlattenEntries(ctx, accountID, folderID)
	if err != nil {
		return nil, err
	}
	if len(folders) == 0 {
		return &FlattenResult{}, nil
	}

	used := make(map[string]struct{}, len(rootItems)+len(files))
	for _, item := range rootItems {
		used[strings.ToLower(strings.TrimSpace(item.Name))] = struct{}{}
	}
	sourceReserved := make(map[string]map[string]struct{}, len(folders)+1)
	reserveSourceName := func(parentID, name string) {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			return
		}
		if sourceReserved[parentID] == nil {
			sourceReserved[parentID] = make(map[string]struct{})
		}
		sourceReserved[parentID][key] = struct{}{}
	}
	for _, folder := range folders {
		reserveSourceName(folder.ParentID, folder.Name)
	}
	for _, file := range files {
		reserveSourceName(file.ParentID, file.Name)
	}

	renamed := 0
	for index := range files {
		nextName := uniqueFlattenName(files[index].Name, used, sourceReserved[files[index].ParentID])
		if nextName != files[index].Name {
			if err := s.files.RenameFile(ctx, accountID, files[index].ID, nextName, files[index].ParentID); err != nil {
				s.logger().Warn("平铺目录时重命名文件失败", "account_id", accountID, "favorite_id", favoriteID, "file_id", files[index].ID, "name", files[index].Name, "new_name", nextName, "error", err)
				return nil, err
			}
			files[index].Name = nextName
			renamed++
		}
	}

	groups := make(map[string][]string)
	for _, file := range files {
		groups[file.ParentID] = append(groups[file.ParentID], file.ID)
	}
	parentIDs := make([]string, 0, len(groups))
	for parentID := range groups {
		parentIDs = append(parentIDs, parentID)
	}
	sort.Strings(parentIDs)
	for _, parentID := range parentIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.files.MoveFiles(ctx, accountID, groups[parentID], folderID, parentID); err != nil {
			s.logger().Warn("平铺目录时移动文件失败", "account_id", accountID, "favorite_id", favoriteID, "source", parentID, "target", folderID, "count", len(groups[parentID]), "error", err)
			return nil, err
		}
	}

	// 自底向上删除子文件夹；此时文件都已移动到收藏文件夹根目录。
	deleteGroups := make([]flattenDeleteGroup, 0, len(folders))
	indexByKey := make(map[string]int, len(folders))
	for _, folder := range folders {
		key := fmt.Sprintf("%d/%s", folder.Depth, folder.ParentID)
		if index, ok := indexByKey[key]; ok {
			deleteGroups[index].IDs = append(deleteGroups[index].IDs, folder.ID)
			continue
		}
		indexByKey[key] = len(deleteGroups)
		deleteGroups = append(deleteGroups, flattenDeleteGroup{
			Depth:    folder.Depth,
			ParentID: folder.ParentID,
			IDs:      []string{folder.ID},
		})
	}
	sort.Slice(deleteGroups, func(i, j int) bool {
		if deleteGroups[i].Depth != deleteGroups[j].Depth {
			return deleteGroups[i].Depth > deleteGroups[j].Depth
		}
		return deleteGroups[i].ParentID < deleteGroups[j].ParentID
	})
	for _, group := range deleteGroups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.files.DeleteFiles(ctx, accountID, group.IDs, group.ParentID); err != nil {
			s.logger().Warn("平铺目录时删除文件夹失败", "account_id", accountID, "favorite_id", favoriteID, "parent", group.ParentID, "count", len(group.IDs), "error", err)
			return nil, err
		}
	}

	result := &FlattenResult{
		Moved:          len(files),
		Renamed:        renamed,
		DeletedFolders: len(folders),
	}
	s.logger().Info("已平铺夸克入库收藏文件夹", "account_id", accountID, "favorite_id", favoriteID, "folder_id", folderID, "moved", result.Moved, "renamed", result.Renamed, "deleted_folders", result.DeletedFolders)
	return result, nil
}

func (s *Service) collectFlattenEntries(ctx context.Context, accountID int64, rootID string) ([]flattenFolder, []flattenFile, []domain.FileItem, error) {
	rootItems, err := s.files.List(ctx, accountID, rootID, true)
	if err != nil {
		return nil, nil, nil, err
	}

	folders := make([]flattenFolder, 0, 16)
	files := make([]flattenFile, 0, 64)
	queue := make([]flattenFolder, 0, 16)
	seen := map[string]struct{}{rootID: {}}
	for _, item := range rootItems {
		if !item.IsDir {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		folder := flattenFolder{ID: item.ID, Name: item.Name, ParentID: rootID, Depth: 1}
		folders = append(folders, folder)
		queue = append(queue, folder)
	}
	if len(folders) > maxFlattenFolders {
		return nil, nil, nil, domain.Errorf(domain.CodeValidation, "子文件夹过多，超过一次平铺上限")
	}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		folder := queue[0]
		queue = queue[1:]
		items, err := s.files.List(ctx, accountID, folder.ID, true)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, item := range items {
			if item.IsDir {
				if _, ok := seen[item.ID]; ok {
					continue
				}
				seen[item.ID] = struct{}{}
				child := flattenFolder{ID: item.ID, Name: item.Name, ParentID: folder.ID, Depth: folder.Depth + 1}
				folders = append(folders, child)
				queue = append(queue, child)
				continue
			}
			files = append(files, flattenFile{ID: item.ID, Name: item.Name, ParentID: folder.ID})
			if len(files) > maxFlattenFiles {
				return nil, nil, nil, domain.Errorf(domain.CodeValidation, "子文件夹文件过多，超过一次平铺上限")
			}
		}
		if len(folders) > maxFlattenFolders {
			return nil, nil, nil, domain.Errorf(domain.CodeValidation, "子文件夹过多，超过一次平铺上限")
		}
	}
	return folders, files, rootItems, nil
}

func uniqueFlattenName(original string, used, sourceReserved map[string]struct{}) string {
	name := strings.TrimSpace(original)
	if name == "" {
		name = "未命名文件"
	}
	if _, exists := used[strings.ToLower(name)]; !exists {
		used[strings.ToLower(name)] = struct{}{}
		return name
	}
	base, ext := splitFlattenName(name)
	for index := 1; ; index++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, index, ext)
		key := strings.ToLower(candidate)
		if _, exists := used[key]; exists {
			continue
		}
		if _, exists := sourceReserved[key]; exists {
			continue
		}
		used[key] = struct{}{}
		return candidate
	}
}

func splitFlattenName(name string) (string, string) {
	index := strings.LastIndex(name, ".")
	if index <= 0 || index == len(name)-1 {
		return name, ""
	}
	return name[:index], name[index:]
}
