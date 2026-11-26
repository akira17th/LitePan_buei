package api

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

// 新增 buei 路由时最容易漏掉 /buei 前缀，导致前端固定路径 404；这里把路径钉住。
func TestBueiDoubanScoreRoutesRegistered(t *testing.T) {
	h := &Handler{}
	r := chi.NewRouter()
	r.Route("/api/admin", func(r chi.Router) {
		h.registerBueiRoutes(r)
	})

	want := map[string]bool{
		"GET /api/admin/buei/douban-score/tasks":  false,
		"GET /api/admin/buei/douban-score/items":  false,
		"GET /api/admin/buei/douban-score/state":  false,
		"POST /api/admin/buei/douban-score/query": false,
		"POST /api/admin/buei/douban-score/stop":  false,
	}
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		if _, ok := want[key]; ok {
			want[key] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	for key, found := range want {
		if !found {
			t.Errorf("缺少路由 %s", key)
		}
	}
}
