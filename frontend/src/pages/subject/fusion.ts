// 多源信息融合：字段融合表 / 各源评分归组 / 简介折叠归并的纯函数层。
//
// 内核通用性不变式（scripts/fusion-test.mjs 与 check-layers 门禁钉住）：
//   - 融合依据只有一样东西——组内行的排列顺序（后端按管理员设置的有效
//     优先序给出，与主行选择同一份序）；先到先得，后续来源的同名字段
//     同值静默去重、异值并陈并标注来源；
//   - 不存在任何「基线来源」：删掉组内任意一行，剩余行按同一规则重建；
//   - 本文件零来源字面量、零来源分支——调用方给什么行就融什么行。
import type { SubjectSourceRow } from '../../api/types'

// 判重归一化与后端合并器 internal/store/workmatch.go 的 NormalizeTitle
// 保持同一实现语义：NFKC 折叠全角/半角 → 小写 → 去掉空白/控制/标点/符号
// 字符，只留字母与数字（含各文字系统）。前后端各一份、语义一致，改动必须同步。
export function normText(s: string): string {
  return (s ?? '')
    .normalize('NFKC')
    .toLowerCase()
    .replace(/[\s\p{Z}\p{C}\p{P}\p{S}]/gu, '')
}

// variantOf 同一来源组内多行（如剧场版/TV 各一条）时的区分标注：
// 平台优先，其次年份。
export function variantOf(row: Pick<SubjectSourceRow, 'platform' | 'date'>): string {
  if (row.platform) return row.platform
  if (row.date) return row.date.slice(0, 4)
  return ''
}

/** 融合表输入：组内一行（任意来源）自己的字段；variant 区分同源多行。 */
export interface FusionRow {
  source: string
  variant?: string
  facts: { k: string; v: string }[]
}

/** 融合后某字段行里的一个值：来源 + 可选变体标注。 */
export interface FusedEntry {
  source: string
  variant?: string
  value: string
}

/** 融合后的一个字段行：entries[0] 是顺序最先提供该字段的值（先到先得），
 *  其余为异值并陈（渲染时才标注来源徽标）。 */
export interface FusedFactRow {
  key: string
  entries: FusedEntry[]
}

// buildFusedFacts 按给定行序把各来源的字段融合成一张表：
//   - 某字段按顺序第一个提供它的来源给出展示值（先到先得）；
//   - 后续来源的同名字段：归一化同值 → 静默去重不渲染；
//     异值 → 追加进该字段行（渲染时标注来源）；
//   - 没有任何行被赋予基线地位——移除任意一行，剩余行按同一规则重建。
export function buildFusedFacts(rows: FusionRow[]): FusedFactRow[] {
  const byKey = new Map<string, { entries: FusedEntry[]; norms: Set<string> }>()
  const out: FusedFactRow[] = []
  for (const row of rows) {
    for (const fact of row.facts) {
      const key = (fact.k ?? '').trim()
      const value = (fact.v ?? '').trim()
      const norm = normText(value)
      if (!key || !norm) continue
      const nk = normText(key)
      let bucket = byKey.get(nk)
      if (!bucket) {
        bucket = { entries: [], norms: new Set() }
        byKey.set(nk, bucket)
        out.push({ key, entries: bucket.entries })
      }
      if (bucket.norms.has(norm)) continue // 同字段同值：静默去重
      bucket.norms.add(norm)
      bucket.entries.push({ source: row.source, variant: row.variant, value })
    }
  }
  return out
}

/** 各源评分归组后的一行：同源多行时用 variant 区分（平台/年份）。 */
export interface ScoreVariant {
  id: number
  url: string
  score: number
  ratingTotal: number
  /** 该行的评分分布（1~10 分十档人数；空 = 该源无分档数据） */
  dist?: number[]
  title: string
  variant: string
}

export interface ScoreGroup {
  source: string
  rows: ScoreVariant[]
}

// groupScoreRows 把作品组内全部来源行按来源归组：来源标只出现一次，
// 同源多行并列展示各自的评分；组内单行不需要区分标注。行序即输入序
//（后端按设置优先序给出）。
export function groupScoreRows(sources: SubjectSourceRow[]): ScoreGroup[] {
  const groups = new Map<string, ScoreVariant[]>()
  for (const row of sources) {
    const list = groups.get(row.source) ?? []
    list.push({
      id: row.id,
      url: row.url,
      score: row.score,
      ratingTotal: row.rating_total,
      dist: row.dist,
      title: row.name_cn || row.name || '',
      variant: variantOf(row),
    })
    groups.set(row.source, list)
  }
  return [...groups.entries()].map(([source, rows]) => {
    if (rows.length === 1) rows[0].variant = '' // 组内单行不需要区分标注
    return { source, rows }
  })
}

export interface SummaryRow {
  source: string
  variant?: string
  text: string
}

export interface SummaryGroup {
  source: string
  /** 同来源多条简介时按变体分段标注；单条时不标 */
  segments: { variant?: string; text: string }[]
}

// mergeSummaries 把组内其他来源的简介按来源归并：同来源多条并进同一个
// 折叠、段内标注变体；与主简介或先前来源完全相同的简介只留一条。
export function mergeSummaries(mainSummary: string, rows: SummaryRow[]): SummaryGroup[] {
  const seen = new Set<string>()
  const mainNorm = normText(mainSummary)
  if (mainNorm) seen.add(mainNorm)
  const groups = new Map<string, { source: string; segments: { variant?: string; text: string }[] }>()
  for (const row of rows) {
    const norm = normText(row.text)
    if (!norm || seen.has(norm)) continue
    seen.add(norm)
    let group = groups.get(row.source)
    if (!group) {
      group = { source: row.source, segments: [] }
      groups.set(row.source, group)
    }
    group.segments.push({ variant: row.variant, text: row.text })
  }
  return [...groups.values()].map(g => ({
    source: g.source,
    // 单条简介不需要变体标注（与主简介相同的行已在上面去重）
    segments:
      g.segments.length > 1
        ? g.segments
        : g.segments.map(s => ({ text: s.text })),
  }))
}
