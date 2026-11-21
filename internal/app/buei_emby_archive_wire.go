package app

import (
	"litepan/internal/buei/embyarchive"
	"litepan/internal/config"
	"litepan/internal/embyproxy"
	"litepan/internal/file"
	"litepan/internal/logx"
	"litepan/internal/store"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

// wireBueiEmbyArchive 装配 Emby 自动归档服务：归档/移回 + 定时规则 + 执行日志。
func wireBueiEmbyArchive(cfg config.Config, logs *logx.Manager, st *storeBundle, files *file.Service, strmSvc *strm.Service, scrape *strmscrape.Service, emby *embyproxy.Service) *embyarchive.Service {
	return embyarchive.New(embyarchive.Options{
		Files:   files,
		Strm:    strmSvc,
		Scrape:  scrape,
		Emby:    emby,
		StrmDir: cfg.StrmDir,
		Rules:   store.NewBueiEmbyArchiveRuleRepo(st.db),
		Runs:    store.NewBueiEmbyArchiveRunRepo(st.db),
		Log:     logs.For(logx.ModuleSystem),
	})
}
