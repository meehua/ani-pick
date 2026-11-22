import type {
  BgmCharacter,
  BgmEpisode,
  BgmPerson,
  BgmRelation,
  BgmSubject,
  Subject as IndexedSubject,
} from '../../api/types'

/** 信息栏一行：k=名称，v=值（bgm 行浏览器直连拉取的动态字段） */
export interface ExtFact {
  k: string
  v: string
}

export type InfoValue =
  | string
  | { k?: string; v?: string }
  | Array<string | { k?: string; v?: string }>

/** 组内一个 bgm 行并行直连拉到的信息栏（融合表分节用）；
 *  扩展源行不再回上游——简介与链接都在本站索引行上。 */
export interface GroupExtra {
  facts: ExtFact[]
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
  /** 组内其他 bgm 行的动态信息栏（键=该行的公开 id；restore 保真） */
  groupExtras?: Record<string, GroupExtra>
}

// extRowToSubject 把本站索引行（库数据）映射进既有视图模型：
// SubjectHero / SubjectRail / 评分退化逻辑对两种来源共用。扩展源行的简介
// 在抓取时已随上游载荷入库，详情页不再回上游取任何数据。
export function extRowToSubject(row: IndexedSubject): BgmSubject {
  return {
    id: row.id,
    type: row.type,
    name: row.name,
    name_cn: row.name_cn,
    date: row.date,
    platform: row.platform,
    images: { large: row.image },
    image: row.image,
    summary: row.summary ?? '',
    // 大字评分用作品合成值（索引主行上物化）；各源原始分在「各源评分」行单独展示。
    // 评分分布（count）只有 bgm 有，恒空走「暂无评分」退化。
    rating: {
      rank: row.rank,
      total: row.rating_total,
      score: row.score,
      count: {},
    },
    infobox: [],
    tags: (row.tags ?? []).map(t => ({ name: t.name, count: t.votes, total_count: 0 })),
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
