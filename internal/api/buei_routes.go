package api

import "github.com/go-chi/chi/v5"

// registerBueiRoutes 注册 buei 定制专用接口。
// 由 router.go 在管理员路由组内调用，按需继续追加 /tools/... 子路由。
func (h *Handler) registerBueiRoutes(r chi.Router) {
	r.Route("/tools/guangya-share", func(r chi.Router) {
		r.Post("/parse", h.bueiGuangyaShareParse)
		r.Post("/list", h.bueiGuangyaShareList)
		r.Post("/transfer", h.bueiGuangyaShareTransfer)
		r.Get("/progress", h.bueiGuangyaShareProgress)
		r.Post("/discard", h.bueiGuangyaShareDiscard)
		r.Get("/saved", h.bueiGuangyaShareSavedList)
		r.Put("/saved/{id}", h.bueiGuangyaShareSavedUpdate)
		r.Delete("/saved/{id}", h.bueiGuangyaShareSavedDelete)
	})
	r.Route("/tools/quark-ingest", func(r chi.Router) {
		r.Get("/preset", h.bueiQuarkIngestPreset)
		r.Get("/favorites", h.bueiQuarkIngestFavorites)
		r.Post("/favorites", h.bueiQuarkIngestFavoriteCreate)
		r.Put("/favorites/{id}", h.bueiQuarkIngestFavoriteUpdate)
		r.Delete("/favorites/{id}", h.bueiQuarkIngestFavoriteDelete)
		r.Post("/flatten", h.bueiQuarkIngestFlatten)
		r.Post("/transfer", h.bueiQuarkIngestTransfer)
		r.Get("/progress", h.bueiQuarkIngestProgress)
	})
}
