// 本站索引（AniPick 后端）与 Bangumi 直连 API 的类型。
//
// 来源与类型词表不在前端硬编码：后端来源登记表（internal/registry）经
// /api/meta 的 sources 清单下发，消费入口见 api/vocab.ts（sourceLabel /
// typeLabel / typeOrder / isExtSource / availableTypes 等）。

/** 来源 manifest 里的一个类型词条（登记表词表的下发形态）。
 *  label 是该源的原生标签（源资产，站点不改名）；kind 是站点级分类。 */
export interface ManifestType {
  code: number
  label: string
  /** 站点级分类（kind 层渲染与合并匹配用） */
  kind?: string
}

/** 站点级分类词表（双层词表的外层）：浏览 chips/首页类型卡按它渲染。 */
export interface ManifestKind {
  kind: string
  label: string
  /** 首页类型卡副文案 */
  hint?: string
}

/** 来源支持的一个抓取模式（登记表能力清单的下发形态）。 */
export interface ManifestMode {
  mode: string
  label: string
  /** 按钮提示（副文案） */
  hint?: string
}

/** 来源 manifest 里的一个凭据字段声明（设置页照单渲染）。 */
export interface ManifestCred {
  key: string
  label: string
  secret: boolean
}

/** 来源登记表的公开清单行：徽标文案/优先序/详情策略/配置状态/类型词汇/凭据声明。 */
export interface ManifestSource {
  id: string
  label: string
  /** 是否原生索引（浏览页 chips 常驻；无 source 字段的旧行归属） */
  native: boolean
  /** 有效优先级（越小越优先；管理设置可调） */
  priority: number
  /** 配置状态（凭据是否已填写） */
  configured: boolean
  /** 登记表声明支持凭据校验（抓取设置页才渲染「校验凭据」按钮） */
  validate?: boolean
  /** 请求间隔默认值（ms，官方限速口径；间隔输入的 placeholder） */
  interval_default?: number
  /** 请求间隔硬下限（ms） */
  interval_floor?: number
  /** 收获预算声明（0 = 该源无此参数；源卡照单渲染预算输入） */
  full_pages?: number
  inc_pages?: number
  page_size?: number
  page_size_max?: number
  tag_min_rank?: number
  region?: string
  types: ManifestType[]
  /** 该源支持的抓取模式（抓取设置页按此渲染按钮组） */
  modes?: ManifestMode[]
  /** 需要在设置页填写的凭据字段 */
  credentials?: ManifestCred[]
}

export interface TagVote {
  name: string
  votes: number
  pct: number
}

/** 作品组内一个来源行（详情页多源评分对比/简介补缺用） */
export interface SubjectSourceRow {
  id: number
  source: string
  /** 该来源自己的原始评分（合成评分在主行上，单独展示） */
  score: number
  rating_total: number
  /** 该行的评分分布（1~10 分十档人数；空/缺 = 该源无分档数据） */
  dist?: number[]
  /** 该来源官方站点的条目外链 */
  url: string
  /** 该行自己的标题/平台/日期/封面（区分同组多行，如剧场版与 TV 各一行） */
  name?: string
  name_cn?: string
  platform?: string
  date?: string
  image?: string
  /** 该行自己的简介（主行缺简介时的库内兜底） */
  summary?: string
}

export interface Subject {
  id: number
  /** 数据来源（bgm/tmdb/itunes/mal/anilist）；缺省视为 bgm */
  source: string
  type: number
  name: string
  name_cn: string
  date: string
  platform: string
  rank: number
  score: number
  rating_total: number
  wish: number
  collect: number
  doing: number
  on_hold: number
  dropped: number
  nsfw: boolean
  meta_tags: string[]
  image: string
  /** 条目简介（抓取时随上游载荷入库；详情页只读本站索引数据） */
  summary?: string
  tags_total: number
  locked: boolean
  fetched_at: string
  tags?: TagVote[]
  /** 所在作品组内的全部来源行（仅详情响应有；单源作品只有自己） */
  sources?: SubjectSourceRow[]
  /** 主行在各源官方站点的条目外链（仅详情响应有，服务端按登记表模板拼出） */
  url?: string
}

export interface SearchResult {
  items: Subject[]
  total: number
  page: number
  page_size: number
}

export interface TagStat {
  name: string
  count: number
  votes: number
}

export interface Stats {
  total: number
  by_type: Record<string, number>
  /** 按来源的条目计数（浏览页来源 chips 按此显隐） */
  by_source?: Record<string, number>
  tags: number
  subject_tags: number
  last_fetched: string | null
  nsfw: number
  db_size: number
}

export interface CrawlProgress {
  phase: string
  /** 当前阶段的数据来源（合并运行随阶段切换；单源抓取固定） */
  source?: string
  type: number
  offset: number
  total: number
  pages: number
  requests: number
  inserted: number
  updated: number
  refreshed: number
  errors: number
  skipped: number
  pruned: number
  message: string
  updated_at: string
}

export interface CrawlState {
  running: boolean
  run_id: number
  mode: string
  trigger: string
  started_at: string
  progress: CrawlProgress
}

export interface Run {
  id: number
  mode: string
  /** 该行运行记录属于的数据源（合并运行里每个来源各占一行） */
  source?: string
  trigger: string
  status: string
  types: number[]
  stats: CrawlProgress | Record<string, unknown>
  error: string
  started_at: string
  finished_at: string | null
}

// 自动任务触发方式：interval 间隔分钟；daily 每天定时；every_days 每隔几天定时
// （后两种在当天的 at_minute 分钟触发，服务器本地时间）
export interface AutoSchedule {
  kind: 'interval' | 'daily' | 'every_days'
  every_min: number
  at_minute: number
  every_days: number
}

export interface CrawlSettings {
  types: number[]
  page_size: number
  request_timeout_sec: number
  refresh_ttl_days: number
  refresh_batch: number
  incremental_pages: number
  /** 旧字段：增量间隔的镜像值（权威判定在 incremental_schedule） */
  incremental_interval_min: number
  incremental_schedule: AutoSchedule
  refresh_schedule: AutoSchedule
  auto_incremental: boolean
  auto_refresh: boolean
  prune_on_full: boolean
  /** 任务稳健性（原为代码常量，现全部后台可调；缺省 = 默认推荐值） */
  scheduler_tick_sec: number
  page_fail_retries: number
  page_retry_wait_sec: number
  refresh_progress_every: number
  scheduler_lookback: number
  stale_retry_cap: number
  source_gap_ms: number
  ext_tag_max: number
  /** 请求与重试（bgm 与扩展源通道同一口径） */
  request_attempts: number
  request_backoff_ms: number
  retry_after_cap_sec: number
  response_limit_mb: number
  outbound_timeout_sec: number
  /** 合并与配对 */
  match_year_window: number
  rematch_batch: number
  /** 同级打平的放播日期贴近容差（天） */
  match_date_days: number
  /** 多对多类型匹配组（同组成员之间允许跨源配对；nil 后端落推荐默认） */
  match_groups: TypeRef[][]
}

export interface CacheSettings {
  query_ttl: number
  public_ttl: number
  meta_ttl: number
  meta_tags_ttl: number
  overview_ttl: number
  max_keys: number
  /** 元标签候选缓存的键上限；0 = 默认 64 */
  meta_tags_max_keys: number
}

// 限流与会话安全参数（管理后台「设置 → 限流与安全」可调）
export interface SecuritySettings {
  rate_limit_rps: number
  rate_limit_burst: number
  login_rpm: number
  login_burst: number
  login_delay_ms: number
  session_ttl_days: number
  limiter_max_ips: number
  max_page: number
  /** 客户端真实 IP 请求头（如 CF-Connecting-IP）；空 = 用 TCP 对端 */
  client_ip_header: string
  /** 可信反代网段（逗号分隔 CIDR）；空 = 不读头 */
  trusted_proxy_cidrs: string
}

/** 单个数据源的管理设置（「设置 → 数据源」可调） */
export interface SourceSetting {
  enabled: boolean
  /** 资料优先序（越小越优先） */
  priority: number
  /** 评分权重（0-100，0 = 不参与合成评分） */
  weight: number
  /** 请求间隔（毫秒）；0 = 用登记表默认（每个源的最佳值） */
  interval_ms: number
  /** 保留期覆盖（天）；0 = 用全局默认（保留期策略每个源都适用） */
  refresh_ttl_days: number
  /** 收获预算覆写（0/空 = 登记表默认；单页条数上限按登记表硬顶钳） */
  page_size: number
  full_pages: number
  inc_pages: number
  tag_min_rank: number
  region: string
}

/** 服务端抓取出口的运行配置（抓取设置 → 网络可调） */
export interface NetworkSettings {
  /** 抓取出站代理（http/https/socks5）；空 = 直连或 DoH */
  egress_proxy: string
  /** 出站走 DoH 解析（代理优先于 DoH） */
  doh: boolean
  /** TLS ECH（DoH 启用时生效） */
  ech: boolean
  /** DoH 入口清单；空 = 内置默认 */
  doh_endpoints: string[]
  /** DoH 解析超时（秒）；默认 6 */
  doh_timeout_sec: number
  /** DNS 结果最短缓存（秒）；默认 60 */
  dns_cache_min_sec: number
  /** DNS 结果默认缓存（秒）；默认 300 */
  dns_cache_ttl_sec: number
  /** 扩展源出站 User-Agent；空 = 内置默认 */
  outbound_ua: string
}

/** 匹配组成员：一个来源的一个类型 */
export interface TypeRef {
  source: string
  type: number
}

/** 服务器运行参数（管理后台「设置 → 服务器与面板」可调） */
export interface ServerSettings {
  session_cache_ttl_sec: number
  session_seen_gap_sec: number
  session_cache_max: number
  min_compress_bytes: number
  snapshot_ttl_sec: number
  perf_sample_cap: number
  recent_req_cap: number
  overview_runs: number
  shutdown_wait_sec: number
  admin_poll_sec: number
  page_cache_max: number
  /** 评分合成全量重算的分块参数 */
  synth_chunk_size: number
  synth_chunk_timeout_sec: number
  synth_overall_timeout_sec: number
}

export interface AdminSettings {
  crawl: CrawlSettings
  cache: CacheSettings
  security: SecuritySettings
  network: NetworkSettings
  /** 服务器与面板运行参数 */
  server: ServerSettings
  /** 数据源启用/顺序/评分权重（键=来源标识） */
  sources: Record<string, SourceSetting>
  /** 数据源凭据（键=来源标识→字段键；回显为掩码） */
  credentials: Record<string, Record<string, string>>
  bgm: {
    token: string
    cookie: string
    ua: string
    token_configured: boolean
    cookie_configured: boolean
  }
  updated_at: string
}

/** 扩展源适配器的配置状态（管理面板「抓取」按源展示） */
export interface HarvesterInfo {
  source: string
  label: string
  types: number[]
  configured: boolean
  /** 设置里是否启用 */
  enabled?: boolean
  /** 有效优先级（越小越优先） */
  priority?: number
  /** 该源超过自己有效保留期的条目数（新鲜度状态，主行可见） */
  stale?: number
  /** 多源匹配度：该源行数与已并入多源作品组的行数（匹配率 = merged/rows） */
  match_rows?: number
  match_merged?: number
}

export interface AdminOverview {
  stats: Stats
  crawler: CrawlState
  runs: Run[]
  stale: number
  harvesters?: HarvesterInfo[]
  /** 最近一次覆盖全部启用中来源的合并运行开始时间（ISO；从未跑过为空） */
  last_all_sources?: string | null
  settings: {
    crawl: CrawlSettings
    bgm: { token_configured: boolean; cookie_configured: boolean }
  }
}

export interface MetaResponse {
  name: string
  version: string
  /** 提交哈希（本地裸构建为 none） */
  commit?: string
  /** 组合版本标识 version-commit；旧后端可能缺失，缺失时不做前后端一致性判定 */
  build?: string
  stats: Stats
  crawler: CrawlState
  /** 来源登记表公开清单（词表/徽标/外链模板/筛选 chips 的唯一来源） */
  sources?: ManifestSource[]
  /** 站点级分类词表（双层词表的外层；浏览 chips/首页类型卡按它渲染） */
  kinds?: ManifestKind[]
  /** 首页快捷入口（登记表 QuickLinks 下发；To 里 {year} = 去年年份） */
  quick_links?: { label: string; to: string }[]
}

// ---------- 管理端性能监测 ----------

// 单路由聚合：耗时/大小/状态码统计与链路拆分（DB / gzip / 其余=处理+编码）。
export interface RouteMetrics {
  route: string
  count: number
  errors: number
  avg_ms: number
  p95_ms: number
  max_ms: number
  avg_bytes: number
  avg_db_ms: number
  avg_gzip_ms: number
}

export interface MetricsRequest {
  at: string
  route: string
  status: number
  ms: number
  db_ms: number
  gzip_ms: number
  bytes: number
}

// 单类查询缓存的计数：hit=直接以缓存应答；origin=未命中回源；merged=单飞合并的并发重复请求。
export interface CacheKindStat {
  hits: number
  origin: number
  merged: number
}

export interface MetricsSnapshot {
  since: string
  uptime_s: number
  routes: RouteMetrics[]
  recent: MetricsRequest[]
  cache: Record<string, CacheKindStat>
}

// ---------- Bangumi 直连 ----------

export interface BgmImages {
  large?: string
  common?: string
  medium?: string
  small?: string
  grid?: string
}

export interface BgmRating {
  rank: number
  total: number
  score: number
  count: Record<string, number>
}

export interface BgmCollection {
  wish: number
  collect: number
  doing: number
  on_hold: number
  dropped: number
}

export interface BgmTag {
  name: string
  count: number
  total_count: number
}

export interface BgmInfoboxItem {
  key: string
  value: string | Array<string | { k?: string; v?: string }>
}

export interface BgmSubject {
  id: number
  type: number
  name: string
  name_cn: string
  date?: string | null
  platform?: string
  summary?: string
  images?: BgmImages
  image?: string
  nsfw?: boolean
  locked?: boolean
  meta_tags?: string[]
  tags?: BgmTag[]
  rating?: BgmRating
  collection?: BgmCollection
  infobox?: BgmInfoboxItem[]
  total_episodes?: number
  eps?: number
  volumes?: number
  series?: boolean
}

export interface BgmCharacter {
  id: number
  name: string
  type: number
  relation: string
  images?: BgmImages
  actors?: { id: number; name: string; type: number; images?: BgmImages }[]
}

export interface BgmPerson {
  id: number
  name: string
  type: number
  relation: string
  career?: string[]
  images?: BgmImages
}

export interface BgmRelation {
  id: number
  type: number
  name: string
  name_cn: string
  relation: string
  images?: BgmImages
  date?: string
}

export interface BgmEpisode {
  id: number
  type: number
  name: string
  name_cn: string
  sort: number
  ep?: number
  airdate?: string
  comment?: number
  duration?: string
}

export interface BgmEpisodeList {
  data: BgmEpisode[]
  total: number
  limit: number
  offset: number
}

export interface BgmUser {
  id: number
  username: string
  nickname: string
  avatar?: { large?: string; medium?: string; small?: string }
}

