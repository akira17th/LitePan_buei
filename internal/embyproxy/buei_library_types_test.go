package embyproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListLibrariesEnrichesCollectionTypeFromVirtualFolders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/Library/SelectableMediaFolders"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"Id":"lib-1","Name":"电影"},{"Id":"lib-2","Name":"综艺"},{"Id":"lib-3","Name":"自定义"}]`)
		case strings.HasSuffix(r.URL.Path, "/Library/VirtualFolders"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[
				{"Name":"电影","ItemId":"lib-1","CollectionType":"movies"},
				{"Name":"综艺","ItemId":"lib-2","CollectionType":"tvshows"}
			]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	svc := testEmbyProxyService(t, upstream.URL)
	libraries, err := svc.ListLibraries(context.Background())
	if err != nil {
		t.Fatalf("ListLibraries 返回错误: %v", err)
	}
	if len(libraries) != 3 {
		t.Fatalf("媒体库数量异常: %#v", libraries)
	}
	if libraries[0].CollectionType != "movies" || libraries[1].CollectionType != "tvshows" {
		t.Fatalf("应从 VirtualFolders 补全内容类型: %#v", libraries)
	}
	if libraries[2].CollectionType != "" {
		t.Fatalf("Emby 未记录类型的媒体库应保持为空: %#v", libraries[2])
	}
}
