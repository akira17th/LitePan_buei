package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

func bueiSyncedProjectDTOs(projects []bueiSyncedProject) []bueiEmbyLibrarySyncItemDTO {
	out := make([]bueiEmbyLibrarySyncItemDTO, 0, len(projects))
	for _, project := range projects {
		out = append(out, bueiEmbyLibrarySyncItemDTO{
			Key:           project.Key,
			TotalEpisodes: project.TotalEpisodes,
			Users:         bueiUserStatusDTOs(project.Users),
		})
	}
	return out
}

func bueiUserStatusDTOs(statuses []embyproxy.LibraryUserItemStatus) []bueiEmbyLibraryUserStatusDTO {
	out := make([]bueiEmbyLibraryUserStatusDTO, 0, len(statuses))
	for _, status := range statuses {
		var remaining *int
		if status.RemainingKnown {
			value := status.RemainingEpisodes
			remaining = &value
		}
		out = append(out, bueiEmbyLibraryUserStatusDTO{
			UserID:            status.UserID,
			UserName:          status.UserName,
			PrimaryImageTag:   status.PrimaryImageTag,
			Played:            status.Played,
			WatchedEpisodes:   status.WatchedEpisodes,
			RemainingEpisodes: remaining,
			TotalEpisodes:     status.TotalEpisodes,
		})
	}
	return out
}

// bueiEmbyUserInfoLocation 返回用户信息 JSON 所在的目录与文件名前缀：
// 项目目录下直接存为 emby_users_<用户名>.json；
// 平铺 STRM 项目（<项目>.emby.json）与其它项目共用目录，前缀带上项目名避免冲突。
func bueiEmbyUserInfoLocation(projectPath string) (dir, prefix string) {
	dir = filepath.Dir(projectPath)
	base := filepath.Base(projectPath)
	if base == bueiEmbyProjectInfoFileName {
		return dir, bueiEmbyUserInfoPrefix + "_"
	}
	base = strings.TrimSuffix(base, ".emby.json")
	return dir, base + "." + bueiEmbyUserInfoPrefix + "_"
}

func bueiMatchEmbyUserInfoFile(prefix, name string) bool {
	return strings.HasPrefix(name, prefix) && strings.EqualFold(filepath.Ext(name), ".json")
}

func bueiWriteEmbyUserItems(projectPath string, users []embyproxy.LibraryUserItemStatus) error {
	dir, prefix := bueiEmbyUserInfoLocation(projectPath)
	// 清理旧版 emby_users/ 目录与本项目之前的用户文件，避免残留已移除的用户。
	if err := os.RemoveAll(filepath.Join(dir, strings.TrimSuffix(prefix, "_"))); err != nil {
		return fmt.Errorf("清理旧用户信息：%w", err)
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !bueiMatchEmbyUserInfoFile(prefix, entry.Name()) {
				continue
			}
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return fmt.Errorf("清理用户信息“%s”：%w", entry.Name(), err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取用户信息目录：%w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建用户信息目录：%w", err)
	}
	for _, user := range users {
		stored := bueiStoredEmbyUserItem{
			User:              embyproxy.LibraryBrowseUser{ID: user.UserID, Name: user.UserName, PrimaryImageTag: user.PrimaryImageTag},
			ItemID:            user.ItemID,
			ItemName:          user.ItemName,
			ItemType:          user.ItemType,
			Played:            user.Played,
			WatchedEpisodes:   user.WatchedEpisodes,
			RemainingEpisodes: user.RemainingEpisodes,
			RemainingKnown:    user.RemainingKnown,
			TotalEpisodes:     user.TotalEpisodes,
			UserData:          user.UserData,
			Raw:               user.Raw,
		}
		body, err := json.MarshalIndent(stored, "", "  ")
		if err != nil {
			return fmt.Errorf("编码用户“%s”信息：%w", user.UserName, err)
		}
		body = append(body, '\n')
		userName := strings.TrimSpace(user.UserName)
		if userName == "" {
			return fmt.Errorf("用户名称为空，无法保存用户信息")
		}
		name := strm.SafeName(userName)
		if name == "" || name == "_" {
			return fmt.Errorf("用户名称无效：%s", userName)
		}
		path := filepath.Join(dir, prefix+name+".json")
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return fmt.Errorf("写入用户“%s”信息：%w", user.UserName, err)
		}
	}
	return nil
}

func bueiReadEmbyUserItems(projectPath string) ([]embyproxy.LibraryUserItemStatus, int, error) {
	dir, prefix := bueiEmbyUserInfoLocation(projectPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	statuses := make([]embyproxy.LibraryUserItemStatus, 0, len(entries))
	total := 0
	for _, entry := range entries {
		if entry.IsDir() || !bueiMatchEmbyUserInfoFile(prefix, entry.Name()) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, 0, err
		}
		var stored bueiStoredEmbyUserItem
		if err := json.Unmarshal(body, &stored); err != nil {
			return nil, 0, fmt.Errorf("解析用户信息“%s”：%w", entry.Name(), err)
		}
		statuses = append(statuses, embyproxy.LibraryUserItemStatus{
			UserID:            stored.User.ID,
			UserName:          stored.User.Name,
			PrimaryImageTag:   stored.User.PrimaryImageTag,
			ItemID:            stored.ItemID,
			ItemName:          stored.ItemName,
			ItemType:          stored.ItemType,
			Played:            stored.Played,
			WatchedEpisodes:   stored.WatchedEpisodes,
			RemainingEpisodes: stored.RemainingEpisodes,
			RemainingKnown:    stored.RemainingKnown,
			TotalEpisodes:     stored.TotalEpisodes,
			UserData:          stored.UserData,
			Raw:               stored.Raw,
		})
		if stored.TotalEpisodes > total {
			total = stored.TotalEpisodes
		}
	}
	return statuses, total, nil
}

func bueiReadStoredEmbyProject(path string) (map[string]any, []embyproxy.LibraryUserItemStatus, int, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, 0, nil
		}
		return nil, nil, 0, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, 0, fmt.Errorf("解析 Emby 项目信息：%w", err)
	}
	users, total, err := bueiReadEmbyUserItems(path)
	if err != nil {
		return nil, nil, 0, err
	}
	if rawTotal := embyarchive.RawInt(raw["RecursiveItemCount"]); rawTotal > total {
		total = rawTotal
	}
	return raw, users, total, nil
}

func bueiBuildSyncState(ctx context.Context, strmDir string, scrape *strmscrape.Service, tasks []*domain.StrmTask) ([]bueiEmbyLibrarySyncItemDTO, error) {
	out := make([]bueiEmbyLibrarySyncItemDTO, 0)
	for _, task := range tasks {
		if task == nil {
			continue
		}
		items, err := bueiListAllStrmItems(ctx, scrape, task.ID)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			path, err := bueiEmbyProjectInfoPath(strmDir, task, item)
			if err != nil {
				return nil, err
			}
			_, users, total, err := bueiReadStoredEmbyProject(path)
			if err != nil {
				return nil, err
			}
			if len(users) == 0 && total == 0 {
				continue
			}
			out = append(out, bueiEmbyLibrarySyncItemDTO{
				Key:           fmt.Sprintf("%d:%s", task.ID, item.ID),
				TotalEpisodes: total,
				Users:         bueiUserStatusDTOs(users),
			})
		}
	}
	return out, nil
}
