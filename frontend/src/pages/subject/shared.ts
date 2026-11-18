import type {
  BgmCharacter,
  BgmEpisode,
  BgmPerson,
  BgmRelation,
  BgmSubject,
  Subject as IndexedSubject,
} from '../../api/types'

export type InfoValue =
  | string
  | { k?: string; v?: string }
  | Array<string | { k?: string; v?: string }>

export interface SubjectSnapshot {
  subjectID: number
  index: IndexedSubject | null
  subject: BgmSubject | null
  characters: BgmCharacter[]
  persons: BgmPerson[]
  relations: BgmRelation[]
  episodes: BgmEpisode[]
  viaMirror: boolean
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
