import type {
  AdminOverview, AdminSettings, BgmCharacter, BgmEpisodeList, BgmPerson, BgmRelation,
  BgmSubject, BgmUser, CrawlState, ExtDetail, MetaResponse, MetricsSnapshot,
  Run, SearchResult, Subject, TagStat,
} from './types'
import { endpointCandidates, getActiveId, imageBase, recordUsedEndpoint, type Endpoint } from '../net/endpoints'

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public hint?: string,
  ) {
    super(message)
  }
}

async function toError(res: Response): Promise<ApiError> {
  let code = 'error'
  let message = `请求失败 (${res.status})`
  let hint: string | undefined
  try {
    const body = await res.json()
    if (body?.error) {
      code = body.error.code ?? code
      message = body.error.message ?? message
      hint = body.error.hint
    }
  } catch {
    /* 非 JSON 错误体 */
  }
  return new ApiError(res.status, code, message, hint)
}

async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, { signal, credentials: 'same-origin' })
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e
    throw new ApiError(0, 'network', '无法连接 AniPick 服务', '请确认后端已启动')
  }
  if (!res.ok) throw await toError(res)
  return res.json() as Promise<T>
}

async function apiSend<T>(path: string, method: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e
    throw new ApiError(0, 'network', '无法连接 AniPick 服务', '请确认后端已启动')
  }
  if (!res.ok) throw await toError(res)
  return res.json() as Promise<T>
}

// 图片由浏览器直接加载；自动/镜像模式下把 lain.bgm.tv 换成镜像图床。
export const imgOriginal = (url?: string): string => (url ? url.replace(/^http:\/\//, 'https://') : '')

export const img = (url?: string): string => {
  const source = imgOriginal(url)
  if (!source) return ''
  const base = imageBase()
  if (base && source.startsWith('https://lain.bgm.tv/')) {
    return base.replace(/\/+$/, '') + source.slice('https://lain.bgm.tv'.length)
  }
  return source
}

export interface SearchParams {
  keyword?: string
  /** 数据来源筛选（bgm/tmdb/itunes/mal/anilist）；缺省 = 全部来源 */
  source?: string
  types?: number[]
  tags?: string[]
  exclude?: string[]
  tag_mode?: string
  pct_min?: number
  meta_tags?: string[]
  date_from?: string
  date_to?: string
  rank_min?: number
  rank_max?: number
  score_min?: number
  score_max?: number
  rating_min?: number
  nsfw?: string
  sort?: string
  order?: string
  page?: number
  page_size?: number
}

export function searchSubjects(params: SearchParams, signal?: AbortSignal): Promise<SearchResult> {
  const q = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '' || value === null) continue
    if (Array.isArray(value)) {
      if (value.length === 0) continue
      q.set(key, value.join(','))
    } else if (typeof value === 'number') {
      if (value === 0 && key !== 'rank_min' && key !== 'rank_max') continue
      q.set(key, String(value))
    } else {
      q.set(key, value)
    }
  }
  return apiGet<SearchResult>(`/api/subjects?${q.toString()}`, signal)
}

export const getMeta = () => apiGet<MetaResponse>('/api/meta')
export const getSubjectIndex = (id: number, signal?: AbortSignal) =>
  apiGet<Subject>(`/api/subjects/${id}`, signal)
// 扩展源详情代理：上游 key 与限速留在服务端，bgm 条目不走此端点
export const getSubjectExtDetail = (id: number, signal?: AbortSignal) =>
  apiGet<ExtDetail>(`/api/subjects/${id}/detail`, signal)
export const getTags = (q: string, types: number[], limit = 30, signal?: AbortSignal) => {
  const p = new URLSearchParams({ q, limit: String(limit) })
  if (types.length) p.set('types', types.join(','))
  return apiGet<{ items: TagStat[] }>(`/api/tags?${p.toString()}`, signal)
}
export const getMetaTags = (type: number, signal?: AbortSignal) =>
  apiGet<{ items: TagStat[] }>(
    `/api/meta-tags?type=${type}&limit=60`,
    signal,
  )

// ---------- 管理员 ----------

export const adminLogin = (password: string) =>
  apiSend<{ ok: boolean }>('/api/admin/login', 'POST', { password })
export const adminLogout = () => apiSend<{ ok: boolean }>('/api/admin/logout', 'POST', {})
export const adminSession = () => apiGet<{ authenticated: boolean }>('/api/admin/session')
export const adminOverview = () => apiGet<AdminOverview>('/api/admin/overview')
export const adminGetSettings = () => apiGet<AdminSettings>('/api/admin/settings')

export interface RunsPage {
  items: Run[]
  total: number
  limit: number
  offset: number
}
// 任务历史：筛选（来源/模式/状态）+ 排序（started_at|mode|source|status）+ 分页
export const adminRuns = (params: {
  source?: string
  mode?: string
  status?: string
  sort?: string
  order?: string
  limit?: number
  offset?: number
}) => {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === '' || v === 0) continue
    q.set(k, String(v))
  }
  return apiGet<RunsPage>(`/api/admin/runs?${q}`)
}

export interface SettingsPayload {
  /** 不提交 = 保持现有（各面板只负责自己的块；凭据单独显式保存） */
  crawl?: AdminSettings['crawl']
  cache?: AdminSettings['cache']
  security?: AdminSettings['security']
  /** 服务器与面板运行参数（设置页可调） */
  server?: AdminSettings['server']
  /** 抓取出口网络（抓取设置 → 网络可调） */
  network?: AdminSettings['network']
  /** 数据源启用/顺序/评分权重（键=来源标识；清单之外的键后端拒绝） */
  sources?: AdminSettings['sources']
  /** 数据源凭据（只发送新填的值；空值/掩码回显 = 保持现有配置） */
  credentials?: AdminSettings['credentials']
  bgm?: { token?: string; cookie?: string; clear_token?: boolean; clear_cookie?: boolean }
}

export const adminSaveSettings = (payload: SettingsPayload) =>
  apiSend<{ ok: boolean; updated_at: string }>('/api/admin/settings', 'PUT', payload)
// 按源校验凭据：扩展源带草稿值（还没保存也能试），不带 = 校验已存配置；
// 不带 source = bgm（站长凭据，沿用旧行为）
export const adminTestCredential = (source?: string, credentials?: Record<string, string>) =>
  apiSend<{ ok: boolean; source?: string; mode?: string }>(
    '/api/admin/credential/test', 'POST', { source, credentials },
  )
// 抓取模式（各源支持哪些模式由来源登记表声明，经 manifest 下发）
export type CrawlMode = 'full' | 'incremental' | 'refresh' | 'harvest' | 'harvest_incremental'
// source 传入时按单源抓取执行（模式须在该源声明的能力内），否则按合并模式跑全部来源
export const adminStartCrawl = (
  mode: CrawlMode,
  source?: string,
) => apiSend<{ run_id: number; status: CrawlState }>('/api/admin/crawl', 'POST', { mode, source })
export const adminCancelCrawl = () => apiSend<{ ok: boolean }>('/api/admin/crawl/cancel', 'POST', {})
export const adminRefreshSubject = (id: number) =>
  apiSend<Subject>(`/api/admin/subjects/${id}/refresh`, 'POST', {})
// 重跑作品匹配：对仍自成作品的扩展源行重新归组（先补 anilist 的 mal_id 键）
export const adminRematchWorks = () =>
  apiSend<{ scanned: number; already_in: number; merged: number; independent: number; ambiguous: number; group_moves: number; samples?: string[] }>(
    '/api/admin/works/rematch',
    'POST',
    {},
  )
export const adminDeleteSubject = (id: number) =>
  apiSend<{ ok: boolean }>(`/api/admin/subjects/${id}`, 'DELETE')
export const adminChangePassword = (current: string, next: string) =>
  apiSend<{ ok: boolean }>('/api/admin/password', 'POST', { current, next })

export const adminSearchSubjects = (params: SearchParams) => {
  const q = new URLSearchParams()
  if (params.keyword) q.set('keyword', params.keyword)
  if (params.types?.length) q.set('types', params.types.join(','))
  q.set('page', String(params.page ?? 1))
  q.set('page_size', String(params.page_size ?? 20))
  if (params.sort) q.set('sort', params.sort)
  return apiGet<SearchResult>(`/api/admin/subjects?${q.toString()}`)
}

export const adminMetrics = () => apiGet<MetricsSnapshot>('/api/admin/metrics')
export const adminMetricsReset = () =>
  apiSend<{ ok: boolean }>('/api/admin/metrics/reset', 'POST', {})

// ---------- 用户登录（凭据只在本机；详情由浏览器直接请求） ----------

const OFFICIAL_TIMEOUT_MS = 10000
const MIRROR_TIMEOUT_MS = 20000

function timeoutSignal(signal: AbortSignal | undefined, ms: number): AbortSignal | undefined {
  const anyAbort = (AbortSignal as unknown as { any?: (s: AbortSignal[]) => AbortSignal }).any
  const timeout = (AbortSignal as unknown as { timeout?: (ms: number) => AbortSignal }).timeout?.(ms)
  if (signal && timeout && anyAbort) return anyAbort([signal, timeout])
  return signal ?? timeout
}

let circuitOpenUntil = 0
const endpointDownUntil = new Map<string, number>()

export function resetBgmCircuit() {
  circuitOpenUntil = 0
  endpointDownUntil.clear()
}

async function requestFrom<T>(
  endpoint: Endpoint,
  path: string,
  token: string | undefined,
  signal: AbortSignal | undefined,
): Promise<T> {
  const headers: Record<string, string> = {}
  // 安全底线：令牌只发往官方域名；镜像站一律匿名。
  if (token && endpoint.kind === 'official') {
    headers.Authorization = `Bearer ${token}`
  }
  const res = await fetch(endpoint.apiBase + path, {
    signal: timeoutSignal(signal, endpoint.kind === 'mirror' ? MIRROR_TIMEOUT_MS : OFFICIAL_TIMEOUT_MS),
    credentials: 'omit',
    cache: 'no-store',
    headers: Object.keys(headers).length > 0 ? headers : undefined,
  })
  if (!res.ok) {
    if (res.status === 404) {
      throw new ApiError(404, 'bgm_not_found', '条目不存在，或当前通道无权查看',
        '敏感内容需要在官方域名登录后查看；镜像站只提供公开数据')
    }
    if (res.status === 401 || res.status === 403) {
      throw new ApiError(res.status, 'bgm_unauthorized', 'Bangumi 凭据无效或已过期', '请重新登录')
    }
    throw await toError(res)
  }
  return (await res.json()) as T
}

// 浏览器直连：按官方 / 镜像站顺序取公开数据；令牌只可能出现在官方请求里。
async function bgmGet<T>(
  path: string,
  token?: string,
  signal?: AbortSignal,
  opts?: { authOnly?: boolean },
): Promise<T> {
  if (Date.now() < circuitOpenUntil) {
    throw new ApiError(
      0,
      'bgm_offline',
      opts?.authOnly ? '官方域名暂不可达' : '当前没有可用的 Bangumi 通道',
      '短时间内不再重试；请检查网络或在登录框切换数据通道后重试',
    )
  }

  const candidates = endpointCandidates(!!token, !!opts?.authOnly)
  const active = getActiveId()
  let lastErr: unknown
  for (const endpoint of candidates) {
    // 手动选中的通道不跳过；其余通道在失败后短暂降权，避免每次等超时。
    if (active === 'auto' && (endpointDownUntil.get(endpoint.apiBase) ?? 0) > Date.now()) continue
    try {
      const data = await requestFrom<T>(endpoint, path, token, signal)
      recordUsedEndpoint(endpoint)
      circuitOpenUntil = 0
      endpointDownUntil.delete(endpoint.apiBase)
      return data
    } catch (e) {
      if ((e as Error).name === 'AbortError') throw e
      // 内容类错误（401/403/404/4xx）直接上抛，不再换通道重试。
      if (e instanceof ApiError && e.status !== 0 && e.status < 500) throw e
      endpointDownUntil.set(endpoint.apiBase, Date.now() + 120_000)
      lastErr = e
    }
  }

  circuitOpenUntil = Date.now() + 60_000
  if (lastErr instanceof ApiError) throw lastErr
  throw new ApiError(
    0,
    'bgm_network',
    opts?.authOnly ? '官方域名不可达，无法校验登录凭据' : '无法连接 Bangumi（官方与镜像均失败）',
    '请检查网络，或在登录框选择可用的镜像站；令牌只会发送给官方域名',
  )
}

export const bgmMe = (token: string, signal?: AbortSignal) =>
  bgmGet<BgmUser>('/v0/me', token, signal, { authOnly: true })
export const bgmSubject = (id: number, token?: string, signal?: AbortSignal) =>
  bgmGet<BgmSubject>(`/v0/subjects/${id}`, token, signal)
export const bgmCharacters = (id: number, token?: string, signal?: AbortSignal) =>
  bgmGet<BgmCharacter[]>(`/v0/subjects/${id}/characters`, token, signal)
export const bgmPersons = (id: number, token?: string, signal?: AbortSignal) =>
  bgmGet<BgmPerson[]>(`/v0/subjects/${id}/persons`, token, signal)
export const bgmRelations = (id: number, token?: string, signal?: AbortSignal) =>
  bgmGet<BgmRelation[]>(`/v0/subjects/${id}/subjects`, token, signal)
export const bgmEpisodes = (id: number, token?: string, signal?: AbortSignal) =>
  bgmGet<BgmEpisodeList>(`/v0/episodes?subject_id=${id}&limit=100`, token, signal)

