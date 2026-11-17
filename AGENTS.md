# AGENTS.md

> 本文件是接手改动时的门禁：语言/提交/分支纪律与性能红线。
> 架构、端点、部署的正文以 [`README.md`](README.md) 为单一事实源，本文件不复述。

## 项目约定（必读）

- **语言**：文档、代码注释、git 提交信息一律使用**中文**。
- **git 提交前缀**：英文约定——`feat:` / `fix:` / `refactor:` / `docs:` / `chore:` / `test:` / `perf:` / `ci:`，前缀后接中文描述（如 `feat: 新增镜像通道熔断`）。
- **提交纪律**（站主 2026-11-18 定）：
  - **提交数尽可能少，一个任务的改动合在一个提交里**；
  - **迭代中间态不进仓库**：改来改去的过程留在工作区，收敛后以最终形态提交；
  - **提交信息只讲改动**：写"改了什么、为什么"，讲人话，让半年后翻 log 的人不查
    上下文就能看懂；禁止轮次/批次/任务等临时编号与背景故事。
- **及时更新文档**：改代码必须同步更新文档——
  - 改端点/配置/部署方式 → [`README.md`](README.md)；
  - 加/改环境变量 → [`.env.example`](.env.example) 与 README 配置表；
  - 改贡献门禁 → 本文件。
  禁止"代码改了、文档还停留在过去"。
- **分支纪律**：日常开发一律落在 `dev`，提交/推送前先确认当前分支
  （`git rev-parse --abbrev-ref HEAD`）；`main` 只用于发版（`v*` tag），在 `main`
  上完成发版操作后必须切回 `dev`。任何时刻 `main` 都必须可构建可发布。
  不对远端 `main` 强制推送；确需改写远端历史时交给站主自己执行 `--force-with-lease`。

## 性能红线（接手改动必须对照）

生产 VPS 为 2 核 1GB 的超售机型，经 Cloudflare 代理（默认只缓存 js/css 等扩展名，
HTML 与 API 每次回源）。以下约束是实现里已经落地的取舍，改动时不得退化：

- **响应压缩不许绕过**：动态响应经 `internal/server/gzip.go` 的 gzip 中间件
  （挂中间件链最内层），静态产物走"压缩一次缓存复用"（`serveGzippedAsset`，产物
  不可变、缓存进程内生效）。新增端点自动继承；若绕开 `withMiddleware` 自起路由，
  必须自行接上压缩。基准数据：471KB 主包 L6 压缩 8.5ms（E5 超售机约 25-50ms），
  所以静态必须缓存、动态按需压、禁用 L9。
- **热点只读查询必须有 TTL 缓存**：搜索总数（`store.Search` 的 count 缓存 30s）、
  `/api/meta`（30s）、`/api/meta-tags`（10 分钟、多键）、`/api/admin/overview`（3s，
  抓取/设置等动作后 `invalidateOverview()`）。新增同类查询照此办理，并写明失效时机。
- **管理面校验禁止每请求写库**：会话校验走 `store.SessionValid` 的进程内缓存
  （1 分钟 TTL，`last_seen` 至多 10 分钟落一次库）；登出/改密必须清缓存
  （`DeleteSession` / `DeleteAllSessions` 已处理，新路径不得绕过）。
- **前端轮询两个铁律**：轮询端点服务端必须有 TTL 缓存；前端 `document.hidden`
  时必须跳过请求（见 `AdminPage.tsx`）。
- **禁 N+1**：批量组装一次 `IN` / `ANY($1)` 查询（现有 `Search`、`UpsertSubjects` 即如此）。
- **内存有界**：1GB 机器上一切进程内缓存/环形缓冲必须带上限（现状：
  metaTags 64 / countCache 256 / sessCache 512 / metrics recent 256 / 每路由样本 256）。
- **新增环境变量**：必须进 `.env.example` 并在 README 配置表说明，含安全前提。

## 新端点/新路由 Checklist

1. 用 `Handler()` 里的 `reg()` 注册（自动接入性能统计），不要裸 `mux.HandleFunc`；
2. 响应走 `writeJSON`/`writeJSONBytes`（统一壳 + no-store + gzip 继承）；
3. 热点查询配 TTL 缓存并写明失效时机；
4. 管理端新端点套 `requireAdmin`；
5. 若涉及真实 IP（限流/审计键），只经 `s.clientIP()` 取得（可信网段校验在内），
   禁止在别处直接读 XFF/CF 头。

## 经验文档

踩坑教训与通用工程约定（迁移自 Morikusa）见 [`docs/EXPERIENCE.md`](docs/EXPERIENCE.md)：
缓存容器契约、迁移只增不改、真实 IP 可信网段、修 bug 先写复现测试、删符号先查零引用等。
接手改动前过一遍。

## 快速命令

```sh
go build ./... && go test ./...          # 构建与单测
bash scripts/smoke.sh                    # 对运行中服务做 API 冒烟（需先启动 ./anipick 且索引非空）
cd frontend && npm run build             # 前端构建（含 tsc -b），产物拷入 internal/server/dist 再编 Go
```

前端产物是 Go embed 的：改前端后必须把 `frontend/dist` 拷到 `internal/server/dist`
（CI/Dockerfile 会自动做；本地验证需手动拷贝）再 `go build`，否则跑的是旧页面。
