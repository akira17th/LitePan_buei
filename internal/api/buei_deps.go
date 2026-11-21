package api

import (
	"litepan/internal/buei/embyarchive"
	"litepan/internal/buei/guangyashare"
	"litepan/internal/buei/quarkingest"
	"litepan/internal/domain"
)

// BueiDeps 集中承载 buei 定制服务，避免上游 Deps / Handler 结构体被反复修改。
type BueiDeps struct {
	GuangyaShare *guangyashare.Service
	QuarkIngest  *quarkingest.Service
	EmbyArchive  *embyarchive.Service
	// Emby 媒体库最近同步时间（“超过 12 小时自动同步”与按钮悬停提示）。
	EmbyLibrarySync domain.BueiEmbyLibrarySyncRepository
}
