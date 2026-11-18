import * as React from 'react'
import { useLocation, useParams } from 'react-router-dom'
import { BackLink, ErrorNote, Notice } from '@/components/ui-kit'
import { useAuthDialog } from '@/components/auth-dialog-provider'
import {
  ApiError,
  bgmCharacters,
  bgmEpisodes,
  bgmPersons,
  bgmRelations,
  bgmSubject,
  getSubjectIndex,
  resetBgmCircuit,
} from '../../api/client'
import type {
  BgmCharacter,
  BgmEpisode,
  BgmPerson,
  BgmRelation,
  BgmSubject,
  Subject as IndexedSubject,
  TagVote,
} from '../../api/types'
import { useCredential } from '../../lib/credential'
import { getLastUsedEndpoint } from '../../net/endpoints'
import { loadPage, savePage } from '../../lib/page-cache'
import { useScrollMemory } from '../../lib/use-scroll-memory'
import { type SubjectSnapshot } from './shared'
import { SubjectFallback } from './SubjectFallback'
import { SubjectHero } from './SubjectHero'
import { SubjectRail } from './SubjectRail'
import { SubjectSkeleton } from './SubjectSkeleton'
import { SubjectTabs } from './SubjectTabs'

export default function SubjectPage() {
  const { id } = useParams()
  const location = useLocation()
  const subjectID = Number(id)
  const credential = useCredential()
  const token = credential?.token
  const { openLogin } = useAuthDialog()
  const [reload, setReload] = React.useState(0)
  const [summaryOpen, setSummaryOpen] = React.useState(false)
  const [allInfo, setAllInfo] = React.useState(false)
  const [tab, setTab] = React.useState<string | null>(null)
  const [viaMirror, setViaMirror] = React.useState(false)

  const mountHit = React.useMemo(() => loadPage<SubjectSnapshot>(location.key), []) // eslint-disable-line react-hooks/exhaustive-deps
  const [index, setIndex] = React.useState<IndexedSubject | null>(mountHit?.data.index ?? null)
  const [subject, setSubject] = React.useState<BgmSubject | null>(mountHit?.data.subject ?? null)
  const [characters, setCharacters] = React.useState<BgmCharacter[]>(mountHit?.data.characters ?? [])
  const [persons, setPersons] = React.useState<BgmPerson[]>(mountHit?.data.persons ?? [])
  const [relations, setRelations] = React.useState<BgmRelation[]>(mountHit?.data.relations ?? [])
  const [episodes, setEpisodes] = React.useState<BgmEpisode[]>(mountHit?.data.episodes ?? [])
  const [loading, setLoading] = React.useState(!mountHit?.data.subject)
  const [error, setError] = React.useState<unknown>(null)
  const restoreRef = React.useRef<SubjectSnapshot | null>(
    mountHit && mountHit.data.subjectID === subjectID ? mountHit.data : null,
  )

  useScrollMemory(location.key, mountHit?.scrollTop ?? null)

  React.useEffect(() => {
    if (!Number.isFinite(subjectID) || subjectID <= 0) return

    if (restoreRef.current) {
      const snap = restoreRef.current
      restoreRef.current = null
      setIndex(snap.index)
      setSubject(snap.subject)
      setCharacters(snap.characters)
      setPersons(snap.persons)
      setRelations(snap.relations)
      setEpisodes(snap.episodes)
      setViaMirror(snap.viaMirror)
      setLoading(false)
      return
    }

    let alive = true
    const ctrl = new AbortController()
    setLoading(true)
    setError(null)
    setSubject(null)
    setSummaryOpen(false)
    setAllInfo(false)

    getSubjectIndex(subjectID, ctrl.signal).then(setIndex).catch(() => setIndex(null))

    bgmSubject(subjectID, token, ctrl.signal)
      .then(sub => {
        if (!alive) return
        setSubject(sub)
        setViaMirror(getLastUsedEndpoint()?.kind === 'mirror')
        setLoading(false)
        void Promise.allSettled([
          bgmCharacters(subjectID, token, ctrl.signal),
          bgmPersons(subjectID, token, ctrl.signal),
          bgmRelations(subjectID, token, ctrl.signal),
          bgmEpisodes(subjectID, token, ctrl.signal),
        ]).then(([c, p, r, e]) => {
          if (!alive) return
          if (c.status === 'fulfilled') setCharacters(c.value)
          if (p.status === 'fulfilled') setPersons(p.value)
          if (r.status === 'fulfilled') setRelations(r.value)
          if (e.status === 'fulfilled') setEpisodes(e.value.data ?? [])
        })
      })
      .catch(err => {
        if (!alive || (err as Error).name === 'AbortError') return
        setError(err)
        setLoading(false)
      })

    return () => {
      alive = false
      ctrl.abort()
    }
  }, [subjectID, token, reload]) // eslint-disable-line react-hooks/exhaustive-deps

  React.useEffect(() => {
    if (!loading && subject) {
      savePage(location.key, {
        subjectID,
        index,
        subject,
        characters,
        persons,
        relations,
        episodes,
        viaMirror,
      } satisfies SubjectSnapshot)
    }
  }, [location.key, loading, subject, index, characters, persons, relations, episodes, viaMirror, subjectID])

  if (!Number.isFinite(subjectID) || subjectID <= 0) {
    return <ErrorNote error={new ApiError(400, 'bad_id', '无效的条目 ID')} />
  }

  if (!subject && !index && loading) {
    return <SubjectSkeleton />
  }

  // 上游详情不可用：降级展示本站索引。
  if (!subject) {
    return (
      <SubjectFallback
        index={index}
        loading={loading}
        error={error}
        onLogin={openLogin}
        onRetry={() => {
          resetBgmCircuit()
          setReload(v => v + 1)
        }}
      />
    )
  }

  const tags: TagVote[] =
    index?.tags?.length
      ? index.tags
      : (subject.tags ?? []).map(t => {
          const total = (subject.tags ?? []).reduce((sum, x) => sum + x.count, 0)
          return { name: t.name, votes: t.count, pct: total ? (t.count / total) * 100 : 0 }
        })
  const counts = Array.from({ length: 10 }, (_, i) => subject.rating?.count?.[String(i + 1)] ?? 0)
  const summary = (subject.summary ?? '').trim()

  return (
    <div className="space-y-6">
      <BackLink to="/browse">返回索引</BackLink>

      <SubjectHero
        subject={subject}
        index={index}
        tags={tags}
        counts={counts}
        summary={summary}
        summaryOpen={summaryOpen}
        onToggleSummary={() => setSummaryOpen(v => !v)}
        token={token}
        onLogin={openLogin}
      />

      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="min-w-0">
          {viaMirror && <Notice>当前详情来自镜像站，敏感内容可能不可见。</Notice>}

          <SubjectTabs
            tab={tab}
            onTabChange={setTab}
            characters={characters}
            persons={persons}
            relations={relations}
            episodes={episodes}
          />
        </div>

        <SubjectRail
          infobox={subject.infobox}
          allInfo={allInfo}
          onToggleInfo={() => setAllInfo(v => !v)}
          locked={!!index?.locked}
        />
      </div>
    </div>
  )
}
