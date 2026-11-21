package embyproxy

import (
	"context"
	"strings"
)

// buei:emby-library —— 补全媒体库内容类型。
//
// Emby 的 /Library/SelectableMediaFolders 只用于选择目录，不一定返回 CollectionType；
// 而 /Library/VirtualFolders 会带上创建媒体库时选择的内容类型（movies / tvshows / music …）。
// 这里按 ItemId / Id / 名称把类型补回，混合内容库 Emby 本身不记录类型，仍由前端按名称兜底。

func (s *Service) enrichLibraryCollectionTypes(ctx context.Context, base, apiKey string, libraries []Library) {
	need := false
	for _, library := range libraries {
		if strings.TrimSpace(library.CollectionType) == "" {
			need = true
			break
		}
	}
	if !need {
		return
	}
	items, status, err := s.fetchLibraries(ctx, base+"/Library/VirtualFolders", apiKey)
	if err != nil || status >= 400 || len(items) == 0 {
		return
	}
	byID := make(map[string]string, len(items))
	byName := make(map[string]string, len(items))
	for _, item := range items {
		collectionType := strings.TrimSpace(anyString(item["CollectionType"]))
		if collectionType == "" {
			continue
		}
		for _, key := range []string{
			strings.TrimSpace(stringValue(item, "ItemId")),
			strings.TrimSpace(stringValue(item, "Id")),
		} {
			if key != "" {
				byID[strings.ToLower(key)] = collectionType
			}
		}
		if name := strings.TrimSpace(anyString(item["Name"])); name != "" {
			byName[strings.ToLower(name)] = collectionType
		}
	}
	for i := range libraries {
		if strings.TrimSpace(libraries[i].CollectionType) != "" {
			continue
		}
		if collectionType, ok := byID[strings.ToLower(strings.TrimSpace(libraries[i].ID))]; ok {
			libraries[i].CollectionType = collectionType
			continue
		}
		if collectionType, ok := byName[strings.ToLower(strings.TrimSpace(libraries[i].Name))]; ok {
			libraries[i].CollectionType = collectionType
		}
	}
}
