package embyproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBueiBrowseLibraryItemsIncludesPerUserWatchState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ApiKey") != "test-key" {
			http.Error(w, "missing api key", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/Library/SelectableMediaFolders":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"Id": "lib-1", "Name": "剧集"}})
		case "/Users":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"Id": "user-1", "Name": "小明"}})
		case "/Items":
			if r.URL.Query().Get("ParentId") != "lib-1" {
				http.Error(w, "missing library", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"TotalRecordCount": 2,
				"Items": []map[string]any{
					{"Id": "movie-1", "Name": "一部电影", "Type": "Movie", "ProductionYear": 2024},
					{"Id": "series-1", "Name": "一部剧集", "Type": "Series", "ProductionYear": 2025},
				},
			})
		case "/Users/user-1/Items":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Items": []map[string]any{
					{"Id": "movie-1", "UserData": map[string]any{"Played": true}},
					{"Id": "series-1", "UserData": map[string]any{"Played": false, "UnplayedItemCount": 3}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := testEmbyProxyService(t, server.URL)
	page, err := svc.BrowseLibraryItems(context.Background(), "", "lib-1", 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("媒体项目数量=%d，期望 2", len(page.Items))
	}
	if !page.Items[0].Users[0].Played {
		t.Fatal("电影已看状态未返回")
	}
	if page.Items[1].Users[0].Played || !page.Items[1].Users[0].RemainingKnown || page.Items[1].Users[0].RemainingEpisodes != 3 {
		t.Fatalf("剧集观看状态异常：%+v", page.Items[1].Users[0])
	}
}

func TestBueiBrowseLibraryItemDetailUsesItemsLookupAndSeasonFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items":
			if ids := r.URL.Query().Get("Ids"); ids == "series-1" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Items": []map[string]any{{
						"Id": "series-1", "Name": "测试剧集", "Type": "Series", "ProductionYear": 2025,
						"Overview": "详情简介", "UserData": map[string]any{"Played": false},
					}},
				})
				return
			}
			if r.URL.Query().Get("ParentId") == "series-1" && r.URL.Query().Get("IncludeItemTypes") == "Season" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Items": []map[string]any{{"Id": "season-1", "Name": "第一季", "Type": "Season", "IndexNumber": 1}},
				})
				return
			}
		case "/Users":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"Id": "user-1", "Name": "小明"}})
			return
		case "/Users/user-1/Items":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Items": []map[string]any{{"Id": "series-1", "UserData": map[string]any{"Played": false, "UnplayedItemCount": 2}}},
			})
			return
		case "/Shows/series-1/Seasons", "/emby/Shows/series-1/Seasons":
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := testEmbyProxyService(t, server.URL)
	detail, err := svc.BrowseLibraryItemDetail(context.Background(), "", "series-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Name != "测试剧集" || len(detail.Seasons) != 1 || detail.Seasons[0].ID != "season-1" {
		t.Fatalf("详情结果异常：%+v", detail)
	}
}
