package app

import (
	"litepan/internal/buei/embyarchive"
	"litepan/internal/buei/guangyashare"
	"litepan/internal/buei/quarkingest"
	"litepan/internal/cache"
	"litepan/internal/domain"
	"litepan/internal/logx"
	"litepan/internal/store"
)

// bueiServices 汇总 buei 定制服务，避免上游 servicesBundle 导入具体功能包。
type bueiServices struct {
	guangyaShare    *guangyashare.Service
	quarkIngest     *quarkingest.Service
	embyArchive     *embyarchive.Service
	embyLibrarySync domain.BueiEmbyLibrarySyncRepository
}

func wireBueiServices(st *storeBundle, core *coreBundle, logs *logx.Manager) *bueiServices {
	return &bueiServices{
		guangyaShare: guangyashare.New(guangyashare.Options{
			Exec:      core.exec,
			Accounts:  st.store.Accounts,
			Links:     store.NewGuangyaShareLinkRepo(st.db),
			Transfers: store.NewGuangyaShareTransferredRepo(st.db),
			Configs:   st.store.Configs,
			Log:       logs.For(logx.ModuleSystem),
			InvalidateDir: func(accountID int64, parentID string) {
				cache.InvalidateDirKeys(core.cache, accountID, parentID)
			},
		}),
		quarkIngest: quarkingest.New(quarkingest.Options{
			Accounts:  st.store.Accounts,
			Configs:   st.store.Configs,
			Favorites: store.NewQuarkIngestFavoriteRepo(st.db),
			Log:       logs.For(logx.ModuleSystem),
		}),
		embyLibrarySync: store.NewBueiEmbyLibrarySyncRepo(st.db),
	}
}
