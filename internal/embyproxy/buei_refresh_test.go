package embyproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestRefreshLibraryDirectPostsItemsRefresh(t *testing.T) {
	var (
		mu       sync.Mutex
		paths    []string
		query    string
		method   string
		authUsed string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		query = r.URL.RawQuery
		method = r.Method
		authUsed = r.Header.Get("X-Emby-Token") + r.Header.Get("Authorization")
		mu.Unlock()
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	svc := testEmbyProxyService(t, upstream.URL)
	if err := svc.RefreshLibraryDirect(context.Background(), "", "lib-1"); err != nil {
		t.Fatalf("直接刷库失败: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 || paths[0] != "/Items/lib-1/Refresh" {
		t.Fatalf("只应请求一次媒体库刷新，实际: %#v", paths)
	}
	if method != http.MethodPost {
		t.Fatalf("刷新应使用 POST，实际: %s", method)
	}
	if !strings.Contains(query, "Recursive=true") {
		t.Fatalf("刷新应带 Recursive=true，实际: %s", query)
	}
	if strings.TrimSpace(authUsed) == "" {
		t.Fatal("刷新请求应带认证信息")
	}
}

func TestRefreshLibraryDirectRequiresLibraryID(t *testing.T) {
	svc := testEmbyProxyService(t, "http://127.0.0.1:1")
	if err := svc.RefreshLibraryDirect(context.Background(), "", "  "); err == nil {
		t.Fatal("缺少媒体库 ID 时应返回错误")
	}
}
