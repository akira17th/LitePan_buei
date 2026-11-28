package api

import "github.com/go-chi/chi/v5"

// registerBueiRoutes 注册 buei 定制专用接口。
// 由 router.go 在管理员路由组内调用，按需继续追加 /tools/... 子路由。
func (h *Handler) registerBueiRoutes(r chi.Router) {
	// buei:emby-library —— 后台 Emby 媒体库浏览与删除。
	r.Route("/buei/emby-library", func(r chi.Router) {
		r.Get("/libraries", h.bueiEmbyLibraryState)
		r.Get("/items", h.bueiEmbyLibraryItems)
		r.Get("/items/{itemID}", h.bueiEmbyLibraryItemDetail)
		r.Delete("/items/{itemID}", h.bueiEmbyLibraryDeleteItem)
		r.Post("/sync", h.bueiEmbyLibrarySync)
		r.Post("/archive", h.bueiEmbyLibraryArchive)
		r.Post("/restore", h.bueiEmbyLibraryRestore)
		// buei:emby-delete —— 删除项目：网盘文件夹 + STRM 文件夹，并刷新索引与媒体库。
		r.Post("/delete", h.bueiEmbyLibraryDeleteProject)
		// buei:emby-project —— 项目 STRM 目录 / 网盘目录对照浏览。
		r.Get("/project-files", h.bueiEmbyLibraryProjectFiles)
		r.Post("/project-delete", h.bueiEmbyLibraryProjectDelete)
		// buei:emby-tmdb —— 项目目录里的 TMDB 搜索与手动匹配刮削。
		r.Get("/tmdb-search", h.bueiEmbyLibraryTMDBSearch)
		r.Post("/tmdb-match", h.bueiEmbyLibraryTMDBMatch)
		r.Post("/project-rescrape", h.bueiEmbyLibraryProjectRescrape)
		// buei:emby-strm —— 检测缺失的 STRM，并按需生成 + 自动刮削。
		r.Get("/project-strm-status", h.bueiEmbyLibraryProjectStrmStatus)
		r.Post("/project-strm-generate", h.bueiEmbyLibraryProjectStrmGenerate)
		r.Post("/project-strm-cleanup", h.bueiEmbyLibraryProjectStrmCleanup)
		r.Get("/sync-state", h.bueiEmbyLibrarySyncState)
		r.Get("/images/{itemID}", h.bueiEmbyLibraryImage)
		// buei:emby-archive —— 归档媒体库的自动归档配置、立即执行与执行日志。
		r.Get("/archive-rule", h.bueiEmbyArchiveRuleView)
		r.Put("/archive-rule", h.bueiEmbyArchiveRuleSave)
		r.Post("/archive-run", h.bueiEmbyArchiveRunNow)
		r.Get("/archive-logs", h.bueiEmbyArchiveLogs)
		// buei:emby-played —— 点击用户名切换“已看完 / 未看”。
		r.Post("/played", h.bueiEmbyLibraryUserPlayed)
		// buei:emby-tv-state —— 点击“已完结 / 未完结”切换完结状态。
		r.Post("/tv-state", h.bueiEmbyLibraryTVState)
	})
	// buei:douban-score —— 辅助工具「豆瓣评分」：按 STRM 任务海报墙查询与保存豆瓣评分。
	r.Route("/buei/douban-score", func(r chi.Router) {
		r.Get("/tasks", h.bueiDoubanScoreTasks)
		r.Get("/items", h.bueiDoubanScoreItems)
		r.Get("/plan", h.bueiDoubanScorePlan)
		r.Get("/state", h.bueiDoubanScoreState)
		r.Post("/query", h.bueiDoubanScoreQuery)
		r.Post("/stop", h.bueiDoubanScoreStop)
		// buei:douban-override —— 手动编辑豆瓣 ID / 评分 / 不再查询。
		r.Post("/override", h.bueiDoubanScoreOverride)
		// buei:douban-library —— 任务 ↔ Emby 媒体库关联（评分联动刷库）。
		r.Get("/libraries", h.bueiDoubanLibraryList)
		r.Post("/library", h.bueiDoubanLibrarySave)
	})
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
		// buei:quark ingest flash to guangya
		r.Get("/staged", h.bueiQuarkIngestStaged)
		r.Post("/cancel", h.bueiQuarkIngestCancel)
	})
}
