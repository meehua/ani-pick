# AniPick

**本站自建 Bangumi 条目索引** + **浏览器直连 Bangumi 详情**的作品检索站点。
Go 后端（单二进制，内嵌前端）+ PostgreSQL；前端 React 19 + shadcn/ui + Tailwind CSS v4。

## 术语与边界（请先读）

| 概念 | 位置 | 说明 |
|---|---|---|
| **本站索引** | 服务器 PostgreSQL | 由站点维护者用站长账号抓取并持续更新；对**所有访客**提供检索，不是“私有本地数据” |
| **用户本机** | 访客浏览器 localStorage | 只保存访客自己的 Bangumi 访问令牌；不上传、不入库 |
| **上游详情** | `api.bgm.tv` | 条目简介、角色、制作人员、剧集、关联作品由**访客浏览器直接请求**，本站不代理、不中转 |

本项目的硬性边界：

- **不提供任何 API 中继 / 反向代理 / 图片代理**。服务端出站请求只用于自己抓取索引与管理员校验抓取凭据。
- 服务端**没有接收用户凭据的接口**；用户令牌只存在各自浏览器。
- 本站是公开站点：索引数据对所有访客可见；管理后台独立鉴权。

## 功能

- 本站索引内检索：类型多选、标签 AND/OR、排除标签、**标签占比门槛**、分类标签、日期、排名、评分、评分人数、敏感内容模式。
- 排序：全站排名、评分、评分人数、日期、想看/看过/在看、ID；**网格 / 列表 / 表格三视图**，年代快捷筛选（近一年 / 各年代）映射到日期区间。
- 已选筛选条件以 chips 展示、可单独移除；窄屏筛选走底部抽屉，工具条吸顶，按 `/` 快速聚焦搜索。
- 条目页：本站索引的标签与占比、排名、评分分布；简介/角色/制作/关联/剧集由浏览器直连 Bangumi。
- 管理后台：抓取概览、全量/增量/过期重爬、任务历史、索引管理、站长凭据、抓取出口诊断；tab 与数据页搜索状态写进 URL。
- 返回不丢状态：浏览页结果与滚动位置按会话缓存，从条目页返回即时还原；前进导航自动回到页顶。
- 用户登录：个人 Access Token，本机保存；登录框可切换**官方 / 镜像通道**并添加自定义镜像（令牌只发官方）。
- 深浅色跟随系统、响应式布局（窄屏触控目标 ≥44px）、URL 即状态。

## 快速开始

需要 Go ≥ 1.27、Node.js ≥ 20、PostgreSQL ≥ 13。

```bash
createdb -U postgres ani_pick          # 默认库名/用户/密码均为 ani_pick
cp .env.example .env                   # 修改数据库、管理员密码、ANIPICK_SECRET
bash scripts/build.sh
./anipick
```

- 站点：http://localhost:8787
- 管理后台：http://localhost:8787/admin（初始密码来自 `ANIPICK_ADMIN_PASSWORD`）
- 开发：`bash scripts/dev.sh`（后端 :8787 + Vite :5173）

首次部署后请立即修改管理员密码，并在反向代理 / 防火墙层限制 `/admin` 与 `/api/admin/*` 的访问来源。

## Docker Compose 部署

仓库自带 [`docker-compose.yml`](docker-compose.yml)：一个 AniPick 容器 + 一个 PostgreSQL 17 容器，数据库数据保存在 `pgdata` volume。

### 最简方式（PostgreSQL 自备）

只跑应用容器，数据库用你自己的：

```yaml
services:
  anipick:
    image: ghcr.io/meehua/ani-pick:latest
    restart: unless-stopped
    ports:
      - "8787:8787"
    environment:
      ANIPICK_DATABASE_URL: "postgres://USER:PASS@HOST:5432/DB?sslmode=disable"
      ANIPICK_ADMIN_PASSWORD: "change-me"
      ANIPICK_SECRET: "change-me"
    # 数据库在宿主机时取消这两行注释
    # extra_hosts:
    #   - "host.docker.internal:host-gateway"
```

保存为 `compose.simple.yml` 后：

```bash
docker compose -f compose.simple.yml up -d
```

也可以直接用命令：

```bash
docker run -d --name anipick --restart unless-stopped -p 8787:8787 \
  --add-host=host.docker.internal:host-gateway \
  -e ANIPICK_DATABASE_URL='postgres://USER:PASS@host.docker.internal:5432/DB?sslmode=disable' \
  -e ANIPICK_ADMIN_PASSWORD='change-me' \
  -e ANIPICK_SECRET='change-me' \
  ghcr.io/meehua/ani-pick:latest
```

数据库和用户提前建好，容器启动时自动建表；访问 `/admin` 初始化抓取即可。


### 1. 准备配置

```bash
cp .env.example .env
```

至少修改：

```dotenv
ANIPICK_ADMIN_PASSWORD=改成强密码
ANIPICK_SECRET=改成长随机字符串
POSTGRES_PASSWORD=改成数据库密码
```

可选：`ANIPICK_BGM_TOKEN`（站长抓取凭据）、`ANIPICK_EGRESS_PROXY`（仅服务端抓取代理）、`ANIPICK_PORT`（默认 8787）。

Compose 会读取 `.env` 做变量替换，但会**覆盖** `ANIPICK_DATABASE_URL` 为 `postgres://ani_pick:...@postgres:5432/ani_pick`；`.env` 里的 `127.0.0.1` 只用于裸机运行，容器内不要使用。

### 2. 启动

```bash
docker compose up -d
docker compose logs -f anipick
```

首次启动会自动建表并写入管理员密码。访问：

- 站点：http://localhost:8787
- 后台：http://localhost:8787/admin

然后在后台「抓取」里执行一次全量抓取，或先在「设置」里填站长凭据。

### 3. 更新

```bash
docker compose pull
docker compose up -d
```

指定版本：

```bash
ANIPICK_IMAGE_TAG=1.0.0 docker compose up -d
```

镜像来自 GHCR；首次发布后如果 package 是私有的，需要在 GitHub Package 设置里改为 public。

### 4. 备份与恢复

```bash
# 备份
docker compose exec -T postgres pg_dump -U ani_pick ani_pick > anipick.sql

# 恢复（会覆盖现有数据，先停应用）
docker compose stop anipick
cat anipick.sql | docker compose exec -T postgres psql -U ani_pick -d ani_pick
docker compose start anipick
```

### 5. 本地源码构建镜像

如果不想用 GHCR 镜像，在 `docker-compose.yml` 里注释 `image:`，取消 `build: .` 注释：

```bash
docker compose build
docker compose up -d
```

### Compose 相关变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `POSTGRES_PASSWORD` | `ani_pick` | 数据库密码；不要使用 URL 特殊字符 |
| `ANIPICK_PORT` | `8787` | 宿主机端口 |
| `ANIPICK_IMAGE` | `ghcr.io/meehua/ani-pick` | 镜像地址 |
| `ANIPICK_IMAGE_TAG` | `latest` | 镜像标签 |
| `ANIPICK_EGRESS_PROXY` | — | 仅服务端抓取代理，例如 `http://host.docker.internal:7890` |

> 服务端抓取需要代理时，`extra_hosts` 已配置 `host.docker.internal:host-gateway`；访客请求不会经过服务端，也不受这个代理影响。

## 工作原理

```
站长 Bangumi 账号 ──► 抓取器（DoH + ECH 出站）──► PostgreSQL（本站索引）
                                                      │
访客浏览器 ──► /api/subjects（筛选/排序/分页）─────────┘
     │
     └──► https://api.bgm.tv（详情；凭据只在本机 localStorage）
```

## 抓取与索引维护

| 模式 | 行为 | 适用 |
|---|---|---|
| 全量 `full` | 逐类型按日期倒序枚举；成功且无跳页时清理本轮未覆盖的旧条目 | 首次建库、重建 |
| 增量 `incremental` | 各类型最新 N 页 + 一批过期条目重爬 | 日常维护、自动调度 |
| 过期重爬 `refresh` | `fetched_at` 超过 TTL 的条目，按排名优先 | 保持排名/标签占比不过期 |

- 全量清理只在**本轮无跳页**时执行，避免半途失败删空索引。
- 上游 404/403 标记 `locked` 并退避；连续失败 5 次后不再自动重试。
- 自动调度每 2 分钟检查一次；抓取间隔、页大小、超时、UA 均可配置。
- 重启会中断当前任务（记为 `interrupted`），重新启动即可，写入幂等。
- 量级参考：动画约 3 万、书籍约 40 万；全量耗时与所选类型成正比。

写入的字段（只存筛选所需）：

- `subjects`：`id`（Bangumi 主键）、类型、名称、日期、平台、**全站排名**、评分、评分人数、收藏计数、敏感标记、分类标签、封面 URL、标签总票数、抓取时间、锁定/失败计数。
- `subject_tags`：`(subject_id, tag_id, votes, pct)`；`pct` = 该标签票数 ÷ 该条目全部标签票数。
- `tag_usage`：按类型的标签热度，供筛选候选。

## 用户登录与详情

**只支持个人 Access Token。** 在 Bangumi 个人令牌页生成后粘贴，前端调用 `/v0/me` 校验通过才保存到 `localStorage`。

为什么没有 Cookie / OAuth：

- 浏览器跨域规则不允许网页设置 `Cookie` 请求头，Cookie 登录无法直连 `api.bgm.tv`。
- OAuth 的授权码兑换必须由持有 `client_secret` 的一方完成；放在浏览器会泄露密钥，放在服务端又违反“用户请求不经过服务端”的边界。
- 因此在不引入任何服务端中转的前提下，Access Token 是唯一可行的浏览器直连方式。若后续允许服务端做一次性 code 兑换，可再评估 OAuth。

凭据行为：

- 只写入 `localStorage`（键 `anipick.credential.v1`）；退出/清除浏览器数据即删除。
- 详情、角色、剧集等请求由浏览器直接发往官方或镜像；**镜像请求不携带令牌**。
- 本站服务端不接收、不转发、不记录用户令牌；镜像也无法拿到令牌。
- 公共电脑请勿保存。

## 数据通道：官方 + 镜像站

官方 API 目前只有 **`https://api.bgm.tv`** 提供 `/v0`；`api.bangumi.tv`、`api.chii.in`、`bgm.tv`、`bangumi.tv`、`next.bgm.tv` 均不提供 JSON API。**不要凭猜测把令牌发到这些域名。**

前端内置与本地配置：

- 官方：`https://api.bgm.tv`（硬编码，不可被用户改写）
- 内置社区镜像：`https://bgmapi.anibt.net`（API）+ `https://bgmimg.anibt.net`（图片）
- 用户可在登录框添加/删除镜像，只写入本机 `localStorage`；只接受 HTTPS 地址。

选择顺序（`frontend/src/net/endpoints.ts`）：

- 自动：**官方优先**，失败后才用镜像取公开数据。`Authorization` 只加在 `kind === 'official'` 的请求上（有令牌也不会发给镜像）。
- 手动选择「官方 api.bgm.tv」可完全禁用镜像。
- 手动选择某条通道：只走该通道；镜像请求永远不带令牌。
- 自动/镜像模式下，`lain.bgm.tv` 的封面会改写为镜像图床；镜像图失败再回退官方地址。

镜像的边界：

- 镜像只能取公开数据；敏感/私密条目可能 404。
- 镜像运营方能看见你的访问记录；不要添加来路不明的地址。
- 页面会提示「当前详情来自镜像站，登录凭据未发送」。

服务端抓取仍使用 DoH + ECH（`internal/netx`），只用于抓取，与访客请求无关。

## 配置

支持 `.env` 与真实环境变量，环境变量优先。完整示例见 [`.env.example`](.env.example)。

| 变量 | 默认 | 说明 |
|---|---|---|
| `ANIPICK_ADDR` | `:8787` | 监听地址 |
| `ANIPICK_DATABASE_URL` | `postgres://ani_pick:ani_pick@127.0.0.1:5432/ani_pick?sslmode=disable` | PostgreSQL |
| `ANIPICK_ADMIN_PASSWORD` | `anipick` | 首次启动写入数据库的管理员密码 |
| `ANIPICK_SECRET` | — | 加密数据库内抓取凭据（AES-GCM） |
| `ANIPICK_BGM_TOKEN` / `ANIPICK_BGM_COOKIE` | — | 站长抓取凭据（首次启动种子值） |
| `ANIPICK_DOH` / `ANIPICK_ECH` | `true` | 服务端抓取的 DoH / ECH（与前台无关） |
| `ANIPICK_DOH_ENDPOINTS` | `https://dns.alidns.com/resolve,https://1.1.1.1/dns-query` | DoH 入口（国内优先 AliDNS） |
| `ANIPICK_EGRESS_PROXY` | — | **仅服务端抓取**的出站代理（http/https/socks5）；用户请求永不经过服务端 |
| `ANIPICK_CLIENT_IP_HEADER` | — | 客户端真实 IP 请求头（如 Cloudflare 的 `CF-Connecting-IP`）；仅当 TCP 对端属于 `ANIPICK_TRUSTED_PROXY_CIDRS` 时才采信，防止伪造头绕过限流 |
| `ANIPICK_TRUSTED_PROXY_CIDRS` | — | 可信反代网段（逗号分隔 CIDR，Cloudflare 见 [cloudflare.com/ips](https://www.cloudflare.com/ips/)）；与 `ANIPICK_CLIENT_IP_HEADER` 配套使用 |
| `ANIPICK_DB_MAX_CONNS` / `ANIPICK_DB_MIN_CONNS` | `12` / `2` | 连接池规格，默认按 1GB 内存机校准；换大机器按内存提高（每个连接对应一个 PG 后端进程） |
| `ANIPICK_CRAWL_TYPES` | `1,2,3,4,6` | 抓取类型 |
| `ANIPICK_CRAWL_PAGE_SIZE` | `100` | 每页条数（10–100） |
| `ANIPICK_CRAWL_DELAY_MS` | `350` | 请求间隔 |
| `ANIPICK_REFRESH_TTL_DAYS` | `30` | 过期天数 |
| `ANIPICK_REFRESH_BATCH` | `300` | 单次重爬上限 |
| `ANIPICK_INCREMENTAL_PAGES` | `10` | 增量每类型页数 |
| `ANIPICK_INCREMENTAL_INTERVAL_MIN` | `360` | 自动增量间隔 |
| `ANIPICK_AUTO_INCREMENTAL` / `ANIPICK_AUTO_REFRESH` | `true` | 自动调度 |
| `ANIPICK_PRUNE_ON_FULL` | `true` | 全量后清理旧数据 |

## API

| 端点 | 说明 |
|---|---|
| `GET /api/subjects` | 本站索引检索（参数见 README 上文） |
| `GET /api/subjects/{id}` | 本站索引详情 |
| `GET /api/tags` / `GET /api/meta-tags` | 筛选候选 |
| `GET /api/meta` | 站点统计 / 抓取状态 |
| `POST /api/admin/login` | 管理员登录（HttpOnly 会话） |
| `GET /api/admin/overview` | 统计 / 抓取状态 / 最近任务 |
| `GET/PUT /api/admin/settings` | 设置（敏感值脱敏） |
| `POST /api/admin/crawl` | 启动抓取 `full` / `incremental` / `refresh` |
| `POST /api/admin/crawl/cancel` | 停止任务 |
| `GET /api/admin/runs` | 任务历史 |
| `GET /api/admin/subjects` | 索引管理检索 |
| `POST /api/admin/subjects/{id}/refresh` | 单条刷新 |
| `DELETE /api/admin/subjects/{id}` | 从索引删除 |
| `GET /api/admin/network` | 服务端抓取出口的 DoH/ECH 诊断 |
| `GET /api/admin/metrics` | 性能监测：每路由耗时/大小/状态统计与链路拆分（DB/gzip/其余） |
| `POST /api/admin/metrics/reset` | 清零性能统计（重新开始累计） |

**不存在** `/api/bgm/*`、`/api/img`、用户凭证接收或 OAuth 兑换接口。

公开 GET 端点（subjects / meta / tags / meta-tags / subjects/{id}）响应带 `Cache-Control: public, max-age=30`：浏览器 30s 内复用；配合 Cloudflare 的 Cache Rule（对上述路径开启缓存、Edge TTL 尊重源站）可让热门列表请求完全被边缘吸收，源站只处理首跳。管理端与错误响应保持 no-store。

服务端在进程内再做一层结果缓存（数据只在抓取与管理端删改条目后变化），参数存系统设置，管理后台「设置 → 查询缓存」可调：

| 缓存 | TTL（默认） | 失效时机 |
| --- | --- | --- |
| 搜索页结果（多键） | `query_ttl` 60s | 抓取收尾 / 管理端删改条目，全量清空 |
| 搜索总数 count（多键） | 同上 | 同上 |
| 标签候选 `/api/tags`（多键） | 同上 | 同上 |
| 条目详情 `/api/subjects/{id}`（按 id，上限 256 键） | 同上 | 抓取收尾全量清空；刷新按 id、删除全量 |
| 公开 GET 响应 `Cache-Control` max-age | `public_ttl` 30s | 浏览器与 CF 边缘按 max-age 自然过期 |
| 元标签候选 `/api/meta-tags` | 10min（固定） | 同标签候选 |

TTL 取值范围 5–86400 秒，0/越界自动回落默认；保存后立即生效，无需重启。缓存未命中经单飞合并：同一键的并发回源只放一个进数据库，其余共享首份结果（TTL 到期瞬间 / 冷启动的重复请求只查一次库）。查询在脱离请求的后台 ctx 上执行——发起者的客户端断开不会让等待者一起失败，请求链路的 DB 计时归属保留。

## 目录结构

```
main.go                 装配配置 / 数据库 / 抓取器 / HTTP 服务
internal/config         .env + 环境变量
internal/db             连接与内嵌 SQL 迁移
internal/store          索引、标签、设置、会话、任务
internal/bgm            Bangumi 客户端（仅服务端抓取）
internal/netx           DoH 解析 + ECH（仅服务端出站）
internal/crawler        全量 / 增量 / 过期重爬 / 调度
internal/server         路由、管理会话、静态托管
internal/secretbox      设置项 AES-GCM（可选）
frontend/               Vite + React 19 + shadcn/ui
  src/net/endpoints.ts  官方 / 镜像通道、用户自定义镜像（本机）
scripts/                build / dev / smoke
```

## 发版

发版由 `.github/workflows/release.yml` 完成，触发条件是推送 `v*` 标签：

```bash
git switch main
git pull
git tag v1.0.0
git push origin v1.0.0
```

工作流会：

1. 校验标签提交是否在 `main` 上（不在则失败）；
2. 构建前端并嵌入 Go 二进制；
3. 生成 linux / windows / darwin 多平台二进制与 `checksums.txt`；
4. 推送多架构容器镜像到 GHCR：
   - `ghcr.io/<owner>/<repo>:1.0.0`
   - `ghcr.io/<owner>/<repo>:1.0`
   - `ghcr.io/<owner>/<repo>:1`
   - `latest`（非预发布标签）
5. 用 `git log <上一个 tag>..<当前 tag>` 生成 Release 日志，创建或更新 GitHub Release。

容器运行示例：

```bash
docker run --rm -p 8787:8787 \
  -e ANIPICK_DATABASE_URL='postgres://ani_pick:ani_pick@host.docker.internal:5432/ani_pick?sslmode=disable' \
  -e ANIPICK_ADMIN_PASSWORD='change-me' \
  -e ANIPICK_SECRET='change-me' \
  ghcr.io/<owner>/<repo>:1.0.0
```

GHCR 首次推送的 package 可能是私有；需要在 GitHub Package 设置里改为 public，匿名用户才能拉取。

## 测试

```bash
go test ./...                 # 配置 / 服务端 DoH 与 ECH / 抓取转换 / SPA
bash scripts/smoke.sh         # 运行中服务的 API 冒烟（含“无中继”断言）
cd frontend && node e2e.mjs   # Playwright：筛选、标签、详情、镜像通道、令牌隔离、后台、主题
cd frontend && node probe.mjs # 侧栏复杂控件探针
```

## 公开部署与合规

本站按**公开站点**运营，请注意：

1. **管理员入口**：页面不暴露任何管理入口链接（直接访问 `/admin`）；首次登录后修改密码；用反向代理 / IP 白名单限制 `/admin` 与 `/api/admin/*`；不要使用默认密码。经 Cloudflare 等反代暴露时，配置 `ANIPICK_CLIENT_IP_HEADER` + `ANIPICK_TRUSTED_PROXY_CIDRS`（如 `CF-Connecting-IP` + Cloudflare 网段），让限流按真实访客 IP 计数且不可伪造。
2. **数据范围**：本站索引只保存检索所需的条目事实字段（ID、名称、日期、排名、评分、标签与占比），不存储简介、剧集、角色等完整内容；这些由访客浏览器实时从 Bangumi 获取。
3. **图片**：直接由访客浏览器从 Bangumi 图床加载，不经本站转发；页面设置 `referrerpolicy="no-referrer"`。
4. **版权与条款**：条目与图片版权归 Bangumi 及原作者；请遵守 Bangumi 的 API 使用条款，保持可描述的 User-Agent 与合理抓取频率。
5. **敏感内容**：默认不展示；界面使用中性措辞；索引不保存任何露骨内容。
6. **隐私**：不收集用户凭据、不注入统计脚本；用户令牌只存在其浏览器，且只发往官方域名。使用第三方镜像时，镜像运营方可见访问记录；前台会明确提示。
7. **数据库**：只监听本机或内网，定期 `pg_dump`；`ANIPICK_SECRET` 用于加密抓取凭据。
8. **CSP 取舍**：因为支持用户自定义镜像，`connect-src`/`img-src` 放宽为任意 HTTPS；页面不使用内联脚本，`script-src` 仍为 `'self'`。若站点不开放自定义镜像，可把 CSP 收紧到内置域名。

## 已知限制

- 官方域名不可达时，未配置代理的访客**无法登录/查看鉴权内容**；镜像只提供公开数据。
- 镜像可用性、速度、数据完整性由第三方决定；内置镜像可能失效，可在登录框更换或自建。
- 全量抓取大类型（如书籍）耗时较长；可按类型分步执行。
