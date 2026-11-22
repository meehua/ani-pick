# AniPick

**本站自建多来源条目索引**（Bangumi + TMDB + Apple TV + MyAnimeList + AniList）
+ **按来源分派的详情获取**的作品检索站点。
Go 后端（单二进制，内嵌前端）+ PostgreSQL；前端 React 19 + shadcn/ui + Tailwind CSS v4。

## 术语与边界（请先读）

| 概念 | 位置 | 说明 |
|---|---|---|
| **本站索引** | 服务器 PostgreSQL | 由站点维护者从各数据源抓取并持续更新；对**所有访客**提供检索，不是“私有本地数据” |
| **用户本机** | 访客浏览器 localStorage | 只保存访客自己的 Bangumi 访问令牌；不上传、不入库 |
| **bgm 上游详情** | `api.bgm.tv` | 条目简介、角色、制作人员、剧集、关联作品由**访客浏览器直接请求**，本站不代理、不中转 |
| **扩展源详情** | 服务端代理端点 | TMDB / Apple TV / MyAnimeList / AniList 的详情由服务端代理获取并归一化（TMDB key 与限速不出服务端） |

本项目的硬性边界：

- **不提供任何 API 中继 / 反向代理 / 图片代理**（例外：扩展源详情代理端点，见上表）。服务端出站请求只用于自己抓取索引与管理员校验抓取凭据。
- 服务端**没有接收用户凭据的接口**；用户令牌只存在各自浏览器。
- 本站是公开站点：索引数据对所有访客可见；管理后台独立鉴权。

## 功能

- 本站索引内检索：类型多选、**来源筛选**（结果区 chips，条件写 URL）、标签 AND/OR、排除标签、**标签占比门槛**、分类标签、日期、排名、评分、评分人数、敏感内容模式。
- 排序：全站排名、评分、评分人数、日期、想看/看过/在看、ID；**网格 / 列表 / 表格三视图**，年代快捷筛选（近一年 / 各年代）映射到日期区间。
- 已选筛选条件以 chips 展示、可单独移除；窄屏筛选走底部抽屉，工具条吸顶，按 `/` 快速聚焦搜索。
- 条目页按来源分派：bgm 简介由浏览器直连（标签与占比、排名、评分分布来自索引）；扩展源简介/信息栏走服务端代理，评分分布只有 bgm 有，其余来源显示「暂无评分」。
- 管理后台：抓取概览、全量/增量/过期重爬、**按源手动抓取与最近运行状态**、任务历史、索引管理、站长凭据、抓取出口诊断；tab 与数据页搜索状态写进 URL。
- 返回不丢状态：浏览页结果与滚动位置按会话缓存，从条目页返回即时还原；前进导航自动回到页顶。
- 用户登录：个人 Access Token，本机保存；登录框可切换**官方 / 镜像通道**并添加自定义镜像（令牌只发官方）。
- 深浅色跟随系统、响应式布局（窄屏触控目标 ≥44px）、URL 即状态。

## 数据源

| 来源 | `source` | 是什么 | 抓取范围 | 节流 | 条目类型 |
|---|---|---|---|---|---|
| Bangumi | `bgm` | ACGN 社区条目库（原生索引） | 全类型全量枚举 + 增量 + 过期重爬（凭据见「用户登录与详情」） | `crawl_delay_ms` 默认 350ms | 1/2/3/4/6 |
| TMDB | `tmdb` | The Movie Database v3（需 API key） | 电影/剧集热度榜各 20 页 ≈ 400 条/类；zh-CN 元数据 | 250ms | 101 电影 / 102 剧集 |
| Apple TV（iTunes） | `itunes` | Apple iTunes 官方公开接口 | 台湾区电影榜单（实测约 100 条，见下方说明） | 3s/请求 | 101 电影 |
| MyAnimeList | `mal` | **官方 v2 API**（凭 Client ID，`X-MAL-CLIENT-ID` 头） | `/anime/ranking` 榜单（单页上限 500，全量/增量各 1 页） | 750ms | 2 动画 |
| AniList | `anilist` | AniList 官方公开 GraphQL | 热度榜前 10 页 ≈ 500 条（tags 按 rank≥20 过滤剧透） | 1.2s | 2 动画 |

抓取方式：

- 全源入口：管理后台「抓取设置」顶部的「启动抓取」卡一键启动全部启用来源（`full` / `incremental` / `refresh` 各对应一键），并显示上次全源抓取时间；卡片同区提供**全局默认保留期**。
- 合并模式：`full` / `incremental` 先跑原生索引主流程，随后各扩展源串行抓取（源间间隔 2s，只抓设置里启用中的源、按有效优先级排序）；`refresh` 原生索引逐条重爬过期条目后，**只重抓条目已超过各自有效保留期的扩展源**。每个来源在任务历史里各占一行（含独立的状态/计数/错误）。
- 单源抓取：管理后台「抓取设置」的来源卡片按源手动触发（`POST /api/admin/crawl` 带 `source`）；TMDB 未配置 key 时该源显示「未配置」、抓取跳过（记为「已跳过」），不影响其他来源。
- **保留期（新鲜度）**：全局默认一处（抓取设置顶卡），每个源卡内可覆盖（0 = 用全局默认）；过期重爬与各源的新鲜度判定（含源卡上的过期条目计数）统一读「源覆盖值 ?? 全局默认」。
- 扩展源不参与原生索引的全量清理；标签热度在每轮运行末尾统一重算。

### 来源登记表（单一事实源）

每个来源从注册到下线的全部可变语义只声明在 `internal/registry/registry.go` 一处：
标识与中文徽标文案、是否原生索引、主行优先级、条目外链模板、类型编码→**原生标签**、
类型编码→站点分类（kind）、站点分类中文标签、评分换算满分、详情获取策略（`proxy`=服务端代理 /
`direct`=浏览器直连）、抓取节流与页数预算、**抓取模式能力清单**、凭据字段声明（如 TMDB 的 API Key）。
matcher、crawler、server 与前端都不自带词表。

**双层词表**：源原生层（各源自己的类型叫法，站点不改名——bgm 的类型 6 永远叫「三次元」）
与站点分类层（kind：浏览筛选 chips / 首页类型卡 / 概览分类构成；动画/电影/剧集/书籍/音乐/游戏，
想改类目名只动登记表一行）。详情页类型徽标与数据面板按行显示该行所在源的原生标签，
站点级筛选与统计按 kind 层渲染（前端把 `by_type` 计数按登记表映射聚合到 kind）。

- 后端经 `/api/meta` 的 `sources` 清单（原生 types 含 kind 映射 + 抓取模式能力）与
  `kinds` 词表下发（只含设置里启用中的源，按有效优先级排序）；前端的来源徽标/分类 chips/
  首页类型卡/抓取按钮组/详情页分派全部由它驱动（消费入口 `frontend/src/api/vocab.ts`），
  清单之外的来源不渲染。
- 匹配器的候选圈定 `(source, type) IN (登记表声明的同 kind 组合)`，且只在可跨源比对的
  kind（动画/电影/剧集）内进行——书籍/音乐/游戏自成作品不查候选；主行优先级 =
  登记表默认 + 管理设置覆写；年份窗口等阈值同样取自登记表。
- 管理后台「抓取设置」是来源相关配置与操作的唯一去处：每源一张紧凑卡
  （徽标 + 原生类型 + 最近运行 + 权重/排序/启用 + **该源自己的抓取按钮组**），
  bgm 与扩展源同构——按钮组就是该源声明的模式（bgm：全量/增量/过期重爬；扩展源：
  全量/增量收获；iTunes 这类无增量的源只有全量）。展开器（默认收起）里是各源自己的
  配置：bgm 的抓取类型/参数/站长凭据，TMDB 的双鉴权凭据（v3 API Key 与 v4 读访问
  令牌，有读令牌优先走 `Authorization: Bearer`）——凭据校验按钮也按登记表声明出现
  （bgm 校验站长凭据、TMDB 请求 `/3/authentication` 验草稿值，其余源无校验器）。
  **每源请求间隔**同样在展开器里调（0 = 登记表默认；下限按官方限速口径钳回：
  bgm 350ms、TMDB 250ms、MyAnimeList 400ms/下限 350ms、AniList 1200ms/下限 700ms、
  Apple TV 3000ms 贴住官方“约 20 次/分钟”），保存后立刷节流时间槽，无需重启。
  出站代理也在此调整（保存后立即重建出站通道）。
  凭据字段按登记表声明渲染，清单外拒绝；secretbox 加密存库。
  停用的源不进 manifest、抓取自动跳过、浏览页 chips 消失（已入库条目保留）；
  顺序调整后点「重新配对作品」即可对既有作品组重排主行；权重调整保存后自动异步重算合成评分。
- 新增一个源的改动面：登记表加一行 + 适配器 + 测试，三处以内。

### 作品合并（分组制）

同一作品在各来源的行归为一组（`subjects.work_id` = 组内主行 id），**物理数据保留各源原样**；检索/计数一律塌缩到主行展示，重复卡不出现。主行按源优先级选取：**bgm > AniList > MyAnimeList > TMDB > Apple TV**（同级取 id 小）；更高优先级源加入既有组时（如 bgm 后来才抓到），整组 `work_id` 单条 UPDATE 重排。

匹配器在写入路径上做三级匹配（拿不准就拒绝，**宁缺毋滥**）：

1. `(source, source_ref)` 已存在 → 保持原组；
2. 跨源确定性键：AniList 的 `idMal` 与 MyAnimeList 的 `mal_id` 相等 → 同作品（存 `ext_key` 列）；② 命中后还会跑一次 ③ 补充——排除已同意的组后若唯一命中其他组（如原生索引行），确定性+启发式双重同意即并入；
3. 启发式：同类（可比对类别由登记表声明：动画={bgm 动画, mal, anilist}；电影={tmdb movie, itunes movie}；剧集={tmdb tv, bgm 三次元}）+ 年份 ±1 + 标题归一化后完全相等（NFKC + 小写 + 去空白标点；候选标题含 bgm name/name_cn、AniList 英文/罗马字/日文/同义词、MAL 英文/日文/同义词、TMDB 原名/译名）。**多个候选打平 → 不合并**，自成作品并记日志。

- 同源行之间绝不互并（同源同名同年可能是不同作品）；bgm 全量清理删除组锚点时，作品组自动重指到最优扩展源行继续存活。
- 来源筛选语义 =「该作品组内含此来源的记录」；来源 chips 与 `stats.by_source` 的计数为**覆盖作品数**（同作品多来源只计一次）。

### 评分合成（各源权重加权）

评分在两个口径上并存（迁移 0007 起）：

- `raw_score` / `raw_rating_total`：每行**该来源自己的原始值**（10 分制归一后），任何后续写入都不改动它——详情页「各源评分」对比行展示的就是它，带各源官方外链；
- 作品组主行的 `score` / `rating_total`：**按管理员配置的各源权重加权合成的组级值**，`Σ(w_i×s_i)/Σ(w_i)`（只计 raw_score>0 的源），评分人数取组内合计；检索排序与卡片大字评分直接走它（物化在主行上，查询走索引）。

权重在管理后台「抓取设置」按源配置（0–100，0 = 该源评分不参与合成；默认全部 100，iTunes 无评分数据默认 0）。全量重算是一条 UPDATE 聚合语句，触发时机：抓取收尾、重新配对作品、**设置保存后（异步，不阻塞响应）**。评分分布环形图仍只认 bgm 分布（其余源无分布数据）。

- 详情响应带 `sources` 数组（组内每源的原始评分与官方外链），详情页头部下方渲染多源评分对比行；主行缺简介时用组内其他来源的简介补（页面标注简介来源）。
- 「重新配对作品」：管理后台「抓取设置」或 `POST /api/admin/works/rematch`——先完整回填存量行（anilist 每 50 id 一次批量请求取 `idMal` 与全部标题变体写回 `ext_key`/`alt_titles`；mal 行写入时自带 MAL id 键，无需回填），再对仍自成作品的扩展源行重新归组，并对全部多源组按当前有效优先级重排 + 全量重算合成评分；已并入多源组的行保持不动，可重复执行。

来源实测备注（2026-11，决定当前采集形状的硬事实）：

- **iTunes**：中国区（CN）电影榜单 RSS 返回空 feed、`lookup?country=CN` 恒 0 结果（CN 区不售卖电影）；`search` 对 movie/tvSeason 实体在全部地区均返回 0（Apple 已下线该类目搜索）；`top-tvseasons` 等剧集 feed 名已被 RSS 服务拒绝。因此本源取**台湾区**（zh-TW 元数据，华语可读）榜单 + `lookup&country=tw` 详情，**只收录电影**；RSS/lookup 均已不返回评分字段，本源 `score`/`rating_total` 恒 0。
- **TMDB**：API Key 在管理后台「抓取设置」的 TMDB 卡片里填写（secretbox 加密存库，仅服务端使用，绝不暴露给浏览器）；未填写时浏览页不出现该来源。
- **AniList**：无中文标题（name 取英文名，缺省罗马字）；评分取 `meanScore`（百分制 ÷10），评分人数取评分分布总和；标签带 rank/votes，rank≥20 且非剧透才入库。
- **MyAnimeList**：官方 v2 API，凭 Client ID（管理后台「抓取设置」填写，secretbox 加密存库；未配置时该源整体跳过，浏览页不出现该来源）；无中文标题，synopsis 为英文。`source_ref` 即 MAL 条目 id，与 AniList 的 `idMal` 同一 ID 空间，确定性合并直接成立。

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

可选：`ANIPICK_PORT`（宿主机端口，默认 8787）。其余一切运行期配置（凭据、抓取参数、出站代理、DoH/ECH、真实 IP 识别）都在管理后台调整，不走环境变量。

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

> 服务端抓取需要代理时，在管理后台「抓取设置 → 网络」的出站代理里填（保存后立即重建出站通道）；`extra_hosts` 已配置 `host.docker.internal:host-gateway`；访客请求不会经过服务端，也不受这个代理影响。

## 工作原理

```
站长 Bangumi 账号 ──┐
TMDB / Apple TV /   ├─► 抓取器（DoH + ECH 出站，按源节流）──► PostgreSQL（本站索引）
MyAnimeList / AniList ┘                                            │
访客浏览器 ──► /api/subjects（筛选/排序/分页/来源）─────────────────┘
     │
     ├──► https://api.bgm.tv（bgm 详情；凭据只在本机 localStorage）
     └──► /api/subjects/{id}/detail（扩展源详情，服务端代理 + 进程内缓存）
```

## 抓取与索引维护

| 模式 | 行为 | 适用 |
|---|---|---|
| 全量 `full` | bgm 逐类型按日期倒序枚举；成功且无跳页时清理本轮未覆盖的旧条目（只清 bgm）；随后各扩展源按预算抓取 | 首次建库、重建 |
| 增量 `incremental` | **真增量**：bgm 按日期窗口拉各类型最新页 + 过期条目重爬；AniList 取「上次运行之后开播」窗口 + 人气榜保底页；TMDB 取「上次运行之后上映/开播」窗口（人气序）；MAL 取在播榜单（官方无日期发现端点）。**空数据（该源还没完成过增量，如首次安装/清空后）自动按全量预算补齐**，bgm 空类型也按全量跑到底——条目不能丢；已有条目的变更统一靠过期重爬（上游无变更流） | 日常维护、自动调度 |
| 过期重爬 `refresh` | 原生索引：`fetched_at` 超过有效保留期的条目按排名优先；扩展源：只重抓有过期条目的源（保留期按源覆盖或全局默认） | 保持排名/标签占比不过期 |

- 全量清理只在**本轮无跳页**时执行，避免半途失败删空索引。
- 上游 404/403 标记 `locked` 并退避；连续失败 5 次后不再自动重试。
- 自动调度每 2 分钟检查一次是否到期；自动增量与自动过期重爬各自支持三种触发形式（固定间隔 / 每日定点 / 每隔 N 天定点，定点按服务器本地时间），在「抓取设置 → 启动抓取」的自动任务区配置；检查周期可调（默认 2 分钟）。
- 调度器与抓取的内部参数全部进后台设置（带默认推荐值与范围提示）：调度精度、页重试、请求重试/退避/429 封顶/响应上限/扩展源超时与 UA、源间间隔、标签上限、合并年份容差、配对分批、过期单条跳过阈值；服务器侧的会话缓存、压缩阈值、设置快照周期、性能样本上限、总览条数、停机等待、面板轮询间隔、返回缓存页数在「设置 → 服务器与面板」。每源收获预算（全量/增量页数、单页条数、标签门槛、商店地区）在各源卡展开区。
- 重启会中断当前任务（记为 `interrupted`），重新启动即可，写入幂等。
- 量级参考：动画约 3 万、书籍约 40 万；全量耗时与所选类型成正比。

写入的字段（只存筛选所需）：

- `subjects`：`id`（bgm 用 Bangumi 主键；扩展源从 `subject_ext_id_seq` 分配，1e10 起与 bgm 空间隔离）、**`source`/`source_ref`**（来源标识与上游原始 id，`(source, source_ref)` 唯一）、类型、名称、日期、平台、全站排名（扩展源无排名概念，恒 0——「排名」排序建议配合来源筛选使用）、评分（扩展源已归一 10 分制）、评分人数、收藏计数、敏感标记、分类标签（bgm 专属，扩展源为空）、封面 URL、标签总票数、抓取时间、锁定/失败计数。
- `subject_tags`：`(subject_id, tag_id, votes, pct)`；`pct` = 该标签票数 ÷ 该条目全部标签票数。扩展源标签无票数概念，`votes`/`pct` 恒 0（占比门槛筛选对它们不生效，名字筛选照常可用），每条目截断 ≤8 个。
- `tag_usage`：按类型的标签热度（跨来源按类型聚合），供筛选候选；`meta_tag_usage` 只聚合 bgm 原生类型（type<100）。

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

**运行期配置一律在管理后台调整，不走环境变量**：数据源启用/顺序/评分权重/每源请求间隔/凭据/抓取参数在「抓取设置」，出站代理与 DoH/ECH 在「抓取设置 → 网络」，真实 IP 识别在「设置 → 限流与安全」。环境变量只承担引导期必需项与基础设施规格两类；非法的时长值会**启动即报错**（fail fast），不静默回落。

| 变量 | 默认 | 说明 |
|---|---|---|
| `ANIPICK_ADDR` | `:8787` | 监听地址 |
| `ANIPICK_DATABASE_URL` | `postgres://ani_pick:ani_pick@127.0.0.1:5432/ani_pick?sslmode=disable` | PostgreSQL |
| `ANIPICK_ADMIN_PASSWORD` | `anipick` | 首次启动写入数据库的管理员密码 |
| `ANIPICK_SECRET` | — | 加密数据库内抓取凭据（NaCl secretbox） |
| `ANIPICK_DEBUG` | `false` | 调试日志 |
| `ANIPICK_DB_MAX_CONNS` / `ANIPICK_DB_MIN_CONNS` | `12` / `2` | 连接池规格，默认按 1GB 内存机校准；换大机器按内存提高（每个连接对应一个 PG 后端进程） |
| `ANIPICK_DB_CONN_LIFETIME` | `1h` | 连接最长存活时间；防反代侧断连后拿到死连接 |
| `ANIPICK_DB_CONN_IDLE_TIME` | `0` | 连接空闲回收时间；`0` = 不按空闲回收 |
| `ANIPICK_DB_HEALTH_CHECK_PERIOD` | `30s` | 空闲连接健康检查周期 |
| `ANIPICK_HTTP_READ_HEADER_TIMEOUT` | `10s` | 读请求头超时（Slowloris 防护） |
| `ANIPICK_HTTP_READ_TIMEOUT` | `30s` | 读整个请求超时 |
| `ANIPICK_HTTP_WRITE_TIMEOUT` | `120s` | 写响应超时 |
| `ANIPICK_HTTP_IDLE_TIMEOUT` | `90s` | 空闲连接保活时长 |

duration 值用 Go 格式（`90s` / `2m` / `1h`）。

### 升级提示（1.0.x → 当前）

- `.env` 里配过 `ANIPICK_BGM_TOKEN` / `ANIPICK_BGM_COOKIE` 的：这两项**已删除**（不再作为首次启动种子）。升级后请在管理后台「抓取设置」的 bgm 卡片里重填一次站长凭据（secretbox 加密存库）。
- `.env` 里的 `ANIPICK_CRAWL_*` / `ANIPICK_REFRESH_*` / `ANIPICK_INCREMENTAL_*` / `ANIPICK_AUTO_*` / `ANIPICK_PRUNE_ON_FULL` / `ANIPICK_BGM_TIMEOUT_SEC` / `ANIPICK_DOH*` / `ANIPICK_ECH` / `ANIPICK_EGRESS_PROXY` / `ANIPICK_CLIENT_IP_HEADER` / `ANIPICK_TRUSTED_PROXY_CIDRS` 全部删除：对应配置分别迁入「抓取设置」与「设置 → 限流与安全」。数据库里已保存的设置原样保留，缺失项用内置默认值补齐，此后以管理后台为准。

## API

| 端点 | 说明 |
|---|---|
| `GET /api/subjects` | 本站索引检索（参数见 README 上文；`source` 过滤来源） |
| `GET /api/subjects/{id}` | 本站索引详情 |
| `GET /api/subjects/{id}/detail` | 扩展源详情代理（tmdb/itunes/mal/anilist；bgm 条目返回 400） |
| `GET /api/tags` / `GET /api/meta-tags` | 筛选候选 |
| `GET /api/meta` | 站点统计 / 抓取状态 |
| `POST /api/admin/login` | 管理员登录（HttpOnly 会话） |
| `GET /api/admin/overview` | 统计 / 抓取状态 / 最近任务 |
| `GET/PUT /api/admin/settings` | 设置（敏感值脱敏） |
| `POST /api/admin/crawl` | 启动抓取 `full` / `incremental` / `refresh`；带 `source` 时按单源执行（模式须在该源的登记表能力内：原生索引 `full`/`incremental`/`refresh`，扩展源 `harvest`/`harvest_incremental`） |
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
| 条目详情 `/api/subjects/{id}`（按 id，键上限 `max_keys` 256） | 同上 | 抓取收尾全量清空；刷新按 id、删除全量 |
| 公开 GET 响应 `Cache-Control` max-age | `public_ttl` 30s | 浏览器与 CF 边缘按 max-age 自然过期 |
| 首页元信息 `/api/meta` | `meta_ttl` 30s | 30s 自然过期 |
| 元标签候选 `/api/meta-tags` | `meta_tags_ttl` 10min | 同标签候选 |
| 扩展源详情代理 `/api/subjects/{id}/detail`（按 id，键上限 `max_keys`） | `ext_detail_ttl` 10min | 同详情缓存（回源要等上游响应，秒级） |
| 后台总览 `/api/admin/overview` | `overview_ttl` 3s | 抓取/设置等动作后主动失效 |

TTL 语义：**填 0 = 关闭该层缓存**（每次都实查/响应退回 `no-store`）；不填或负值回落默认（兼容旧库数据）；正值只钳下限 5s（overview 1s、元标签 10s），**不设上限**——失效靠显式钩子，TTL 只是兜底，可设任意长。`public_ttl` 是浏览器与 CF 边缘的**自然过期**时长，不随抓取主动失效，设得很大意味着访问者会长期看到旧列表。键上限 `max_keys` 16–10000（0 回落默认 256；搜索页/标签候选/详情/搜索总数共用，超限整体换新）；保存后立即生效，无需重启。缓存未命中经单飞合并：同一键的并发回源只放一个进数据库，其余共享首份结果（TTL 到期瞬间 / 冷启动的重复请求只查一次库）。查询在脱离请求的后台 ctx 上执行——发起者的客户端断开不会让等待者一起失败，请求链路的 DB 计时归属保留。

**过期后台刷新（stale-while-revalidate）**：`/api/meta` 与后台总览过期后先把旧响应原样返回，聚合在后台单飞重算（同一时刻至多一次）——轮询/进站永远不等数据库，弱机上持续轮询不再阻塞请求；主动失效清空缓存后下一次请求仍同步实算。管理总览的统计查询走部分索引（0008 迁移）仅索引扫描，生产 169k 行上每轮回源从数百毫秒降到毫秒级。

限流与会话参数存系统设置，管理后台「设置 → 限流与安全」可调（0/越界自动回落默认，保存后立即生效）：

| 参数 | 默认 | 范围 | 说明 |
| --- | --- | --- | --- |
| `rate_limit_rps` / `rate_limit_burst` | 30 / 120 | 1–1000 / 10–10000 | 每 IP 常规请求令牌桶速率与突发容量 |
| `login_rpm` / `login_burst` | 5 / 6 | 1–60 / 1–100 | 登录尝试节流（次/分钟/IP），防密码爆破 |
| `login_delay_ms` | 300 | 0–2000 | 密码错误的响应延迟，拉高爆破成本 |
| `session_ttl_days` | 7 | 1–90 | 管理员会话有效期（对新会话生效） |
| `limiter_max_ips` | 4096 | 256–65536 | 限流表 IP 条目硬上限（超限先清 10 分钟未活跃条目，仍满强制淘汰最旧的） |
| `max_page` | 5000 | 100–100000 | 公开搜索最大页码（深分页成本上限） |

## 目录结构

```
main.go                 装配配置 / 数据库 / 抓取器 / 数据源适配器 / HTTP 服务
internal/config         .env + 环境变量
internal/db             连接与内嵌 SQL 迁移
internal/store          索引、标签、设置、会话、任务（身份读写收口在 UpsertSubjects）
internal/bgm            Bangumi 客户端（仅服务端抓取）
internal/sources        扩展数据源适配器（tmdb/itunes/mal/anilist：抓取 + 详情归一化）
internal/netx           DoH 解析 + ECH（仅服务端出站）
internal/crawler        全量 / 增量 / 过期重爬 / 数据源抓取 / 调度
internal/server         路由、管理会话、静态托管、扩展源详情代理
internal/secretbox      设置项加密（NaCl secretbox，可选）
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

### 版本标识

版本标识为 `版本号-提交哈希`（如 `0.1.2-4f2a9c1`），由 tag 推导的 `VERSION` 与 `git rev-parse --short HEAD` 组成，构建时同源注入前后端：

- Go 二进制：`-ldflags "-X main.version=… -X main.commit=…"`（Dockerfile / release.yml 已接好）；
- 前端：vite 构建注入 `__APP_BUILD__`，Docker 构建经 `ANIPICK_VERSION` / `ANIPICK_COMMIT` 与 Go 侧取同一组值。

运行期页脚 logo 右侧展示服务端标识（`/api/meta` 的 `build` 字段），前端加载后与自身 `__APP_BUILD__` 比对：不一致则弹窗引导刷新（浏览器/CDN 缓存旧页面时会出现）；本地裸构建（无 ldflags）标识退化为 `dev`，视为开发实例不弹窗；vite dev 模式不弹窗，仅在页脚版本处提示不一致。

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
go test ./...                 # 配置 / 服务端 DoH 与 ECH / 抓取转换 / SPA / 数据源适配器
bash scripts/smoke.sh         # 运行中服务的 API 冒烟（含“无中继”断言）
cd frontend && node e2e.mjs   # Playwright：筛选、标签、详情、镜像通道、令牌隔离、后台、主题
cd frontend && node probe.mjs # 侧栏复杂控件探针
```

数据库集成测试（upsert 身份含 id 序列、source 过滤、Prune 来源隔离、迁移幂等）在共享库的
独立 schema 上运行、不触碰线上数据，默认跳过；提供 `ANIPICK_TEST_DSN` 后启用：

```bash
ANIPICK_TEST_DSN='postgres://ani_pick:ani_pick@127.0.0.1:5432/ani_pick?sslmode=disable' go test ./internal/store/
```

## 公开部署与合规

本站按**公开站点**运营，请注意：

1. **管理员入口**：页面不暴露任何管理入口链接（直接访问 `/admin`）；首次登录后修改密码；用反向代理 / IP 白名单限制 `/admin` 与 `/api/admin/*`；不要使用默认密码。经 Cloudflare 等反代暴露时，在管理后台「设置 → 限流与安全」配置真实 IP 请求头与可信网段（如 `CF-Connecting-IP` + Cloudflare 网段），让限流按真实访客 IP 计数且不可伪造。
2. **数据范围**：本站索引只保存检索所需的条目事实字段（ID、来源、名称、日期、排名、评分、标签与占比），不存储简介、剧集、角色等完整内容；bgm 的这些内容由访客浏览器实时从 Bangumi 获取，扩展源的简介经服务端代理端点按需获取并缓存（不落库）。
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
- **混源排序语义**：「全站排名」是 bgm 专属概念（扩展源 rank 恒 0，排在有排名条目之后）；对扩展源条目排序请配合来源筛选（`?source=tmdb`）使用，混源排名列不具可比性。
- **扩展源评分分布**：评分分布（1-10 分档）只有 bgm 有；TMDB/Apple TV/MyAnimeList/AniList 详情页评分显示数值但扇形图退化为「暂无评分」。
- **MyAnimeList 可达性**：`api.myanimelist.net` 走 CloudFront，多数环境可直连；被阻断时抓取失败记录报错，可在管理后台「抓取设置 → 网络」的出站代理里配置代理，保存后立即换新出站通道。
- **扩展源条目量级**：各扩展源只抓取热门榜单（约数百条），不是全量镜像；Apple TV 源受 Apple 官方接口限制只收录台湾区电影榜单（约 100 条、无评分数据），剧集暂无官方发现通道。
