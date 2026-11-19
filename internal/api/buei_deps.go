package api

import (
	"litepan/internal/buei/guangyashare"
	"litepan/internal/buei/quarkingest"
)

// BueiDeps 集中承载 buei 定制服务，避免上游 Deps / Handler 结构体被反复修改。
type BueiDeps struct {
	GuangyaShare *guangyashare.Service
	QuarkIngest  *quarkingest.Service
}
