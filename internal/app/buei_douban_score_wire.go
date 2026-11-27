package app

import (
	"litepan/internal/buei/doubanscore"
	"litepan/internal/embyproxy"
	"litepan/internal/logx"
	"litepan/internal/store"
	"litepan/internal/strm"
	"litepan/internal/strmscrape"
)

// wireBueiDoubanScore 装配豆瓣评分服务：按 STRM 任务海报墙查询并保存豆瓣评分，每天 08:00 自动刷新。
func wireBueiDoubanScore(logs *logx.Manager, st *storeBundle, strmSvc *strm.Service, scrape *strmscrape.Service, embySvc *embyproxy.Service) *doubanscore.Service {
	return doubanscore.New(doubanscore.Options{
		Tasks:   strmSvc,
		Items:   scrape,
		Scores:  store.NewBueiDoubanScoreRepo(st.db),
		Configs: st.store.Configs,
		Ratings: scrape, // 查到的评分一并写进作品 NFO 的社区评分
		// buei:emby-refresh —— 查到评分后触发对应 Emby 媒体库刷库。
		Libraries: store.NewBueiDoubanTaskLibraryRepo(st.db),
		Emby:      embySvc,
		Log:       logs.For(logx.ModuleSystem),
		// 开发环境不启动每天 08:00 的后台查询，避免本地调试时打豆瓣。
		SchedulerDisabled: bueiDevMode(),
	})
}
