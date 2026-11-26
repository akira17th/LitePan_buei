#!/bin/bash
set -Eeuo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Go 本地环境
export GOROOT="$PROJECT_DIR/.tools/go"
export PATH="$GOROOT/bin:$PATH"
export GOPATH="$PROJECT_DIR/.tools/gopath"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOCACHE="$PROJECT_DIR/.tools/gocache"
export GOTOOLCHAIN=local
export LITEPAN_CORS_ORIGINS="http://10.0.0.5:5173,http://10.0.0.5:5211"
# 本地开发：不启动 buei 定时后台任务（豆瓣评分每日 08:00 查询等）
export LITEPAN_DEV=1

# 检查环境
for cmd in go npm; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "错误：找不到 $cmd"
        exit 1
    fi
done

# 确保依赖已安装
cd "$PROJECT_DIR"
go mod download

cd "$PROJECT_DIR/web"
if [ ! -d node_modules ]; then
    echo "首次安装前端依赖..."
    npm ci
fi

# 退出时同时关闭前后端
cleanup() {
    echo "正在关闭 LitePan..."
    kill "$BACKEND_PID" "$FRONTEND_PID" 2>/dev/null || true
    wait "$BACKEND_PID" "$FRONTEND_PID" 2>/dev/null || true
}

trap cleanup EXIT INT TERM

# 启动后端
echo "启动 Go 后端 :5211..."
cd "$PROJECT_DIR"
go run ./cmd/litepan \
    -listen :5211 \
    -data-dir ./data \
    -strm-dir ./strm &
BACKEND_PID=$!

# 启动前端
echo "启动 Vue 前端 :5173..."
cd "$PROJECT_DIR/web"
npm run dev -- --host 0.0.0.0 &
FRONTEND_PID=$!

echo ""
echo "LitePan 已启动"
echo "前端：http://服务器IP:5173"
echo "后端：http://服务器IP:5211"
echo "按 Ctrl+C 停止服务"
echo ""

wait