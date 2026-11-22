// 词表（后端 internal/registry）经 /api/meta 下发的 manifest 在前端的
// 唯一消费入口。双层词表：源原生层（各源自己的类型标签，站点不改名）与
// 站点分类层（kind：浏览筛选/首页类型卡）。来源徽标文案、抓取能力、展示顺序、
// 详情分派策略全部由 manifest 驱动——这里不自带任何硬编码词表；
// manifest 未到达时按旧约定退化。

import type { ManifestKind, ManifestSource } from './types'
import { MODE_LABEL } from '../lib/format'

// 首页快捷入口（登记表 QuickLinks 经 manifest 下发；{year} 占位符替换为去年年份）
let quickLinkList: { label: string; to: string }[] = []

/** 装入快捷入口（meta 重放时调用）。 */
export function applyQuickLinks(list?: { label: string; to: string }[]): void {
  quickLinkList = Array.isArray(list) ? list : []
}

/** 首页快捷入口（To 里的 {year} 已替换为去年年份）；未到达时返回空。 */
export function quickLinks(): { label: string; to: string }[] {
  const year = new Date().getFullYear() - 1
  return quickLinkList
    .filter(q => q && q.label && q.to)
    .map(q => ({ label: q.label, to: q.to.replace('{year}', String(year)) }))
}

let manifest: ManifestSource[] = []
let byId = new Map<string, ManifestSource>()
let kinds: ManifestKind[] = []
let typeOrderSeq: number[] = []
let typeLabelMap = new Map<number, string>()
let typeKindMap = new Map<number, string>()
let kindSeq: string[] = []

/** 把 /api/meta 的词表装入（每次 meta 刷新都会重放）。 */
export function applyManifest(list?: ManifestSource[], kindList?: ManifestKind[]): void {
  manifest = Array.isArray(list) ? list : []
  byId = new Map(manifest.map(s => [s.id, s]))
  kinds = Array.isArray(kindList) ? kindList : []
  typeOrderSeq = []
  typeLabelMap = new Map()
  typeKindMap = new Map()
  for (const s of manifest) {
    for (const t of s.types) {
      if (!typeLabelMap.has(t.code)) typeLabelMap.set(t.code, t.label) // 多源同码同义，取第一份
      if (!typeKindMap.has(t.code) && t.kind) typeKindMap.set(t.code, t.kind)
      typeOrderSeq.push(t.code)
    }
  }
  kindSeq = kinds.map(k => k.kind)
}

/** 全部启用中的来源（已按有效优先级排序）。 */
export function manifestSources(): ManifestSource[] {
  return manifest
}

/** 原生索引来源（浏览页 chips 常驻、无 source 字段的旧行归属）。 */
export function nativeSource(): ManifestSource | undefined {
  return manifest.find(s => s.native)
}

export function sourceMeta(id?: string): ManifestSource | undefined {
  return id ? byId.get(id) : undefined
}

/** 来源展示名；未登记/未加载时原样返回标识。 */
export function sourceLabel(id?: string): string {
  const s = id ? byId.get(id) : nativeSource()
  return s?.label ?? (id ?? '')
}

/** 详情获取策略：登记表声明 proxy=服务端代理 / direct=浏览器直连。
 *  无 source 字段的旧行视为原生（直连）；manifest 未到达的瞬态里按
 *  旧约定退化——原生索引标识直连，其余来源按扩展源处理。 */
function sourceDetail(id?: string): 'proxy' | 'direct' {
  if (manifest.length === 0) {
    return !id || id === 'bgm' ? 'direct' : 'proxy'
  }
  const s = id ? byId.get(id) : nativeSource()
  return s?.detail ?? 'direct'
}

/** 是否扩展源（详情走服务端代理、外链与评分行分派）。 */
export function isExtSource(id?: string): boolean {
  return sourceDetail(id) === 'proxy'
}

// ---------- 源原生层（各源自己的类型标签，永不改名） ----------

/** 类型中文标签；给了来源时优先用该源的原生标签（双层词表：同一编码
 *  在不同源可能有不同叫法，如 6 在 bgm 叫「三次元」）。未登记的编码返回
 *  原值（数字转字符串）。 */
export function typeLabel(code: number, source?: string): string {
  const s = source ? byId.get(source) : undefined
  const own = s?.types.find(t => t.code === code)
  if (own) return own.label
  return typeLabelMap.get(code) ?? String(code)
}

/** 全站原生类型展示顺序（登记表 TypeOrder 经 manifest 下发）。 */
export function typeOrder(): number[] {
  return typeOrderSeq
}

/** 某来源声明的抓取模式（抓取设置页按钮组按此渲染；manifest 下发的
 *  {mode,label,hint}，未加载时退化为空）。 */
export function sourceModes(id?: string) {
  return sourceMeta(id)?.modes ?? []
}

/** 抓取模式展示名：优先用该源 manifest 里声明的 label，未到达时用本地兜底。 */
export function modeLabel(mode: string, source?: string): string {
  const m = sourceModes(source).find(x => x.mode === mode)
  return m?.label ?? MODE_LABEL[mode] ?? mode
}

/** 类型的站点分类；未声明返回 undefined。 */
export function kindOf(code: number): string | undefined {
  return typeKindMap.get(code)
}

/** 某分类的中文标签；未登记返回空串（调用方自行退化）。 */
export function kindLabel(kind: string): string {
  return kinds.find(k => k.kind === kind)?.label ?? kind
}

/** 某分类的副文案；未登记返回空串。 */
export function kindHint(kind: string): string {
  return kinds.find(k => k.kind === kind)?.hint ?? ''
}

/** 属于某分类的全部类型编码（跨源并集，按全站类型序去重）。 */
export function codesOfKind(kind: string): number[] {
  const set = new Set<number>()
  for (const t of typeOrderSeq) {
    if (typeKindMap.get(t) === kind) set.add(t)
  }
  return [...set]
}

// ---------- 站点分类层（kind：浏览筛选/首页类型卡/筛选摘要） ----------

/** 站点分类的条目数：把 by_type 的原生类型计数聚合到 kind 层（多源同 kind
 *  求和）。统计未取到时全部分类都出现、计数留空（调用方显示占位符）；
 *  取到时只列计数 > 0 的分类（顺序照词表）。 */
export function kindCounts(
  byType?: Record<string, number>,
): Array<{ kind: string; count?: number }> {
  if (kindSeq.length === 0) return []
  if (!byType) return kindSeq.map(k => ({ kind: k }))
  const totals = new Map<string, number>()
  for (const [codeStr, n] of Object.entries(byType)) {
    if (!(n > 0)) continue
    const k = typeKindMap.get(Number(codeStr))
    if (!k) continue
    totals.set(k, (totals.get(k) ?? 0) + n)
  }
  return kindSeq
    .filter(k => (totals.get(k) ?? 0) > 0)
    .map(k => ({ kind: k, count: totals.get(k) }))
}
