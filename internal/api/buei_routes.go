package api

import "github.com/go-chi/chi/v5"

// registerBueiRoutes 注册 buei 定制专用接口。
// 由 router.go 在管理员路由组内调用，按需继续追加 /tools/... 子路由。
func (h *Handler) registerBueiRoutes(r chi.Router) {
	r.Route("/tools/guangya-share", func(r chi.Router) {
		r.Post("/parse", h.bueiGuangyaShareParse)
		r.Post("/list", h.bueiGuangyaShareList)
		r.Post("/transfer", h.bueiGuangyaShareTransfer)
		r.Get("/saved", h.bueiGuangyaShareSavedList)
		r.Delete("/saved/{id}", h.bueiGuangyaShareSavedDelete)
	})
}
