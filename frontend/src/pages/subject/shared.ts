import type {
  BgmCharacter,
  BgmEpisode,
  BgmPerson,
  BgmRelation,
  BgmSubject,
  ExtDetail,
  ExtFact,
  Subject as IndexedSubject,
} from '../../api/types'

export type InfoValue =
  | string
  | { k?: string; v?: string }
  | Array<string | { k?: string; v?: string }>

/** 组内一个扩展源行并行拉到的归一化详情（信息栏分节 + 简介折叠用）。 */
export interface GroupExtra {
  facts: ExtFact[]
  summary?: string
}

export interface SubjectSnapshot {
  subjectID: number
  index: IndexedSubject | null
  subject: BgmSubject | null
  characters: BgmCharacter[]
  persons: BgmPerson[]
  relations: BgmRelation[]
  episodes: BgmEpisode[]
  viaMirror: boolean
  /** 扩展源详情的官方外链（restore 时无需重新请求代理） */
  extLinkHref?: string | null
  /** 主条目缺简介时从组内其他来源补的简介（restore 保真） */
  altSummary?: { text: string; source: string } | null
  /** 组内其他扩展源的详情（键=该行的公开 id；restore 保真） */
  groupExtras?: Record<string, GroupExtra>
}

// extToBgmSubject 把服务端归一化的扩展源详情映射进既有视图模型：
// 字段重命名收在这一处（真正的上游→归一化映射在后端 detail 代理完成），
// SubjectHero / SubjectRail / 评分退化逻辑对两种来源共用。
export function extToBgmSubject(d: ExtDetail, index: IndexedSubject | null): BgmSubject {
  return {
    id: index?.id ?? 0,
    type: index?.type ?? 1,
    name: d.title,
    name_cn: d.title_cn ?? '',
    date: d.date,
    platform: d.platform ?? index?.platform ?? '',
    images: { large: d.image },
    image: d.image ?? '',
    summary: d.summary,
    // 大字评分用作品合成值（索引主行上物化）；各源原始分在「各源评分」行单独展示。
    // 评分分布（count）只有 bgm 有，恒空走「暂无评分」退化。
    rating: {
      rank: 0,
      total: index?.rating_total ?? d.score_count ?? 0,
      score: index?.score ?? d.score ?? 0,
      count: {},
    },
    infobox: (d.facts ?? []).map(f => ({ key: f.k, value: f.v })),
    tags: (d.tags ?? []).map(name => ({ name, count: 0, total_count: 0 })),
  }
}

export function infoValue(value: InfoValue): string {
  if (Array.isArray(value)) return value.map(infoValue).filter(Boolean).join(' / ')
  if (value && typeof value === 'object') {
    const obj = value as { v?: string; k?: string }
    return obj.v ?? obj.k ?? ''
  }
  return value == null ? '' : String(value)
}

export function titleOf(s: { name_cn?: string; name: string }) {
  return s.name_cn || s.name
}
