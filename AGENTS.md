# AGENTS.md

> 本文件面向在本仓库工作的 AI 编码代理与开发者。
> 项目：**LitePan（Go 版）**——多网盘聚合、文件浏览、跨盘传输、STRM、媒体整理、WebDAV/FUSE、Emby/飞牛反代等。
> 上游：<https://github.com/Ponphil/LitePan>；本仓库是 `akira17th/LitePan_buei` fork。

> **TL;DR**：日常在 `buei` 分支开发；做扩展时优先新建文件，必须改上游文件时只加最小接入点，并用 `buei:` / `#region buei:` 标记。详见 1.2。

---

## 1. 仓库、分支与最高优先级规则

### 1.1 分支约定

| 分支 | 用途 |
| --- | --- |
| `main` | 跟随上游 `Ponphil/LitePan`，尽量保持干净，方便同步与对比 |
| `buei` | 个人定制分支，当前默认工作分支；所有个人扩展优先落在这里 |

当前 `origin` 指向 fork：`git@github.com:akira17th/LitePan_buei.git`。
上游更新通常先合到 `main`，再合并进 `buei`；也可以直接添加 `upstream` 远程后合并。

### 1.2 buei 扩展开发规则（必须遵守）

> **在 `buei` 分支做扩展时，尽可能新建文件来实现；不要随意修改原项目文件。**
> 目的：后续上游更新时，合并冲突最小、可维护性最好。

具体做法：

1. **默认新建文件**
   - 后端新功能优先新建独立包：`internal/buei/<feature>/`，或既有包内新文件 `xxx_buei.go`。
   - API 处理函数放新文件：`internal/api/buei_<feature>.go`。
   - 路由注册放新文件：`internal/api/buei_routes.go`，提供 `registerBueiRoutes`。
   - 装配代码放新文件：`internal/app/wire_buei.go`。
   - 新驱动放新目录：`drivers/BueiXxx/`，并通过 `drivers/buei_all.go` 空导入聚合。
   - 新仓储放新文件：`internal/store/buei_<feature>_repo.go`，用 `NewXxxRepo(db)` 构造，避免改 `Store` 结构体。
   - 新迁移只新增 `internal/store/migrations/00NN_buei_*.sql`，不改已执行迁移。
   - 新事件放 `internal/eventbus/buei_events.go`。
   - 前端新功能优先新增组件 / composable / API 文件；例如 `web/src/components/buei/`、`web/src/composables/useBueiXxx.ts`、`web/src/api/bueiXxx.ts`。

2. **确实必须改上游文件时，只做最小改动**
   - 只增加必要的调用点 / 字段 / 一行模板挂载，不重排 import、不整文件格式化、不重命名、不删除上游注释。
   - 用显著标记包住定制代码：
     - Go / TS：`// #region buei:<feature>` … `// #endregion buei:<feature>`
     - Vue 模板：`<!-- #region buei:<feature> -->` … `<!-- #endregion buei:<feature> -->`
   - 逻辑尽量放进新文件，原文件只保留一行调用或一个 prop 传递。
   - 不修改上游测试来“适配”定制；新增 `*_buei_test.go` 或新测试文件。

3. **命名隔离**
   - 自定义数据库表、设置键、API 前缀尽量带 `buei` 前缀，例如 `buei_xxx`、`/api/buei/...`。
   - 自定义 Go 文件/包、Vue 组件也带 `buei` 标识，便于识别哪些是 fork 定制。

4. **不要扩大 diff**
   - 不升级 `go.mod` / `go.sum` / `package-lock.json`，除非任务明确要求。
   - 不引入新依赖，除非确有必要；引入时单独提交并说明原因。
   - 不把“同步上游”“格式化”“重构”混进功能提交。

5. **生成物不手工编辑**
   - `internal/api/web/` 是前端构建产物，由 `cd web && npm run build` 生成。
   - 合并冲突时不要手工合并该目录：先合并 `web/src`，再重新构建生成。

6. **提交粒度**
   - 一个定制功能一个 commit，提交信息用中文并标明 `buei:` 或功能名。
   - 合并上游使用 `git merge`，不要 rebase 已推送的 `buei` 历史，避免 force push。

### 1.3 当前 buei 定制（生成本文件时）

可用 `git diff origin/main buei --stat` 查看最新差异。当前主要定制：

| 定制 | 主要文件 |
| --- | --- |
| iOS 长按表格行弹出右键菜单 | `web/src/components/file/FileTable.vue` |
| 文件菜单“用 IINA / Infuse 打开” | `web/src/components/file/FileBrowser.vue`、`FileTable.vue`、`web/src/composables/useFileActions.ts`、`useFileTableInline.ts` |
| 下拉菜单禁止文本选中 | `web/src/styles/dropdown-menu.css` |
| GHCR 自动构建（仅 amd64，推 `buei` 触发并更新 `latest`） | `.github/workflows/docker-build.yml` |
| 本地一键启动脚本 | `run.sh` |
| 本地运行 / 镜像构建说明 | `readme_run.md` |
| README 定制说明 | `README.md` |
| `.tools/` 忽略 | `.gitignore` |
| 删除一条上游注释（无功能影响） | `drivers/all.go` |

> 注意：`docker-compose.yml` 默认仍使用上游镜像 `ponphil/litepan:beta`；要运行自己的镜像需改为 GHCR 地址。
> 当前 GitHub Actions 在推送 `buei` 分支或 `v*` tag 时自动构建；推 `buei` 会同时更新 `latest` 标签。

### 1.4 合并上游建议流程

```bash
# 方式一：先同步 fork 的 main，再合进 buei
git fetch origin
git switch main
git merge origin/main          # 如果本地 main 落后
git switch buei
git merge main

# 方式二：直接加 upstream
git remote add upstream https://github.com/Ponphil/LitePan.git
git fetch upstream
git switch buei
git merge upstream/main
```

冲突处理原则：

1. **上游原文件优先接受上游版本**，不要把 buei 的大段逻辑硬塞回去。
2. 把 buei 逻辑迁移到新文件、新 composable、新 hook；原文件只保留最小调用点。
3. 迁移编号冲突按 7.5 处理；已执行迁移不能改编号。
4. `internal/api/web/` 不手工合并：合并 `web/src` 后重新 `npm run build`。
5. 合并完成后至少跑：`go test ./...`（或 `make test`）、`npm run type-check`；前端有改动再 `npm run build`。
6. 用 `git diff origin/main buei --stat` 检查定制范围是否意外扩大。

### 1.5 Git 操作规则（必须遵守）

- **不要自动执行 `git add`、`git commit`、`git push`**。修改完成后停在“工作区已修改/未暂存”状态，由用户自行 review、stage、commit、push。
- 不要自动执行 `git stash`、`git reset`、`git checkout`、`git restore`、`git clean`、`git tag`、`git branch -D` 等会改变工作区、暂存区、分支或历史的操作。
- 不要撤销、覆盖或提交用户手动做的修改；如果工作区已有用户改动，先 `git status` 确认，只编辑任务涉及的文件。
- 不要 force push，不要自动创建/删除分支或 tag。

---

## 2. 快速命令

### 2.1 Go 工具链

本机可能没有全局 `go`。仓库根目录的 `.tools/go` 是本地工具链（已被 `.gitignore` 忽略，`run.sh` 也依赖它）：

```bash
cd /path/to/LitePan_buei
export GOROOT="$PWD/.tools/go"
export PATH="$GOROOT/bin:$PATH"
export GOPATH="$PWD/.tools/gopath"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOCACHE="$PWD/.tools/gocache"
export GOTOOLCHAIN=local   # go.mod / Dockerfile 都要求固定 1.26.x
```

如果 `.tools/` 不存在，可安装 Go 1.26.x（`go.mod` 为 `go 1.26.6`）；若要用 `./run.sh`，需要让 `.tools/go` 指向可用的 Go 发行版，或按脚本内容调整路径。

### 2.2 后端

```bash
# 开发运行（需要 internal/api/web 已有内嵌产物）
go run ./cmd/litepan

# 显式指定参数
go run ./cmd/litepan -listen :5211 -data-dir ./data -strm-dir ./strm

# 编译（带 FUSE；生产形态）
make build          # go build -tags fuse ./...
make build-nofuse   # 不带 FUSE

# 单元测试
make test                        # go test -race ./...
go test ./internal/file/... -count=1
go test ./internal/api -run TestSPA -count=1

# lint（需要 golangci-lint v2.12.2）
make lint
make lint-install   # 首次安装 golangci-lint
```

`make` 内部使用 `GOWORK=off`，与 CI/Docker 保持一致。
`make build` 只做全包编译检查；要产出单二进制请用：

```bash
go build -tags fuse -trimpath -ldflags="-s -w" -o litepan ./cmd/litepan
```

二进制 `litepan` 已被 git 忽略。

### 2.3 前端

```bash
cd web
npm ci                 # 首次或 package-lock 变更后
npm run dev            # http://127.0.0.1:5173，/api 代理到 5211
npm run type-check     # vue-tsc，严格 TS 检查
npm run build          # 构建到 internal/api/web，并执行 gzip 压缩
npm run test:upload    # 上传调度相关的 Node 脚本测试
```

开发态可用 `LITEPAN_API_PROXY` 覆盖后端地址：

```bash
LITEPAN_API_PROXY=http://192.168.1.10:5211 npm run dev
```

### 2.4 一键开发（前后端同时启动）

```bash
./run.sh
```

脚本会设置 `.tools/go` 环境、执行 `go mod download`、必要时 `npm ci`，然后同时启动：
- 后端 `http://服务器IP:5211`
- 前端 `http://服务器IP:5173`

### 2.5 Docker / 镜像

```bash
make docker-build     # docker build --platform linux/amd64 -t litepan-go:dev .
make docker-save      # 构建并导出 dist/litepan-go:dev.tar.gz
make docker-up        # docker compose up -d --build
make docker-down

# 多架构 buildx（手动）
docker buildx build --platform linux/amd64,linux/arm64 \
  -t ghcr.io/akira17th/litepan_buei:beta --push .
```

Dockerfile 会在构建阶段执行 `npm run type-check && npm run build`，然后编译 Go。
`BUILD_TAGS` 默认 `fuse`。

---

## 3. 运行时与部署

### 3.1 默认值

| 项 | 默认值 |
| --- | --- |
| HTTP 端口 | `5211` |
| 内置 Magnet/BT 端口 | `42069/tcp`、`42069/udp` |
| 数据目录 | `./data`（SQLite、日志、缓存、备份等） |
| STRM 输出 | `./strm` |
| FUSE 挂载根 | 容器内默认 `/app/mounts`（compose 将宿主机 `./mounts` 映射到此）；可用 `LITEPAN_MOUNT_ROOT` 覆盖 |
| 管理员账号 | `admin` / `admin`（首次登录后必须修改） |

### 3.2 环境变量

| 变量 | 作用 |
| --- | --- |
| `LITEPAN_DATA_DIR` | 数据目录；未显式设置 STRM 时，默认 STRM 为数据目录同级 |
| `LITEPAN_STRM_DIR` | STRM 输出目录 |
| `LITEPAN_DB_PATH` | SQLite 文件路径 |
| `LITEPAN_LISTEN` | HTTP 监听地址，如 `:5211` |
| `LITEPAN_LOG_LEVEL` | `debug` / `info` / `warn` / `error` |
| `LITEPAN_MOUNT_ROOT` | FUSE 挂载根目录 |
| `LITEPAN_CORS_ORIGINS` | 额外允许的跨域来源（逗号/分号分隔） |
| `LITEPAN_SECRET_KEY` | 覆盖 `data/secret.key` 的密钥（备份加密等） |
| `LITEPAN_API_PROXY` | 仅前端 dev server 使用，代理 `/api` 到后端 |

### 3.3 FUSE

- 必须使用 `-tags fuse` 构建，否则接口返回 `NOT_IMPLEMENT`。
- Docker 需要 `/dev/fuse`、`privileged: true`、`pid: "host"`，并按 README 挂载 `mounts`。
- FUSE 读缓存目录默认在 `data/fuse_read_cache`，也可以单独映射到更快的磁盘。

---

## 4. 架构总览

### 4.1 技术栈

- 后端：Go 1.26、`chi` v5、`modernc.org/sqlite`（纯 Go SQLite）、`go-fuse` v2、`anacrolix/torrent`（内置 Magnet）。
- 前端：Vue 3 + TypeScript（strict）+ Vite 8 + Pinia 4 + vue-router 5。
- 内嵌：前端构建产物写入 `internal/api/web/`，由 `//go:embed web` 打进单二进制。
- 存储：SQLite WAL，单写连接 + 多读连接；迁移 SQL 内嵌。
- 部署：单二进制 / Docker；FUSE 需要宿主权限。

### 4.2 启动装配链

```text
cmd/litepan/main.go
  ├─ config.Load()                  // 环境变量 + 默认值
  ├─ logx.New(...)                  // data/log，按天落盘
  ├─ signal.NotifyContext(...)      // SIGINT/SIGTERM
  ├─ app.New(ctx, Options)
  │   ├─ prepareDataDirs()          // data / db / strm
  │   ├─ backuprestore.ApplyPending()
  │   ├─ openStore()                // SQLite + Migrate + settings
  │   ├─ wireCore()                 // eventbus/cache/driver manager/auth/scheduler/driverexec/secret
  │   ├─ wireServices()             // file/playback/strm/upload/mediaorganize/... 
  │   └─ wireHTTPServer()           // notification/apikey/backup/cover/cleanup/router
  ├─ a.Run(ctx)                     // 启动后台任务 + HTTP ListenAndServe
  └─ a.Shutdown(ctx)                // 反向优雅关闭
```

`app/app.go` 是唯一组装根（composition root）。新增常驻服务时，启动放在 `Run`，停止放在 `Shutdown`，并保持“后启动的先关闭”。

### 4.3 目录职责

| 路径 | 职责 |
| --- | --- |
| `cmd/litepan/` | 进程入口、flag、信号、优雅退出 |
| `internal/app/` | 装配根；`wire_*.go` 按 store/core/services/http 分阶段注入 |
| `internal/api/` | HTTP 传输层：路由、DTO、鉴权中间件、SSE、内嵌 SPA；**禁止 import store** |
| `internal/domain/` | 纯领域类型、错误码、仓储接口；**禁止 import 其它 internal 包** |
| `internal/store/` | SQLite 仓储实现、迁移、备份/清理；主要实现 domain 接口 |
| `internal/driver/` | 驱动契约、注册表、实例管理、可选能力接口 |
| `drivers/<name>/` | 具体网盘驱动；只依赖 domain/driver/httpx 等公共层 |
| `internal/core/driverexec/` | 文件域调用驱动的统一中轴：认证闸门 → 网络退避 → 被动刷新 → 驱动实例 |
| `internal/<feature>/` | 业务服务包（file/playback/upload/strm/mediaorganize/...） |
| `internal/share/dav/` | WebDAV 服务实现（`/dav` 外层旁路） |
| `internal/share/fuse/` | FUSE 文件系统实现（`-tags fuse`） |
| `pkg/` | 纯工具包，**禁止 import litepan/internal** |
| `web/` | Vue 前端源码；构建产物输出到 `internal/api/web/` |
| `data/`、`strm/`、`mounts/` | 运行时数据，均被 git 忽略，不要提交 |

### 4.4 依赖方向（golangci-lint depguard 强制）

- `internal/api` 不得 import `internal/store`。
- `internal/file`、`internal/playback`、`internal/upload` 不得 import `internal/api`。
- `drivers/**` 不得 import `internal/file`、`internal/auth`、`internal/upload`。
- `internal/domain` 不得 import 任何 `litepan/internal/*`。
- `internal/**` 不得 import 具体驱动 `litepan/drivers`（公共层只认 `internal/driver` 接口）。
- `pkg/**` 不得 import `litepan/internal`。

违反这些规则会被 `make lint` 拦截。

### 4.5 一次 API 请求怎么走

```text
HTTP
 → chi 全局中间件：trackResponseCommit / RequestID / attachRequestLogger / slowDashboard / Recoverer
 → 路由鉴权：requireAdmin 或 requirePublicOrAdmin
 → handler（internal/api/*.go）
 → 业务服务（internal/<feature>）
 → driverexec.Executor（认证闸门 + 网络退避 + 被动刷新）
 → driver.Manager 获取驱动实例
 → 具体驱动 → 上游网盘 HTTP
 → writeOK / writeErr 统一 JSON 响应
```

- 文件读（列目录/下载）使用 `requirePublicOrAdmin`；写操作基本都是 `requireAdmin`。
- `/dav` 不走 chi：`davBypass` 在 chi 外层直接交给 WebDAV handler，因为 chi 不识别 WebDAV 方法。
- 管理员写请求会校验 `Origin`/`Referer`；前端在非 GET/HEAD 请求上显式设置 `Origin`。跨域场景需配置 `LITEPAN_CORS_ORIGINS`。
- 公开接口：`/api/health`、`/api/auth/*`、`/api/public/*`、STRM 播放路径等。
- 自动化 webhook：`POST /api/open/automation/events`，由 API Key 校验。

### 4.6 响应与错误

统一响应结构（`internal/api/resp.go`）：

```json
{
  "success": true,
  "data": {},
  "message": "",
  "error_type": "VALIDATION",
  "details": {},
  "timestamp": "RFC3339 UTC"
}
```

- 成功用 `writeOK(w, data)`。
- 失败用 `writeErr(w, err)`；错误应为 `domain.AppError`。
- 常用错误码：`AUTH_EXPIRED`(401)、`ADMIN_AUTH_REQUIRED`(401)、`RATE_LIMITED`(429)、`NOT_FOUND`(404)、`DRIVER_ERROR`(502)、`VALIDATION`(400)、`NOT_IMPLEMENT`(501)、`PERMISSION_DENIED`(403)、`INTERNAL`(500)。
- 构造错误：`domain.Errorf(domain.CodeValidation, "...")` / `domain.Wrap(code, err)`。
- `decodeJSON` 使用 `DisallowUnknownFields()`；新增字段时注意前端是否发送了未声明字段。
- DTO 放在 `internal/api`，JSON 字段统一 `snake_case`；不要把 domain 结构直接序列化出去。

### 4.7 持久化与迁移

- SQLite 文件默认 `data/litepan.db`；WAL 模式，`busy_timeout=5000`，`foreign_keys=on`。
- `store.DB` 有写池（1 连接）和读池（4 连接）；写操作必须走 `db.write`，读走 `db.read`。
- **不要在持有写事务时做网络 I/O**，否则会阻塞所有写请求。
- 迁移文件在 `internal/store/migrations/NNNN_name.sql`，按版本号顺序执行，记录在 `schema_migrations`。
- 迁移 SQL 按分号拆分执行，**字符串字面量里不要出现分号**。
- 已执行的迁移绝不能修改或改编号；只能新增后续迁移。
- 当前最新迁移为 `0022_upload_task_batches.sql`；buei 新迁移从 `0023` 起。

### 4.8 全局设置

- 设置键、类型、默认值、分类都在 `internal/settings/registry.go` 的 `defaultSpecs()` 中声明。
- DB 只保存被用户改过的值；读取走内存快照，缺失回落默认值。
- `settings.Update` 只接受已注册键，未知键返回 `VALIDATION`。
- 敏感项 `Sensitive` 在接口输出时打码；`Hidden` 不在普通设置页展示；`SilentLog` 不写“系统设置已更新”日志。
- 新增设置时同步考虑：前端 `SystemSettings` 表单、默认值、校验范围、是否需要热更新。
- buei 定制设置建议使用 `buei_` 前缀，避免与上游键冲突。

### 4.9 事件总线

- `internal/eventbus` 是按类型订阅的异步总线，队列 256。
- 关键事件：`AccountAuthFailed`、`AccountAuthRecovered`、`FileMutated`、`OfflineDownloadCompleted`、`NotificationCreated`。
- 发布者不感知订阅者；订阅应在装配期完成。
- **总线是单协程串行分发**：订阅者不能长时间阻塞，否则会拖慢所有事件；耗时逻辑请自行 `safego` 启协程或转任务队列。
- `Bus.Close` 会排空队列；关闭顺序在 `app.Shutdown` 中保证。

### 4.10 日志

- `logx.Manager` 负责控制台 + 按天落盘（`data/log`），支持级别、模块、保留天数。
- 模块常量在 `internal/logx/module.go`：`system`、`driver`、`api`、`file_op`、`cache`、`auth`、`webdav` 等。
- 装配时通过 `logs.For(logx.ModuleX)` 注入；服务内不要直接 `fmt.Println`。
- 不要记录 token、cookie、直链签名等敏感信息；播放日志只记录目标 host。
- 后台任务异常用 `pkg/safego.Guard` 兜住单轮 panic，避免整个进程退出。

### 4.11 驱动模型

- 最小契约：`driver.Driver` = `Meta` + `Lister`。
- `Meta`：`Config()`、`GetAddition()`、`Init()`、`Drop()`、`Ping()`。
- 可选能力通过接口断言：`Downloader`、`InfoGetter`、`Deleter`、`Mover`、`Copier`、`Renamer`、`FolderCreator`、`LocalUploader`、`FullListLister`、`AccountProfileProvider`、`AuthRefresher`、离线下载相关接口、分享能力、扫码登录等。
- 驱动在 `init()` 中调用 `driver.Register(New)`；`drivers/all.go` 通过空导入触发注册。
- `Addition` 结构体的 struct tag 会反射生成前端表单：
  - `json:"name"`、`label:"..."`、`type:"string|select|number|bool|password|local_dir"`
  - `form:"required"`、`form:"full"`、`form:"pair=xxx"`、`form:"-"`
  - `default:"..."`、`options:"value:Label,..."`、`default_by`、`defaults`
- `driver.Manager` 按账号缓存实例；账号配置 hash 变化才重建；注入认证凭证、OAuth 地址、请求间隔闸门。
- 驱动不得 import `internal/file` / `internal/auth` / `internal/upload`，否则 lint 失败。
- 公共层只能通过 `internal/driver` 接口访问驱动，禁止 import 具体驱动包。

### 4.12 认证与任务调度

- `internal/auth` 维护账号认证状态机：active / cooldown / token_expired / failed。
- `driverexec.Executor` 在每次驱动调用前检查认证闸门；网络错误会触发短退避（3 次后 30s）。
- `auth.Scheduler` 负责主动刷新；`taskauth.Coordinator` 在认证失败时暂停/恢复关联任务。
- 后台任务在启动时可能受 `startupwait` / `startupGate` 约束，等首次认证巡检完成后再跑。

---

## 5. 功能地图

| 功能 | 主要包 / 文件 | 说明 |
| --- | --- | --- |
| 账号与认证 | `internal/account`、`internal/auth`、`internal/adminauth`、`internal/apikey`、`internal/accountprofile` | 账号 CRUD、OAuth/扫码、认证状态机、管理员登录/2FA、API Key |
| 文件浏览 | `internal/file`、`internal/cache`、`internal/favorites`、`internal/api/files*.go` | 目录列表、缓存、收藏、重命名/移动/复制/删除、命名对齐 |
| 播放/下载 | `internal/playback` | 302 直链或 Range 代理、断点/多段、直链缓存、本地文件播放 |
| 上传 | `internal/upload`、`internal/api/upload*.go`、`internal/api/local_upload.go` | 上传任务队列、断点续传、并发、暂停/恢复、SSE 进度 |
| 跨盘传输 | `internal/crosstransfer`、`internal/api/cross_transfer_admin.go` | 跨盘秒传/普传、扫描、探测、执行 |
| 离线下载 | `internal/offlinedownload`、`internal/api/offline_download.go` | 网盘离线任务 + 内置 HTTP/Magnet 下载器 |
| STRM | `internal/strm`、`internal/strmdelete` | 扫描、生成 `.strm`、元数据同步、签名 URL、删除工具 |
| STRM 刮削 | `internal/strmscrape` | TMDB 匹配、nfo/海报/fanart/分集图写入、失败重试 |
| 目录整理 | `internal/mediaorganize`（`planner`/`recognition`/`executor`/`tmdb`/`rules`） | TMDB 识别、分组、计划预览、冲突处理、执行归档 |
| AI / 分类整理 | `internal/aiorganize`、`internal/classifyorganize` | AI 识别、分类规则 |
| 海报提取 | `internal/coverextract` | 视频抽帧、拼图、封面样式 |
| 自动联动 | `internal/automation` | 定时/间隔/webhook/离线完成触发；整理→STRM→刮削→刷库等动作链 |
| WebDAV | `internal/share/dav` | `/dav`，路径缓存、PROPFIND 缓存、读写、挂载 |
| FUSE | `internal/fusemount`、`internal/share/fuse`、`internal/fusereadcache` | 本地挂载、读缓存、写支持 |
| 网盘分享 | `internal/cloudshare`、`internal/driver/share.go` | 通过驱动接口实现网盘分享创建/管理（当前 123、夸克已实现） |
| Emby 反代 | `internal/embyproxy` | Emby 播放信息、WebSocket、媒体库刷新 |
| 飞牛影视反代 | `internal/fnosproxy` | 飞牛播放/管理接口反代、路径映射 |
| 夸克 TV | `internal/quarktv` | 夸克 TV 绑定、播放链接接管 |
| 缓存治理 | `internal/cache`、`internal/cacheretention` | 元数据 LRU、目录缓存、缓存保持任务 |
| 空间清理 | `internal/spacecleanup` | 垃圾扫描、清理报告、FUSE/封面/备份临时文件 |
| 备份恢复 | `internal/backuprestore` | 加密备份、导入导出、pending restore、重启应用 |
| 通知/公告 | `internal/notification`、`internal/announcement` | 站内通知、未读 SSE、公告 |
| 日志 | `internal/logx`、`internal/api/logs.go` | 分级日志、按天落盘、日志页、清理 |
| 设置 | `internal/settings` | 全局设置注册、校验、快照 |

---

## 6. 前端（web/）

### 6.1 入口与路由

- `web/src/main.ts`：创建 Vue app、Pinia、router，加载全局 CSS。
- `web/src/App.vue`：`RouterView` + `ToastHost` + `ConfirmHost`。
- `web/src/router/index.ts` 只有三个顶层页面：
  - `/` → `IndexView.vue`（文件浏览）
  - `/login` → `LoginView.vue`
  - `/admin` → `AdminView.vue`（后台）
- 后台的页面与 tab 通过 query 参数切换（`page`、`tab`），页面组件在 `AdminView.vue` 中用 `defineAsyncComponent` 懒加载。

### 6.2 状态

- Pinia setup store：
  - `stores/auth.ts`：管理员会话、是否必须改密、公开首页开关。
  - `stores/accounts.ts`：账号列表与当前账号。
  - `stores/browser.ts`：当前目录、面包屑、文件列表、收藏夹、加载/刷新状态。
- 组件内部状态优先用 `ref` / `computed`；跨组件共享再进 store。
- 文件浏览的主状态在 `browser` store，避免在多个组件里重复维护列表。

### 6.3 API 层

- `web/src/api/client.ts` 是唯一通用 HTTP 封装：
  - 自动加 `/api` 前缀、`credentials: "include"`、默认 90s 超时。
  - 非 GET/HEAD 自动设置 `Origin`。
  - 统一解包 `{success,data,message}`；失败抛 `ApiError`。
  - 401 + `ADMIN_AUTH_REQUIRED` 自动跳登录。
- 每个业务域一个 `web/src/api/<domain>.ts`，导出 `xxxApi` 对象。
- 流式/预览等特殊请求可以直接 `fetch`，但应复用 `downloadURL` / `proxyPreviewURL` 等已有工具。
- 长耗时请求用 `http.postWithTimeout` 或 `AbortController`。
- 类型定义优先放在 `web/src/api/types.ts` 或对应 API 文件；领域专用类型放 `web/src/types/`。

### 6.4 组件与样式

- 基础组件在 `web/src/components/base/`：`AppButton`、`AppInput`、`AppSelect`、`AppModal`、`AppMenuPanel`、`AppStateBlock`、`BusySpinner` 等，优先复用。
- 文件相关：`web/src/components/file/`（`FileBrowser`、`FileTable`、`FileContextMenu`、预览组件等）。
- 后台相关：`web/src/components/admin/`（面板、卡片、表格、设置行等）。
- 逻辑复用放 `web/src/composables/`；确认框用 `showConfirm` / `confirm`，提示用 `toast`，不要用原生 `alert` / `confirm`。
- 样式：
  - 全局入口在 `main.ts` 加载 `tokens.css`、`base.css`、`buttons.css`、`dropdown-menu.css` 等。
  - 颜色/间距/圆角优先用 CSS 变量（`tokens.css`）。
  - SFC 内使用 `<style scoped>`；跨页面共享才放 `web/src/styles/`。
- 文案统一简体中文；日期/大小格式化复用 `web/src/utils/format.ts`。

### 6.5 构建与内嵌

```text
web/src  --npm run build-->  internal/api/web  --go:embed-->  litepan 二进制
```

- `web/vite.config.ts` 的 `build.outDir` 指向 `../internal/api/web`，并 `emptyOutDir`。
- `web/scripts/compress-build.mjs` 会把可压缩资源替换为 `.gz` 并删除原文件；`internal/api/router.go` 的 `spaHandler` 负责按 `Accept-Encoding` 返回 gzip 或解压。
- `internal/api/web/` 是 Git 跟踪的生成物；改了前端并希望 `go build` 直接可用时，需要重新构建并提交。
- 合并上游时不要手工合并 `internal/api/web/`：以合并后的 `web/src` 重新 `npm run build` 为准。

---

## 7. 扩展开发菜谱

### 7.1 新增一个后端功能服务

推荐结构：

```text
internal/buei/myfeature/
  service.go        # Options + New + 业务方法
  service_test.go   # 单元测试
internal/app/wire_buei.go   # 构造并注入
```

- 依赖通过 `Options` 注入接口或已有服务，不要用包级全局变量。
- 错误统一用 `domain.Errorf` / `domain.Wrap`。
- 长任务接收 `context.Context`，循环内检查 `ctx.Err()`。
- 后台循环用 `safego.Guard(log, "buei.myfeature", func(){...})` 包住单轮逻辑。
- 启动/停止如果必须挂到 `app.Run` / `app.Shutdown`，只加最小调用，逻辑放 `internal/app/buei_runtime.go`。

### 7.2 新增 API

1. 新建 `internal/api/buei_<feature>.go`，在 `package api` 中给 `*Handler` 加方法。
2. 新建 `internal/api/buei_routes.go`：

```go
func (h *Handler) registerBueiRoutes(r chi.Router) {
    r.Route("/buei/myfeature", func(r chi.Router) {
        r.Use(h.requireAdmin) // 或 requirePublicOrAdmin
        r.Get("/", h.bueiList)
        r.Post("/", h.bueiCreate)
    })
}
```

3. 在 `internal/api/router.go` 的 `/api` 路由组内加**一行**调用：

```go
h.registerBueiRoutes(r) // buei: extension routes
```

4. 如果新 handler 需要新服务：
   - 优先在 `internal/api/buei_deps.go` 里放一个包级注册函数（由 `wire_buei.go` 初始化），避免改 `Deps` / `Handler` 结构体；或
   - 只在 `Deps` / `Handler` 各加一个 `buei` 字段，并把逻辑全部留在新文件。

约定：

- JSON 字段用 `snake_case`。
- 请求体用 `decodeJSON`，路径参数用 `pathID` / `parsePathInt64`，查询参数用 `parseQueryInt64`。
- 成功 `writeOK`，失败 `writeErr`；不要直接写裸 JSON。
- 返回 DTO，不直接返回 domain 结构。
- 需要管理员权限就加 `requireAdmin`；公开读接口再考虑 `requirePublicOrAdmin`。

### 7.3 新增一个网盘驱动

```text
drivers/BueiXxx/
  driver.go   # 实现 driver.Driver；init() 中 driver.Register(New)
  config.go   # Addition 配置（如果需要）
  ops.go      # 其它可选能力
drivers/buei_all.go
```

`drivers/buei_all.go`：

```go
package drivers

import _ "litepan/drivers/BueiXxx"
```

因为 `cmd/litepan/main.go` 已经空导入 `litepan/drivers`，新增这个文件即可注册，不必改 `drivers/all.go`。

驱动实现要点：

- `Config()` 返回 `driver.Config`：`Name` 唯一、`DisplayName`、`AuthType`、`CardLogo` 等。
- `GetAddition()` 返回配置结构指针；字段 tag 决定前端表单。
- `Init` 中校验配置；`Ping` 做轻量连通性检查；`Drop` 释放资源。
- 可选能力通过接口实现，不要塞进 `Driver` 最小接口。
- 不要 import `internal/file` / `internal/auth` / `internal/upload`。
- 大文件上传默认 HTTP/1.1；确需 HTTP/2 时在 `Config` 设置 `UploadUseHTTP2`。
- 新驱动 logo 放 `web/public/logos/`，并在 `Config.CardLogo` 指向 `/logos/xxx.png`。

### 7.4 新增数据库表 / 仓储

推荐不修改 `internal/store/store.go`：

```text
internal/domain/buei_myfeature.go       # 实体 + Repository 接口
internal/store/buei_myfeature_repo.go   # type myFeatureRepo struct{ db *DB }
                                        # func NewMyFeatureRepo(db *DB) *myFeatureRepo
internal/store/migrations/0023_buei_myfeature.sql
```

- 在 `internal/app/wire_buei.go` 里用 `store.NewMyFeatureRepo(st.db)` 构造，传给服务。
- 仓储方法接收 `context.Context`，读用 `db.read`，写用 `db.write`。
- 用 `wrapDB(err)` 归一化数据库错误；`sql.ErrNoRows` 会变成 `NOT_FOUND`。
- 时间字段用现有 `tsValue` / `parseTS` 约定（UTC 文本）。
- 表名、索引名带 `buei_` 前缀，降低与上游迁移冲突概率。

### 7.5 新增迁移

1. 从当前最大编号 +1 开始，例如 `0023_buei_myfeature.sql`。
2. 文件名格式必须是 `NNNN_name.sql`（迁移加载器按第一个 `_` 解析版本号）。
3. 迁移内容会被按 `;` 拆分逐条执行；不要在字符串字面量里写分号。
4. 已发布/已执行的迁移不能改内容或改编号；需要变更就新增下一号迁移。
5. 如果上游后来也占用了同一编号：
   - 本地还没发布：把 buei 迁移改成下一个未占用编号；
   - 已经发布/用户库执行过：不要改旧编号，新增一个更高编号的迁移做增量变更。

### 7.6 新增全局设置

如果必须出现在系统设置页：

1. 在 `internal/settings/registry.go` 增加 `Key...` 常量和 `Spec`（只加必要行，并用 `// buei:` 标记）。
2. 也可以把 Spec 放 `internal/settings/buei_specs.go`，并把 `defaultSpecs()` 的 `return []Spec{...}` 改成 `return append([]Spec{...}, bueiSpecs()...)`（只改开头和结尾两处，逻辑仍在新增文件里）。
3. 前端在 `SystemSettings` 对应分类里渲染；值类型、范围、默认值与后端保持一致。
4. 需要热更新的设置，在 `internal/app` 的 settings hook 或对应服务里处理。

如果只是 buei 内部配置、不需要出现在设置页：
- 直接用 `domain.ConfigRepository` 存 `buei_...` 键，新增自己的读写 API；不要强行塞进 `settings.Service`。

### 7.7 新增前端功能

推荐：

```text
web/src/api/bueiXxx.ts
web/src/components/buei/BueiXxxPanel.vue
web/src/composables/useBueiXxx.ts
```

- 新页面/新后台 tab 如果必须挂到 `AdminView.vue`：
  - 在 `adminPageLoaders`、`nav`、`PAGE_TABS` 增加最小条目，并用 `// #region buei:` 标记。
- 文件浏览器扩展如果必须挂到 `FileBrowser.vue` / `FileTable.vue`：
  - 新组件 + 一个 prop / 一个事件 / 一行模板挂载。
  - 逻辑放新 composable，避免把大段逻辑写进上游组件。
- 样式优先复用 `web/src/styles/tokens.css` 的变量；新增全局样式才放 `web/src/styles/`。
- 不要直接改 `internal/api/web/`；改完 `web/src` 后重新 `npm run build`。

### 7.8 新增事件 / 通知 / 后台任务

- 事件：在 `internal/eventbus/buei_events.go` 定义事件结构；状态变更后 `bus.Publish(ctx, evt)`；装配期 `eventbus.Subscribe(bus, handler)`。
- 通知：调用 `notification.Service.Notify(ctx, level, category, title, message, accountID, refID)`；自定义 category 用常量放新文件即可。
- 后台任务：
  - 接收 `context.Context`，循环内 `select ctx.Done()`。
  - 每轮用 `safego.Guard`。
  - 需要认证就等 `startupGate` 或使用 `driverexec.Executor`。
  - 在 `app.Run` 启动、`app.Shutdown` 停止（最小接入点）。

---

## 8. 编码约定

### 8.1 Go

- 使用 `gofmt`；提交前跑 `make lint`。
- 注释、错误信息、日志文案使用简体中文，与现有代码一致。
- `context.Context` 作为第一个参数；不要存进结构体长期持有（后台任务可保存 `appCtx`，但要明确生命周期）。
- 错误处理：
  - 业务错误用 `domain.Errorf` / `domain.Wrap`。
  - 不要 `panic` 处理可预期错误；后台任务用 `safego.Guard`。
  - 网络错误判断用 `domain.IsNetworkError`；认证失效用 `domain.IsAuthExpiredError`。
- 并发：
  - 共享 map 必须加锁；注意 `sync.Mutex` 不要跨网络调用持有。
  - 同一资源的重复加载优先用 `pkg/singleflight` 或缓存层的 `Coalesce`。
  - 后台 goroutine 必须可退出，避免泄漏。
- 依赖：
  - 公共层只依赖接口；具体实现通过 `Options` 注入。
  - 不要新增循环依赖；遵守 4.4 的 depguard 规则。
- 文件命名：
  - 测试 `*_test.go`。
  - buei 定制文件尽量带 `buei` 标识，便于合并时识别。

### 8.2 SQL

- 全部使用参数化查询，禁止字符串拼 SQL 值。
- 写操作走 `db.write`，读操作走 `db.read`；不要混用。
- 时间统一用 `tsValue` / `parseTS`（UTC 文本）。
- 查询列表时始终 `defer rows.Close()` 并检查 `rows.Err()`。
- 新表/索引带 `buei_` 前缀；迁移只新增，不改旧。

### 8.3 TypeScript / Vue

- `<script setup lang="ts">`；类型尽量显式，避免 `any`。
- 组件 props / emits 用类型声明；复杂对象定义 interface。
- 逻辑复用抽 composable；不要在上游组件里堆大段定制逻辑。
- 使用 `@/` 别名引用 `web/src`。
- 网络请求统一走 `web/src/api/*` 和 `http` 封装；错误用 `getApiErrorMessage` 转文案。
- 确认/提示使用 `showConfirm` / `toast`；不要用浏览器原生弹窗。
- 样式优先 CSS 变量与 scoped style；不要硬编码主题色。
- 文案中文；变量名英文。

### 8.4 提交前自检

- [ ] 改动是否可以用新文件完成？是否已经尽量少改上游文件？
- [ ] 必须改的上游文件是否只有最小调用点，并有 `buei:` 标记？
- [ ] 新迁移是否只新增、编号未冲突、未修改已执行迁移？
- [ ] 新设置键 / 表名 / API 是否带 `buei` 前缀？
- [ ] 是否误改了 `internal/api/web/` 生成物或运行时 `data/`、`strm/`、`mounts/`？
- [ ] 是否新增了不必要的依赖或升级了 lock 文件？

---

## 9. 测试与质量门禁

### 9.1 后端

```bash
# 单包 / 单测试
go test ./internal/file/... -count=1
go test ./internal/api -run TestSPAHandlerCompressedAsset -count=1

# 全量（含 race）
make test
```

测试约定：

- 表驱动测试优先，失败信息写清楚输入/期望。
- 需要数据库的用 `store.Open(ctx, store.Options{Memory: true})` + `db.Migrate(ctx)`，参考 `internal/store/store_test.go`。
- 需要 HTTP 的用 `httptest.NewServer` / `httptest.NewRecorder`。
- 服务测试用 `t.TempDir()` 隔离文件。
- 并发测试可加 `t.Parallel()`，但注意共享全局状态（驱动注册表等）不要互相污染。
- 不依赖真实外网；用 `httptest` 模拟上游。
- FUSE 相关测试可能带 `//go:build fuse`，普通 `make test` 不会编译；需要时 `go test -tags fuse ./internal/share/fuse/...`。

### 9.2 前端

```bash
cd web
npm run type-check
npm run test:upload
npm run build
```

- 没有 ESLint/Prettier 配置；`vue-tsc` 的类型检查是主要门禁。
- `npm run build` 会执行 Vite 构建 + gzip 压缩；Dockerfile 也会跑 `type-check`。
- 没有浏览器端单测框架；交互改动需要手动验证关键路径。

### 9.3 lint

```bash
make lint
```

- `golangci-lint` v2.12.2；配置在 `.golangci.yml`。
- 重点：depguard 的分层依赖规则、errcheck、govet、staticcheck。
- `web/`、`.bak/`、`node_modules/` 等已被排除。

---

## 10. 常见坑

1. **`go: command not found`**
   使用 `.tools/go/bin/go`，或先按 2.1 导出环境。`run.sh` 已处理。

2. **`//go:embed web` 失败 / 前端 404**
   `internal/api/web` 缺失或为空。先 `cd web && npm run build`。
   仓库虽然跟踪了生成物，但合并/清理后可能为空。

3. **`npm run build` 后 `internal/api/web/` 里的 JS/CSS 消失**
   `compress-build.mjs` 会把构建输出中的 `.js/.css/.svg/.wasm` 等替换为 `.gz` 并删除原文件，这是预期行为；`spaHandler` 会处理。`web/src` 源码不受影响。

4. **FUSE 不可用**
   构建没加 `-tags fuse`，或容器缺少 `/dev/fuse` / `privileged` / `pid: host`。未编译时接口返回 `NOT_IMPLEMENT`。

5. **Docker Compose 跑的仍是上游镜像**
   `docker-compose.yml` 默认 `ponphil/litepan:beta`；要跑自己的定制镜像，改 `image:` 为 `ghcr.io/akira17th/litepan_buei:...` 或本地 tag。

6. **推 buei 没有自动构建镜像**
   检查 `.github/workflows/docker-build.yml` 是否在 `buei` 分支、`on.push.branches` 是否包含 `buei`；也可在 Actions 手动 `Run workflow` 选 `buei`。

7. **管理员写接口 403 “请求来源不受信任”**
   浏览器跨域/反代场景需要正确的 `Origin`/`Referer`，或把来源加入 `LITEPAN_CORS_ORIGINS`。前端 `http` 封装会自动加 `Origin`。

8. **默认密码**
   `admin/admin` 首次登录后必须修改；忘记密码可用登录页重置，临时密码打印在容器控制台日志中。

9. **数据目录与 STRM 目录**
   设置 `LITEPAN_DATA_DIR` 时，STRM 默认变成数据目录同级；要固定位置请显式设置 `LITEPAN_STRM_DIR`。

10. **SQLite 写阻塞**
    写池只有 1 个连接；不要在写事务/写锁内做网络请求或长时间计算。

11. **事件总线被拖慢**
    订阅者在总线单协程里串行执行；耗时操作请异步化。

12. **迁移编号冲突**
    上游新增迁移后，buei 的迁移编号可能撞车。未发布的本地迁移改编号；已发布的只能新增后续迁移。

13. **`internal/api/web` 冲突**
    不要手工合并 hashed 资源文件；合并 `web/src` 后重新构建。

14. **驱动注册**
    新增驱动若忘了空导入，`driver.List()` 不会出现。检查 `drivers/buei_all.go`。

15. **`drivers/all.go` 与本地驱动**
    该文件是上游聚合入口；buei 新驱动请用 `drivers/buei_all.go`，不要直接改它。

16. **版本号**
    发布时需要同步修改 `internal/buildinfo/version.go` 和 `web/src/version.ts`。

---

## 11. 关键文件索引

| 文件 | 用途 |
| --- | --- |
| `cmd/litepan/main.go` | 进程入口、flag、信号、优雅退出 |
| `internal/app/app.go` | 应用生命周期、后台任务启动/关闭 |
| `internal/app/wire_store.go` | SQLite、迁移、settings 初始化 |
| `internal/app/wire_core.go` | eventbus/cache/driver/auth/scheduler/driverexec |
| `internal/app/wire_services.go` | 业务服务装配 |
| `internal/app/wire_http.go` | HTTP server、API Deps 装配 |
| `internal/api/router.go` | 全部路由、内嵌 SPA、`/dav` 旁路 |
| `internal/api/resp.go` | 统一响应与错误输出 |
| `internal/api/request.go` | JSON 解码、路径/查询参数解析 |
| `internal/config/config.go` | 环境变量与默认配置 |
| `internal/settings/registry.go` | 全局设置键、类型、默认值、分类 |
| `internal/store/db.go` | SQLite 读写双池 |
| `internal/store/migrate.go` | 迁移加载与执行 |
| `internal/store/store.go` | 仓储聚合 |
| `internal/domain/errors.go` | 错误码与 `AppError` |
| `internal/driver/driver.go` | 驱动最小接口与可选能力 |
| `internal/driver/registry.go` | 驱动注册、表单 schema 反射 |
| `internal/driver/manager.go` | 驱动实例缓存、认证注入 |
| `internal/core/driverexec/exec.go` | 驱动调用中轴、网络退避 |
| `internal/file/service.go` | 文件域核心服务、缓存失效、事件发布 |
| `internal/playback/service.go` | 播放/下载直链解析与流式代理 |
| `internal/upload/manager.go` | 上传任务队列与断点续传 |
| `internal/strm/service.go` | STRM 任务调度与服务入口 |
| `internal/mediaorganize/service.go` | 目录整理服务入口 |
| `internal/eventbus/bus.go` | 异步事件总线 |
| `internal/logx/module.go` | 日志模块定义 |
| `pkg/safego/safego.go` | 后台任务 panic 兜底 |
| `web/src/main.ts` | 前端入口 |
| `web/src/router/index.ts` | 前端顶层路由 |
| `web/src/api/client.ts` | 前端 HTTP 封装 |
| `web/src/stores/browser.ts` | 文件浏览状态 |
| `web/vite.config.ts` | 前端构建/代理/输出目录 |
| `.github/workflows/docker-build.yml` | GHCR 自动构建 |
| `Dockerfile` | 前端构建 + Go 构建 + 运行时 |
| `Makefile` | 构建/测试/lint/Docker 快捷命令 |
| `readme_run.md` | 本地运行、GHCR、手动镜像详细说明 |
| `run.sh` | 前后端一键开发启动 |

---

> 如果架构、构建方式或 buei 定制规则发生变化，请同步更新本文件。
> 新增 buei 定制时，优先把实现放进新文件，并在提交信息中说明定制点，方便未来合并上游。
