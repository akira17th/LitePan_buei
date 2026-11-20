package embyproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListLibraryItemUserStatusesKeepsPerUserProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Users":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"Id": "u1", "Name": "小明"}, {"Id": "u2", "Name": "小红"}})
		case "/Users/u1/Items":
			_ = json.NewEncoder(w).Encode(map[string]any{"Items": []map[string]any{
				{"Id": "series-1", "Name": "测试剧集", "Type": "Series", "RecursiveItemCount": 10, "UserData": map[string]any{"Played": false, "UnplayedItemCount": 8, "LastPlayedDate": "2025-01-01"}},
			}})
		case "/Users/u2/Items":
			_ = json.NewEncoder(w).Encode(map[string]any{"Items": []map[string]any{
				{"Id": "series-1", "Name": "测试剧集", "Type": "Series", "RecursiveItemCount": 10, "UserData": map[string]any{"Played": false, "UnplayedItemCount": 7}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	statuses, err := testEmbyProxyService(t, server.URL).ListLibraryItemUserStatuses(context.Background(), "", []map[string]any{{
		"Id": "series-1", "Name": "测试剧集", "Type": "Series", "RecursiveItemCount": 10,
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := statuses["series-1"]
	if len(got) != 2 {
		t.Fatalf("用户状态数量=%d，期望 2", len(got))
	}
	if got[0].RemainingEpisodes != 8 || got[0].WatchedEpisodes != 2 || got[0].TotalEpisodes != 10 {
		t.Fatalf("第一个用户状态异常：%+v", got[0])
	}
	if got[0].UserData["LastPlayedDate"] != "2025-01-01" {
		t.Fatalf("未保留用户原始 UserData：%+v", got[0].UserData)
	}
	if got[1].RemainingEpisodes != 7 || got[1].WatchedEpisodes != 3 {
		t.Fatalf("第二个用户状态异常：%+v", got[1])
	}
}
