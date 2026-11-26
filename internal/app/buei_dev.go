package app

import (
	"os"
	"strings"
)

// bueiDevMode 判断是否处于本地开发环境（run.sh 会设置 LITEPAN_DEV=1）。
// 开发环境不启动 buei 的后台定时任务，避免本地调试时反复请求豆瓣等外部服务。
func bueiDevMode() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LITEPAN_DEV"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
