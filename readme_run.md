# LitePan 本地运行 / GitHub 自动打包 / 手动打 Docker 镜像 指南

> 适用仓库：`LitePan_buei`（Go 版 LitePan）
> 后端：Go + chi + FUSE/WebDAV，前端：Vue 3 + Vite（构建产物内嵌进 Go 二进制）

---

## 目录

1. [项目概览](#1-项目概览)
2. [前置要求](#2-前置要求)
3. [本地运行](#3-本地运行)
4. [推送到 GitHub 自动打包 Docker 镜像](#4-推送到-github-自动打包-docker-镜像)
5. [手动生成 Docker 镜像](#5-手动生成-docker-镜像)
6. [常见问题](#6-常见问题)

---

## 1. 项目概览

| 项 | 说明 |
| --- | --- |
| 后端入口 | `cmd/litepan/main.go`，二进制名 `litepan` |
| 前端 | `web/`，`npm run build` 后产物输出到 `internal/api/web/`，由 Go `//go:embed web` 内嵌 |
| 默认端口 | `5211`（HTTP），`42069`（Magnet TCP/uTP/DHT） |
| 默认账号 | 管理员密码 `admin` |
| 数据目录 | `./data`（含 `litepan.db`、`log/`），STRM 输出 `./strm`，挂载点 `./mounts` |
| FUSE | 可选功能，需要宿主机 `/dev/fuse` 权限，Docker 内需 `privileged` + `pid: host` |
| 环境变量 | `LITEPAN_DATA_DIR`、`LITEPAN_STRM_DIR`、`LITEPAN_DB_PATH`、`LITEPAN_LISTEN`、`LITEPAN_LOG_LEVEL` |

> ⚠️ 官方镜像 `ponphil/litepan:latest` 是 Python 旧版，**不要用**。Go 版对应 `ponphil/litepan:beta`（或 `v0.5.1-Beta` 等版本标签）。

---

## 2. 前置要求

| 工具 | 版本要求 | 用途 |
| --- | --- | --- |
| Go | `>= 1.26.4`（与 `go.mod` 对齐） | 编译后端 |
| Node.js | `>= 20` | 构建前端 |
| npm | `>= 10` | 前端依赖 |
| Docker | `>= 24` | 构建/运行镜像 |
| Docker Compose | `v2` | 一键部署 |
| git | 任意 | 拉取/推送 |

检查命令：

```bash
go version
node --version && npm --version
docker --version
docker compose version
```

---

## 3. 本地运行

### 3.1 方式一：Docker Compose（最简单，推荐）

直接使用官方已发布镜像运行（无需本地编译）：

```bash
cd LitePan_buei
docker compose up -d
```

- 打开 `http://你的IP:5211`，默认密码 `admin`。
- 数据会落在当前目录的 `data/`、`strm/`、`mounts/` 下。
- 停止：`docker compose down`

> 如果你要的是「用自己改过的源码打包的镜像」，请先看 [第 5 节手动打镜像](#5-手动生成-docker-镜像)，然后把 `docker-compose.yml` 里的
> `image: ponphil/litepan:beta` 改成你本地打好的镜像名，再 `docker compose up -d`。

### 3.2 方式二：源码开发模式（前后端分离，改代码热更新）

后端和前端分开跑，适合边改边调。

**① 启动后端（监听 5211）**

```bash
cd LitePan_buei
go run ./cmd/litepan
# 或指定参数：
go run ./cmd/litepan -listen :5211 -data-dir ./data -strm-dir ./strm
```

**② 另开一个终端启动前端（监听 5173，自动代理 /api 到后端）**

```bash
cd LitePan_buei/web
npm ci          # 首次需要，或 npm install
npm run dev
```

- 前端开发地址：`http://127.0.0.1:5173`（改前端代码热更新）
- 后端内嵌页地址：`http://127.0.0.1:5211`（后端重启后生效）

> `web/vite.config.ts` 中 dev 代理默认指向 `http://127.0.0.1:5211`，
> 可用环境变量 `LITEPAN_API_PROXY` 覆盖。

### 3.3 方式三：源码构建单二进制后运行（生产形态）

前端产物内嵌进 Go 二进制，最终只跑一个文件。

```bash
cd LitePan_buei

# 1) 构建前端（产物输出到 internal/api/web/，仓库里已自带一份）
cd web
npm ci
npm run build
cd ..

# 2) 编译后端（带 fuse 支持）
go build -tags fuse -trimpath -ldflags="-s -w" -o litepan ./cmd/litepan

# 3) 运行
./litepan
```

- 打开 `http://127.0.0.1:5211`
- 数据默认在 `./data`、STRM 在 `./strm`
- 如果不需要 FUSE 挂载，去掉 `-tags fuse` 即可（等同于 `make build-nofuse`）

> 说明：仓库中 `internal/api/web/` 已经提交了预构建前端资源，所以即使不跑 `npm run build`，
> 直接 `go build` 也能成功；重新 `npm run build` 只是刷新前端产物。

---

## 4. 推送到 GitHub 自动打包 Docker 镜像

原理：用 **GitHub Actions**，在 `push`（推分支 / 打 tag）时自动执行 `Dockerfile` 构建，并把镜像推送到 GitHub Container Registry（GHCR）。推送代码后立即自动触发。

### 4.1 无需额外配置 Secrets

推送到 GHCR 使用 GitHub 自带的 `GITHUB_TOKEN`（workflow 里已声明 `packages: write` 权限），
**不需要**再添加 Docker Hub 账号或 PAT 等任何凭据。

> 首次构建成功后，镜像会出现在仓库的 `Packages` 页面。GHCR 默认是私有包，
> 如需 `docker pull` 免登录拉取，到该包的 `Package settings → Change visibility` 改成 Public。

### 4.2 创建工作流文件

在仓库根目录新建：

```bash
mkdir -p .github/workflows
```

创建 `.github/workflows/docker-build.yml`，内容如下：

```yaml
name: Build and Publish Docker Image

on:
  push:
    branches: [main]      # 推 main 分支立即触发
    tags: ['v*']          # 打 v 开头的 tag 触发
  workflow_dispatch:      # 允许在 Actions 页面手动触发

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: litepan_buei   # 镜像名（GHCR 要求全小写）

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write      # 推送 GHCR 所需权限
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up QEMU
        uses: docker/setup-qemu-action@v3

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata (tags, labels)
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ github.repository_owner }}/${{ env.IMAGE_NAME }}
          tags: |
            type=ref,event=branch
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=sha,format=short
            type=raw,value=latest,enable=${{ github.ref == 'refs/heads/main' }}

      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

> 提示：
> - 最终镜像地址为 `ghcr.io/你的用户名/litepan_buei`（`你的用户名` 即仓库所属账号，自动取自 `github.repository_owner`）。
> - `platforms` 默认同时构建 `amd64` 和 `arm64`（NAS 常用 arm64）。如果觉得构建太慢，
>   可以只保留 `linux/amd64`。
> - 想改镜像名，改 `IMAGE_NAME` 即可（GHCR 要求全小写）。
> - 打 tag 如 `v0.5.1` 时，会自动额外生成 `0.5.1`、`0.5` 这样的标签。

### 4.3 推送代码触发构建

```bash
cd LitePan_buei
git add .
git commit -m "add docker build workflow"
git push origin main
```

推送后：

1. 打开仓库的 `Actions` 标签页，能看到 `Build and Publish Docker Image` 正在运行。
2. 绿色 ✓ 表示构建并推送成功。
3. 之后每次 `git push origin main` 或 `git push origin v0.5.1`（tag）都会自动触发。

### 4.4 拉取 / 使用自动打包的镜像

```bash
docker pull ghcr.io/你的用户名/litepan_buei:latest
```

把 `docker-compose.yml` 中的镜像改成你自己的：

```yaml
services:
  litepan:
    image: ghcr.io/你的用户名/litepan_buei:latest   # 原来是 ponphil/litepan:beta
    # ... 其余保持不变
```

> GHCR 私有包需要先登录（`$GITHUB_TOKEN` 是变量占位符，需自己创建 GitHub PAT 填入）：
>
> 1. GitHub → Settings → Developer settings → Personal access tokens → Fine-grained tokens → Generate new token
> 2. Repository access 选本仓库，Permissions 里把 Packages 设为 Read（只拉取）
> 3. 复制 token（只显示一次），然后：
>
> ```bash
> export GHCR_TOKEN=粘贴你的PAT
> echo "$GHCR_TOKEN" | docker login ghcr.io -u 你的用户名 --password-stdin
> docker pull ghcr.io/你的用户名/litepan_buei:latest
> ```
>
> 登录一次后凭据缓存到 `~/.docker/config.json`，**过期前无需每次重复登录**（换机器/过期才需重登）。
> 更省事：把包改成 Public 后可免登录直接 pull。

然后：

```bash
docker compose up -d
```

### 4.5（可选）改用 Docker Hub

如果你想推送到 Docker Hub 而不是 GHCR：

1. 在仓库 `Settings → Secrets and variables → Actions` 添加 `DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN`。
2. 把 workflow 里的 `REGISTRY` 改成 `docker.io`，登录步骤换成：

```yaml
      - name: Log in to Docker Hub
        uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}
```

并把 `images` 改为 `${{ secrets.DOCKERHUB_USERNAME }}/${{ env.IMAGE_NAME }}`，其余不变。

---

## 5. 手动生成 Docker 镜像

### 5.1 基础命令（单架构）

```bash
cd LitePan_buei

# 生成镜像，默认 tag 为 litepan-go:dev
docker build -t litepan-go:dev .

# 指定平台与 tag（NAS x86 常用 linux/amd64）
docker build --platform linux/amd64 -t litepan-go:dev .

# 指定其它 tag，例如推送到自己的 GHCR
docker build -t ghcr.io/你的用户名/litepan_buei:beta .
```

构建完成后查看：

```bash
docker images | grep litepan
```

### 5.2 使用 Makefile（项目自带）

```bash
cd LitePan_buei

make docker-build        # 等价 docker build --platform linux/amd64 -t litepan-go:dev .
make docker-save         # 构建并导出 dist/litepan-go:dev.tar.gz，方便拷到 NAS 离线导入
make docker-up           # docker compose up -d --build（本地 compose 部署）
make docker-down         # docker compose down
```

> Makefile 中可覆盖变量：
> ```bash
> make docker-build DOCKER_IMAGE=litepan-go:beta DOCKER_PLATFORM=linux/amd64
> ```

### 5.3 多架构构建并直接推送（buildx）

一次性打 `amd64 + arm64` 并推到 GHCR：

```bash
docker buildx create --use --name multiarch 2>/dev/null || docker buildx use multiarch

docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t ghcr.io/你的用户名/litepan_buei:beta \
  --push .
```

> 推 GHCR 前先登录（PAT 需要 Packages 的 Write 权限）：
> ```bash
> export GHCR_TOKEN=你的PAT
> echo "$GHCR_TOKEN" | docker login ghcr.io -u 你的用户名 --password-stdin
> ```

只构建不推送（导出到本地，多架构需 `--load` 仅单架构）：

```bash
docker buildx build --platform linux/amd64 -t litepan-go:dev --load .
```

### 5.4 导出 tar.gz 离线导入（NAS 场景）

```bash
# 方式一：用 make
make docker-save

# 方式二：手动
docker save litepan-go:dev | gzip > litepan-go-dev.tar.gz
```

到 NAS 上导入：

```bash
gunzip -c litepan-go-dev.tar.gz | docker load
```

### 5.5 用本地打的镜像跑起来

把 `docker-compose.yml` 的 `image` 改为本地镜像名后：

```bash
docker compose up -d
```

或直接用 `docker run`：

```bash
docker run -d \
  --name litepan \
  --restart unless-stopped \
  -p 5211:5211 \
  -p 42069:42069/tcp \
  -p 42069:42069/udp \
  -e TZ=Asia/Shanghai \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/strm:/app/strm \
  -v $(pwd)/mounts:/app/mounts:shared \
  --device /dev/fuse:/dev/fuse \
  --pid host \
  --privileged \
  litepan-go:dev
```

---

## 6. 常见问题

**Q1：`go build` 报 `pattern ./cmd/litepan: no required module provides package` 或找不到依赖？**
先执行 `go mod download`（国内可设置 `GOPROXY=https://goproxy.cn,direct`）。

**Q2：`go build` 报 `//go:embed web` 相关错误 / `internal/api/web` 不存在？**
前端产物目录缺失或为空。先 `cd web && npm ci && npm run build` 再回根目录 `go build`。

**Q3：前端 `npm run dev` 能打开页面，但接口 404？**
确认后端已在 5211 端口启动；如果后端不在本机或端口不同，设置
`LITEPAN_API_PROXY=http://后端IP:5211 npm run dev`。

**Q4：FUSE 挂载不可用 / 报 `fuse: device not found`？**
Docker 方式需要 `--device /dev/fuse:/dev/fuse`、`privileged: true`、`pid: "host"`，且宿主机内核支持 fuse。不用挂载功能可忽略。

**Q5：GitHub Actions 构建 arm64 太慢或超时？**
把 workflow 里 `platforms: linux/amd64,linux/arm64` 改成 `platforms: linux/amd64`。

**Q6：不想每次 push 都触发构建？**
删掉 workflow 里的 `push:` 段，只保留 `workflow_dispatch:`，改成在 Actions 页面手动点 `Run workflow`。
