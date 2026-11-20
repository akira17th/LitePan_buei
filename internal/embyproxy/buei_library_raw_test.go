package embyproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListLibraryItemsRawKeepsFullProjectFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Items" || r.URL.Query().Get("ParentId") != "lib-1" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("IncludeItemTypes") != "Movie,Series" {
			t.Fatalf("媒体类型过滤=%q", r.URL.Query().Get("IncludeItemTypes"))
		}
		fields := r.URL.Query().Get("Fields")
		if fields == "" || !strings.Contains(fields, "ProviderIds") {
			t.Fatalf("原始项目查询字段异常：fields=%q", fields)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"TotalRecordCount": 1,
			"Items": []map[string]any{{
				"Id": "emby-1", "Name": "测试电影", "Type": "Movie",
				"ProviderIds":  map[string]any{"Tmdb": "123"},
				"MediaSources": []any{map[string]any{"Path": "/media/test.mkv"}},
			}},
		})
	}))
	defer server.Close()

	items, err := testEmbyProxyService(t, server.URL).ListLibraryItemsRaw(context.Background(), "", "lib-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || bueiString(items[0], "Id") != "emby-1" {
		t.Fatalf("原始项目异常：%+v", items)
	}
	if _, ok := items[0]["MediaSources"]; !ok {
		t.Fatalf("未保留完整项目字段：%+v", items[0])
	}
}
