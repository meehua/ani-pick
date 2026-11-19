// 本站索引（AniPick 后端）与 Bangumi 直连 API 的类型。

export const TYPE_LABEL: Record<number, string> = {
  1: '书籍',
  2: '动画',
  3: '音乐',
  4: '游戏',
  6: '三次元',
}

export const TYPE_ORDER = [2, 1, 4, 3, 6]

export interface TagVote {
  name: string
  votes: number
  pct: number
}

export interface Subject {
  id: number
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
  tags_total: number
  locked: boolean
  fetched_at: string
  tags?: TagVote[]
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
  tags: number
  subject_tags: number
  last_fetched: string | null
  nsfw: number
  db_size: number
}

export interface CrawlProgress {
  phase: string
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
  trigger: string
  status: string
  types: number[]
  stats: CrawlProgress | Record<string, unknown>
  error: string
  started_at: string
  finished_at: string | null
}

export interface CrawlSettings {
  types: number[]
  page_size: number
  delay_ms: number
  request_timeout_sec: number
  refresh_ttl_days: number
  refresh_batch: number
  incremental_pages: number
  incremental_interval_min: number
  auto_incremental: boolean
  auto_refresh: boolean
  prune_on_full: boolean
}

export interface CacheSettings {
  query_ttl: number
  public_ttl: number
  meta_ttl: number
  meta_tags_ttl: number
  overview_ttl: number
  max_keys: number
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
}

export interface AdminSettings {
  crawl: CrawlSettings
  cache: CacheSettings
  security: SecuritySettings
  bgm: {
    token: string
    cookie: string
    ua: string
    token_configured: boolean
    cookie_configured: boolean
  }
  updated_at: string
}

export interface AdminOverview {
  stats: Stats
  crawler: CrawlState
  runs: Run[]
  stale: number
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
}

// 服务端抓取出口的诊断信息（仅管理员可见）。
export interface NetworkStatus {
  doh: boolean
  proxy: boolean
  upstream_ok?: boolean
  upstream_status?: number
  upstream_error?: string
  diagnostics?: {
    host: string
    doh_enabled: boolean
    endpoints: string[]
    addresses: string[]
    error?: string
  }
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
