// 数据通道：官方域名 + 镜像站。
//
// 安全底线：Authorization 只会发往 kind === 'official' 的地址。
// 镜像站（内置或用户添加）一律不带令牌，只能取公开数据。

export type EndpointKind = 'official' | 'mirror'

export interface Endpoint {
  id: string
  label: string
  apiBase: string
  imageBase?: string
  kind: EndpointKind
  builtin?: boolean
}

// 官方站点域名：bangumi.tv / bgm.tv / chii.in（以及 next.bgm.tv）。
// 实测只有 api.bgm.tv 提供 /v0 JSON API；其余域名返回站点页面或 404，
// 不能作为 API 通道，也不允许被添加成镜像。
export const OFFICIAL_SITE_HOSTS = ['bgm.tv', 'bangumi.tv', 'chii.in', 'next.bgm.tv'] as const

// 不要凭猜测往这里加域名，否则会把用户令牌发到不可信主机。
export const OFFICIAL_ENDPOINTS: ReadonlyArray<Endpoint> = [
  { id: 'official-bgm', label: '官方 api.bgm.tv', apiBase: 'https://api.bgm.tv', kind: 'official', builtin: true },
]

// 社区镜像：只接管公开数据；请求方不携带任何凭据。
export const BUILTIN_MIRRORS: ReadonlyArray<Endpoint> = [
  {
    id: 'mirror-anibt',
    label: '社区镜像 anibt.net',
    apiBase: 'https://bgmapi.anibt.net',
    imageBase: 'https://bgmimg.anibt.net',
    kind: 'mirror',
    builtin: true,
  },
]

export interface UserMirror {
  apiBase: string
  imageBase?: string
  label?: string
}

const MIRROR_KEY = 'anipick.mirrors.v1'
const ACTIVE_KEY = 'anipick.endpoint.v1'

function readJSON<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as T) : fallback
  } catch {
    return fallback
  }
}

function writeJSON(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    /* 隐私模式 */
  }
}

function normalizeBase(raw: string): string | null {
  const value = raw.trim()
  if (!value) return null
  try {
    const u = new URL(value)
    if (u.protocol !== 'https:') return null
    if (u.username || u.password) return null
    if (u.search || u.hash) return null
    const path = u.pathname.replace(/\/+$/, '')
    return u.origin + (path === '/' ? '' : path)
  } catch {
    return null
  }
}

function hostOf(apiBase: string): string {
  try {
    return new URL(apiBase).hostname.toLowerCase()
  } catch {
    return ''
  }
}

const OFFICIAL_HOSTS = new Set([
  ...OFFICIAL_ENDPOINTS.map(e => hostOf(e.apiBase)),
  ...OFFICIAL_SITE_HOSTS,
])

export function userMirrors(): UserMirror[] {
  const list = readJSON<UserMirror[]>(MIRROR_KEY, [])
  return Array.isArray(list) ? list.filter(m => m && typeof m.apiBase === 'string') : []
}

export function allMirrors(): Endpoint[] {
  const user = userMirrors()
    .map((m): Endpoint => ({
      id: `user:${m.apiBase}`,
      label: m.label?.trim() || hostOf(m.apiBase),
      apiBase: m.apiBase,
      imageBase: typeof m.imageBase === 'string' ? m.imageBase : undefined,
      kind: 'mirror',
      builtin: false,
    }))
  return [...BUILTIN_MIRRORS, ...user]
}

export function allEndpoints(): Endpoint[] {
  return [...OFFICIAL_ENDPOINTS, ...allMirrors()]
}

export function endpointById(id: string): Endpoint | undefined {
  return allEndpoints().find(e => e.id === id)
}

/** 返回值：错误信息；空字符串表示成功。 */
export function addUserMirror(apiBaseRaw: string, imageBaseRaw = ''): string {
  const apiBase = normalizeBase(apiBaseRaw)
  if (!apiBase) return '请输入有效的 HTTPS 地址（不能带账号、查询参数或锚点）'
  if (OFFICIAL_HOSTS.has(hostOf(apiBase))) return '官方域名不能添加为镜像（官方 API 只有 api.bgm.tv）'

  const imageBase = imageBaseRaw.trim() ? (normalizeBase(imageBaseRaw) ?? undefined) : undefined
  if (imageBaseRaw.trim() && !imageBase) return '图片镜像地址无效（必须是 HTTPS）'

  const exists = allMirrors().some(m => m.apiBase === apiBase)
  if (exists) return '该镜像已存在'

  const list = userMirrors()
  list.push({ apiBase, imageBase, label: hostOf(apiBase) })
  writeJSON(MIRROR_KEY, list)
  return ''
}

export function removeUserMirror(apiBase: string) {
  writeJSON(MIRROR_KEY, userMirrors().filter(m => m.apiBase !== apiBase))
  if (getActiveId() === `user:${apiBase}`) setActiveId('auto')
}

export function getActiveId(): string {
  try {
    return localStorage.getItem(ACTIVE_KEY) || 'auto'
  } catch {
    return 'auto'
  }
}

export function setActiveId(id: string) {
  try {
    if (id === 'auto') localStorage.removeItem(ACTIVE_KEY)
    else localStorage.setItem(ACTIVE_KEY, id)
  } catch {
    /* ignore */
  }
}

/**
 * 请求候选顺序：
 * - 需要鉴权（/v0/me）：只用官方；
 * - 手动选了某条通道：只走该通道（镜像不带令牌）；
 * - 自动 + 有令牌：官方优先，全部失败后用镜像公开数据；
 * - 自动 + 无令牌：镜像优先，失败后回退官方。
 */
export function endpointCandidates(_hasToken: boolean, authOnly: boolean): Endpoint[] {
  if (authOnly) return [...OFFICIAL_ENDPOINTS]

  const active = getActiveId()
  if (active !== 'auto') {
    const chosen = allEndpoints().find(e => e.id === active)
    if (chosen) return [chosen]
  }

  // 自动模式：官方优先（镜像只是官方不可用时的公开数据兜底）。
  return [...OFFICIAL_ENDPOINTS, ...allMirrors()]
}

// 记录最近一次成功的数据通道，供页面提示“当前来自镜像”。
let lastEndpoint: Endpoint | null = null
export function recordUsedEndpoint(ep: Endpoint) {
  lastEndpoint = ep
}
export function getLastUsedEndpoint(): Endpoint | null {
  return lastEndpoint
}

/**
 * 图片地址：自动/镜像模式下，把 lain.bgm.tv 的封面转到镜像图床；
 * 官方模式保持原地址。用户自定义镜像未填图片地址时不改写。
 */
export function imageBase(): string | undefined {
  const active = getActiveId()
  if (active === 'auto') return BUILTIN_MIRRORS[0]?.imageBase
  const ep = endpointById(active)
  if (!ep || ep.kind === 'official') return undefined
  return ep.imageBase
}
