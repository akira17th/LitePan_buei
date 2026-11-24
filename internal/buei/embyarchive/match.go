package embyarchive

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/strmscrape"
)

const (
	archiveWord                    = "归档"
	archiveRecordFileName          = "buei_archive.json"
	archiveLibraryMovieName        = "归档 · 电影"
	archiveLibraryTVName           = "归档 · 剧集"
	bueiEmbyItemListPageLimit      = 200
	bueiEmbyItemListMaxPages       = 200
	bueiEmbyItemListDefaultSort    = strmscrape.ItemListSortTitleAsc
	bueiEmbyRefreshLibraryModeName = "library"
)

// FileOps 是归档需要的文件操作子集，便于单元测试替换。
type FileOps interface {
	List(ctx context.Context, accountID int64, parentID string, forceRefresh bool) ([]domain.FileItem, error)
	CreateFolder(ctx context.Context, accountID int64, parentID, name string) (*domain.FileItem, error)
	MoveFiles(ctx context.Context, accountID int64, fileIDs []string, targetParentID, sourceParentID string) error
	DeleteFiles(ctx context.Context, accountID int64, fileIDs []string, parentID string) error
}

// StrmOps 是 STRM 任务相关的依赖子集。
type StrmOps interface {
	ListTasks(ctx context.Context) ([]*domain.StrmTask, error)
	GetTask(ctx context.Context, id int64) (*domain.StrmTask, error)
	TryBeginTaskFileOperation(taskID int64) (func(), bool)
}

// ScrapeOps 是刮削索引相关的依赖子集。
type ScrapeOps interface {
	ListItems(ctx context.Context, strmTaskID int64, query strmscrape.ItemListQuery) (strmscrape.ItemListResult, error)
	RefreshIndex(ctx context.Context, strmTaskID int64, query strmscrape.ItemListQuery) (strmscrape.ItemListResult, error)
}

// EmbyOps 是 Emby/Jellyfin 媒体库相关的依赖子集。
type EmbyOps interface {
	ListLibraries(ctx context.Context, configIDs ...string) ([]embyproxy.Library, error)
	RefreshLibrary(ctx context.Context, req embyproxy.RefreshRequest) (embyproxy.RefreshResult, error)
	ListLibraryItemsRaw(ctx context.Context, configID, libraryID string) ([]map[string]any, error)
	ListLibraryItemUserStatuses(ctx context.Context, configID string, rawItems []map[string]any) (map[string][]embyproxy.LibraryUserItemStatus, error)
}

// MatchStrmTasks 按媒体库名称匹配 STRM 任务：
// 已禁用（status=paused）的任务不参与匹配；普通媒体库排除含「归档」的任务；
// 归档媒体库只匹配去掉「归档」后同类的、名称同样含「归档」的任务，不再回退到普通任务。
// 与前端 bueiEmbyLibraryTasks.ts 的规则保持一致。
func MatchStrmTasks(tasks []*domain.StrmTask, libraryName string) []*domain.StrmTask {
	libraryName = strings.ToLower(strings.TrimSpace(libraryName))
	if libraryName == "" {
		return nil
	}
	if !strings.Contains(libraryName, archiveWord) {
		out := make([]*domain.StrmTask, 0)
		for _, task := range tasks {
			if !matchableTask(task) {
				continue
			}
			taskName := strings.ToLower(strings.TrimSpace(task.Name))
			if !strings.Contains(taskName, archiveWord) && strings.Contains(taskName, libraryName) {
				out = append(out, task)
			}
		}
		return out
	}

	baseName := TrimArchiveSeparators(strings.TrimSpace(strings.ReplaceAll(libraryName, archiveWord, "")))
	if baseName == "" {
		return nil
	}
	archiveTasks := make([]*domain.StrmTask, 0)
	for _, task := range tasks {
		if !matchableTask(task) {
			continue
		}
		taskName := strings.ToLower(strings.TrimSpace(task.Name))
		if strings.Contains(taskName, archiveWord) && strings.Contains(taskName, baseName) {
			archiveTasks = append(archiveTasks, task)
		}
	}
	return archiveTasks
}

// matchableTask 过滤掉无效任务与已禁用（暂停）的任务。
func matchableTask(task *domain.StrmTask) bool {
	return task != nil && task.ID > 0 && task.Status != domain.StrmStatusPaused
}

// TrimArchiveSeparators 去掉「归档」两侧常见的分隔符。
func TrimArchiveSeparators(value string) string {
	return strings.Trim(value, " \t·•|/\\_-:：")
}

// ListAllStrmItems 分页读取一个 STRM 任务的全部刮削项目。
func ListAllStrmItems(ctx context.Context, scrape ScrapeOps, taskID int64) ([]strmscrape.Item, error) {
	all := make([]strmscrape.Item, 0, bueiEmbyItemListPageLimit)
	for offset, page := 0, 0; page < bueiEmbyItemListMaxPages; page++ {
		result, err := scrape.ListItems(ctx, taskID, strmscrape.ItemListQuery{
			Offset: offset,
			Limit:  bueiEmbyItemListPageLimit,
			Sort:   bueiEmbyItemListDefaultSort,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, result.Items...)
		if !result.HasMore || len(result.Items) == 0 {
			return all, nil
		}
		next := offset + len(result.Items)
		if next <= offset {
			return all, nil
		}
		offset = next
	}
	return all, nil
}

// ResolveLibraryName 根据配置与媒体库 ID 取媒体库名称。
func ResolveLibraryName(ctx context.Context, emby EmbyOps, configID, libraryID string) (string, error) {
	libraries, err := emby.ListLibraries(ctx, configID)
	if err != nil {
		return "", err
	}
	for _, library := range libraries {
		if library.ID == libraryID {
			if name := strings.TrimSpace(library.Name); name != "" {
				return name, nil
			}
		}
	}
	return "", domain.Errorf(domain.CodeNotFound, "所选 Emby/Jellyfin 媒体库不存在")
}

// FindLibraryByID 按 ID 查找媒体库。
func FindLibraryByID(ctx context.Context, emby EmbyOps, configID, libraryID string) (embyproxy.Library, error) {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return embyproxy.Library{}, domain.Errorf(domain.CodeValidation, "缺少 Emby 媒体库 ID")
	}
	libraries, err := emby.ListLibraries(ctx, configID)
	if err != nil {
		return embyproxy.Library{}, err
	}
	for _, library := range libraries {
		if library.ID == libraryID {
			return library, nil
		}
	}
	return embyproxy.Library{}, domain.Errorf(domain.CodeValidation, "Emby 媒体库不存在：%s", libraryID)
}

// FindLibraryByName 按名称查找媒体库，忽略空白的常见分隔符差异。
func FindLibraryByName(ctx context.Context, emby EmbyOps, configID, wanted string) (embyproxy.Library, error) {
	libraries, err := emby.ListLibraries(ctx, configID)
	if err != nil {
		return embyproxy.Library{}, err
	}
	for _, library := range libraries {
		if strings.TrimSpace(library.Name) == strings.TrimSpace(wanted) || compactLibraryName(library.Name) == compactLibraryName(wanted) {
			return library, nil
		}
	}
	return embyproxy.Library{}, domain.Errorf(domain.CodeValidation, "未找到 Emby 媒体库：%s", wanted)
}

func compactLibraryName(value string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '·', '•', '-', '_':
			return -1
		default:
			return r
		}
	}, strings.TrimSpace(value))
}

// RefreshLibraries 刷新若干 Emby 媒体库，ID 为空或重复时自动跳过。
func RefreshLibraries(ctx context.Context, emby EmbyOps, configID string, libraryIDs ...string) error {
	seen := make(map[string]struct{}, len(libraryIDs))
	for _, libraryID := range libraryIDs {
		libraryID = strings.TrimSpace(libraryID)
		if libraryID == "" {
			continue
		}
		if _, ok := seen[libraryID]; ok {
			continue
		}
		seen[libraryID] = struct{}{}
		if _, err := emby.RefreshLibrary(ctx, embyproxy.RefreshRequest{
			ConfigID:  strings.TrimSpace(configID),
			Mode:      bueiEmbyRefreshLibraryModeName,
			LibraryID: libraryID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ItemDisplayName 返回日志里展示项目用的名称。
func ItemDisplayName(item strmscrape.Item) string {
	if title := strings.TrimSpace(item.Title); title != "" {
		return title
	}
	if folder := strings.TrimSpace(item.FolderName); folder != "" {
		return folder
	}
	return item.ID
}

// EmbyMatchScore 计算 STRM 项目与 Emby 原始项目的匹配分数，0 表示不匹配。
func EmbyMatchScore(item strmscrape.Item, raw map[string]any) int {
	itemType := strings.ToLower(strings.TrimSpace(item.MediaType))
	rawType := strings.ToLower(strings.TrimSpace(RawString(raw, "Type")))
	if itemType == strmscrape.MediaTypeTV && rawType != "" && rawType != "series" {
		return 0
	}
	if itemType == strmscrape.MediaTypeMovie && rawType != "" && rawType != "movie" {
		return 0
	}

	itemTMDB := strings.TrimSpace(item.TMDBID)
	rawTMDB := RawProviderID(raw, "tmdb")
	if itemTMDB != "" && rawTMDB != "" && itemTMDB == rawTMDB {
		return 1000
	}

	rawName := RawString(raw, "Name")
	if rawName == "" {
		return 0
	}
	rawTitle := NormalizeMatchName(rawName)
	if rawTitle == "" {
		return 0
	}
	itemNames := []string{item.Title, item.FolderName, strings.TrimSuffix(item.StrmName, filepath.Ext(item.StrmName))}
	best := 0
	pathName := strings.ReplaceAll(RawString(raw, "Path"), "\\", "/")
	pathName = NormalizeMatchName(filepath.Base(pathName))
	if pathName != "" && NormalizeMatchName(item.FolderName) == pathName {
		best = 900
	}
	for _, candidate := range itemNames {
		candidate = NormalizeMatchName(candidate)
		if candidate == "" {
			continue
		}
		score := 0
		switch {
		case candidate == rawTitle:
			score = 700
		case len(candidate) >= 4 && (strings.Contains(candidate, rawTitle) || strings.Contains(rawTitle, candidate)):
			score = 300
		}
		if score == 0 {
			continue
		}
		rawYear := RawInt(raw["ProductionYear"])
		if item.Year != nil && rawYear > 0 {
			if *item.Year == rawYear {
				score += 150
			} else {
				score -= 200
			}
		}
		if score > best {
			best = score
		}
	}
	return best
}

// NormalizeMatchName 去掉标点与空白，只保留字母数字，便于跨语言匹配。
func NormalizeMatchName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// RawProviderID 读取 Emby 项目 ProviderIds 里的外部 ID。
func RawProviderID(raw map[string]any, wanted string) string {
	for key, value := range raw {
		if !strings.EqualFold(key, "ProviderIds") && !strings.EqualFold(key, "ProviderIDs") {
			continue
		}
		providers, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for key, value := range providers {
			key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
			if key == wanted || strings.Contains(key, wanted) {
				return strings.TrimSpace(fmt.Sprint(value))
			}
		}
	}
	return ""
}

// RawString 读取 Emby 原始项目里的字符串字段（键名不区分大小写）。
func RawString(raw map[string]any, key string) string {
	for name, value := range raw {
		if strings.EqualFold(name, key) && value != nil {
			return strings.TrimSpace(fmt.Sprint(value))
		}
	}
	return ""
}

// RawInt 读取 Emby 原始项目里的整数。
func RawInt(value any) int {
	switch value := value.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := strconv.Atoi(value.String())
		return parsed
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(value))
		return parsed
	default:
		return 0
	}
}

// SplitArchivePath 把 /a/b/c 拆成 [a b c]，忽略 . 与 ..。
func SplitArchivePath(raw string) []string {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		out = append(out, part)
	}
	return out
}

// JoinArchivePath 拼接网盘展示路径。
func JoinArchivePath(root, relative string) string {
	parts := append(SplitArchivePath(root), SplitArchivePath(relative)...)
	if len(parts) == 0 {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}

// TargetRelativePath 计算归档目标相对目录：
// 去掉源路径的顶级目录；目标根同名目录不重复；保留源项目相对目录的父级。
func TargetRelativePath(sourcePath, targetPath string, itemRel []string) []string {
	sourceParts := SplitArchivePath(sourcePath)
	targetParts := SplitArchivePath(targetPath)
	relativeRoot := make([]string, 0)
	if len(sourceParts) > 1 {
		relativeRoot = append(relativeRoot, sourceParts[1:]...)
	}
	if len(relativeRoot) > 0 && len(targetParts) > 0 && strings.EqualFold(relativeRoot[0], targetParts[len(targetParts)-1]) {
		relativeRoot = relativeRoot[1:]
	}
	if len(itemRel) > 1 {
		relativeRoot = append(relativeRoot, itemRel[:len(itemRel)-1]...)
	}
	return relativeRoot
}

func safeJoin(root string, parts []string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", domain.Wrap(domain.CodeInternal, err)
	}
	cleanParts := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.ReplaceAll(part, "\\", "/"))
		if part == "" || part == "." || part == ".." || strings.Contains(part, "/") {
			return "", domain.Errorf(domain.CodeValidation, "STRM 相对路径无效")
		}
		cleanParts = append(cleanParts, part)
	}
	joined := filepath.Join(append([]string{rootAbs}, cleanParts...)...)
	joinedAbs, err := filepath.Abs(joined)
	if err != nil {
		return "", domain.Wrap(domain.CodeInternal, err)
	}
	rel, err := filepath.Rel(rootAbs, joinedAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", domain.Errorf(domain.CodeValidation, "STRM 目标路径越界")
	}
	return joinedAbs, nil
}

func (s *Service) resolveDir(ctx context.Context, accountID int64, rootID string, relative []string) (*domain.FileItem, string, error) {
	parentID := strings.TrimSpace(rootID)
	var found *domain.FileItem
	for index, name := range relative {
		items, err := s.files.List(ctx, accountID, parentID, true)
		if err != nil {
			return nil, "", err
		}
		var matched *domain.FileItem
		for i := range items {
			if items[i].Name != name {
				continue
			}
			if matched != nil {
				return nil, "", domain.Errorf(domain.CodeValidation, "源路径存在同名项目：%s", name)
			}
			item := items[i]
			matched = &item
		}
		if matched == nil {
			return nil, "", domain.Errorf(domain.CodeNotFound, "源路径不存在：%s", name)
		}
		found = matched
		if index < len(relative)-1 {
			if !matched.IsDir {
				return nil, "", domain.Errorf(domain.CodeValidation, "源路径不是目录：%s", name)
			}
			parentID = matched.ID
		}
	}
	return found, parentID, nil
}

func (s *Service) resolveParentDir(ctx context.Context, accountID int64, rootID string, relative []string) (string, error) {
	parentID := strings.TrimSpace(rootID)
	if len(relative) <= 1 {
		return parentID, nil
	}
	for _, name := range relative[:len(relative)-1] {
		items, err := s.files.List(ctx, accountID, parentID, true)
		if err != nil {
			return "", err
		}
		var matched *domain.FileItem
		for i := range items {
			if items[i].Name != name {
				continue
			}
			if matched != nil {
				return "", domain.Errorf(domain.CodeValidation, "原始路径存在同名目录：%s", name)
			}
			item := items[i]
			matched = &item
		}
		if matched == nil || !matched.IsDir {
			return "", domain.Errorf(domain.CodeNotFound, "原始父目录不存在：%s", name)
		}
		parentID = matched.ID
	}
	return parentID, nil
}

func (s *Service) ensureDir(ctx context.Context, accountID int64, rootID string, relative []string) (string, error) {
	parentID := strings.TrimSpace(rootID)
	for _, name := range relative {
		items, err := s.files.List(ctx, accountID, parentID, true)
		if err != nil {
			return "", err
		}
		var matched *domain.FileItem
		for i := range items {
			if items[i].Name == name {
				if matched != nil {
					return "", domain.Errorf(domain.CodeValidation, "目标路径存在同名项目：%s", name)
				}
				item := items[i]
				matched = &item
			}
		}
		if matched == nil {
			created, err := s.files.CreateFolder(ctx, accountID, parentID, name)
			if err != nil {
				return "", err
			}
			matched = created
		}
		if !matched.IsDir {
			return "", domain.Errorf(domain.CodeValidation, "目标路径中的项目不是目录：%s", name)
		}
		parentID = matched.ID
	}
	return parentID, nil
}

func (s *Service) lockTasks(sourceID, targetID int64) (func(), error) {
	ids := []int64{sourceID}
	if targetID != sourceID {
		ids = append(ids, targetID)
	}
	if len(ids) == 2 && ids[0] > ids[1] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	releases := make([]func(), 0, len(ids))
	for _, id := range ids {
		release, ok := s.strm.TryBeginTaskFileOperation(id)
		if !ok {
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
			return nil, domain.Errorf(domain.CodeValidation, "相关 STRM 任务正在运行，请稍后再试")
		}
		releases = append(releases, release)
	}
	return func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}, nil
}

func (s *Service) refreshIndexes(ctx context.Context, taskIDs ...int64) error {
	seen := make(map[int64]struct{}, len(taskIDs))
	for _, taskID := range taskIDs {
		if taskID <= 0 {
			continue
		}
		if _, ok := seen[taskID]; ok {
			continue
		}
		seen[taskID] = struct{}{}
		if _, err := s.scrape.RefreshIndex(ctx, taskID, strmscrape.ItemListQuery{
			Offset: 0,
			Limit:  bueiEmbyItemListPageLimit,
			Sort:   bueiEmbyItemListDefaultSort,
		}); err != nil {
			return err
		}
	}
	return nil
}

func newLogger(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.Default()
	}
	return log
}

func nowUTC(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}

func writeFileAtomic(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
