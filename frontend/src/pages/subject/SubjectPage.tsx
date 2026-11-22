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
  getSubjectExtDetail,
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
import { isExtSource, sourceLabel } from '../../api/vocab'
import { useCredential } from '../../lib/credential'
import { getLastUsedEndpoint } from '../../net/endpoints'
import { loadPage, savePage } from '../../lib/page-cache'
import { useScrollMemory } from '../../lib/use-scroll-memory'
import {
  extToBgmSubject,
  infoValue,
  type GroupExtra,
  type SubjectSnapshot,
} from './shared'
import {
  buildFusedFacts,
  mergeSummaries,
  variantOf,
  type FusionRow,
} from './fusion'
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
  const [extLinkHref, setExtLinkHref] = React.useState<string | null>(null)
  const [altSummary, setAltSummary] = React.useState<{ text: string; source: string } | null>(null)
  // 组内其他扩展源的归一化详情（键=该行公开 id）：信息栏按源分节 + 简介折叠共存
  const [groupExtras, setGroupExtras] = React.useState<Record<string, GroupExtra>>({})

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
      setExtLinkHref(snap.extLinkHref ?? null)
      setAltSummary(snap.altSummary ?? null)
      setGroupExtras(snap.groupExtras ?? {})
      setLoading(false)
      return
    }

    let alive = true
    const ctrl = new AbortController()
    setLoading(true)
    setError(null)
    setSubject(null)
    setExtLinkHref(null)
    setAltSummary(null)
    setGroupExtras({})
    setSummaryOpen(false)
    setAllInfo(false)

    // 组内其他来源行的详情并行拉取（扩展源走服务端代理，同组直连源走浏览器
    // 直连，与主获取同一分派；失败静默跳过该行）：不阻塞主渲染——主内容先画，
    // 信息栏融合表与简介折叠到达后补入。
    const fetchGroupExtras = (indexData: IndexedSubject | null) => {
      const rows = (indexData?.sources ?? []).filter(s => s.id !== subjectID)
      for (const row of rows) {
        if (isExtSource(row.source)) {
          getSubjectExtDetail(row.id, ctrl.signal)
            .then(d => {
              if (!alive) return
              setGroupExtras(prev => ({
                ...prev,
                [String(row.id)]: { facts: d.facts ?? [], summary: d.summary },
              }))
            })
            .catch(() => {
              // 该来源详情不可用：静默跳过，融合表里不出现该行
            })
        } else {
          // 同组直连源行的字段也并入融合表（每组至多一两行，复用详情缓存）
          bgmSubject(row.id, token, ctrl.signal)
            .then(b => {
              if (!alive) return
              setGroupExtras(prev => ({
                ...prev,
                [String(row.id)]: {
                  facts: (b.infobox ?? []).map(f => ({ k: f.key, v: infoValue(f.value) })),
                },
              }))
            })
            .catch(() => {
              // 同上：静默跳过
            })
        }
      }
    }

    // 主行缺简介时用组内其他来源的简介补（按主行优先级顺序尝试，命中即停；
    // 详情获取策略按来源 manifest 分派：代理源走服务端，直连源走浏览器——与主获取一致）。
    const fetchSummaryFallback = async (indexData: IndexedSubject | null) => {
      const rows = (indexData?.sources ?? []).filter(s => s.id !== subjectID)
      for (const row of rows) {
        if (!alive) return
        try {
          if (isExtSource(row.source)) {
            const d = await getSubjectExtDetail(row.id, ctrl.signal)
            const text = (d.summary ?? '').trim()
            if (text && alive) {
              setAltSummary({ text, source: row.source })
              return
            }
          } else {
            const b = await bgmSubject(row.id, undefined, ctrl.signal)
            const text = (b.summary ?? '').trim()
            if (text && alive) {
              setAltSummary({ text, source: row.source })
              return
            }
          }
        } catch (e) {
          if ((e as Error).name === 'AbortError' || !alive) return
          // 该来源简介不可用：继续试下一来源
        }
      }
    }

    // 详情获取器按条目来源分派：bgm 走浏览器直连（凭据不出本机），
    // 其余来源走服务端代理端点（TMDB key 与限速不出服务端）。
    // 来源要先由索引行给出，因此索引详情先行、详情获取随后。
    const dispatch = (indexData: IndexedSubject | null) => {
      if (!alive) return
      if (isExtSource(indexData?.source)) {
        getSubjectExtDetail(subjectID, ctrl.signal)
          .then(d => {
            if (!alive) return
            setExtLinkHref(d.link ?? null)
            setSubject(extToBgmSubject(d, indexData))
            setLoading(false)
            if (!(d.summary ?? '').trim()) void fetchSummaryFallback(indexData)
          })
          .catch(err => {
            if (!alive || (err as Error).name === 'AbortError') return
            setError(err)
            setLoading(false)
          })
        return
      }
      bgmSubject(subjectID, token, ctrl.signal)
        .then(sub => {
          if (!alive) return
          // 大字评分用作品合成值（索引主行上物化，组内各源按权重加权）；
          // 评分分布仍用 bgm 原始分布，各源原始分在「各源评分」行单独展示。
          if (indexData && indexData.score > 0 && sub.rating) {
            setSubject({
              ...sub,
              rating: { ...sub.rating, score: indexData.score, total: indexData.rating_total },
            })
          } else {
            setSubject(sub)
          }
          setViaMirror(getLastUsedEndpoint()?.kind === 'mirror')
          setLoading(false)
          if (!(sub.summary ?? '').trim()) void fetchSummaryFallback(indexData)
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
    }

    getSubjectIndex(subjectID, ctrl.signal)
      .then(idx => {
        if (!alive) return
        setIndex(idx)
        dispatch(idx)
        void fetchGroupExtras(idx)
      })
      .catch(() => {
        if (!alive) return
        setIndex(null)
        dispatch(null)
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
        extLinkHref,
        altSummary,
        groupExtras,
      } satisfies SubjectSnapshot)
    }
  }, [location.key, loading, subject, index, characters, persons, relations, episodes, viaMirror, extLinkHref, altSummary, groupExtras, subjectID])

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
        // 索引未返回时按 id 段推断（公开 id 从 1e10 起为扩展源，迁移 0005 的数据不变量）
        ext={isExtSource(index?.source) || (!index && subjectID >= 10000000000)}
        onLogin={openLogin}
        onRetry={() => {
          resetBgmCircuit()
          setReload(v => v + 1)
        }}
      />
    )
  }

  const ext = isExtSource(index?.source)
  const tags: TagVote[] =
    index?.tags?.length
      ? index.tags
      : (subject.tags ?? []).map(t => {
          const total = (subject.tags ?? []).reduce((sum, x) => sum + x.count, 0)
          return { name: t.name, votes: t.count, pct: total ? (t.count / total) * 100 : 0 }
        })
  const counts = Array.from({ length: 10 }, (_, i) => subject.rating?.count?.[String(i + 1)] ?? 0)
  const summary = (subject.summary ?? '').trim()

  // 信息栏融合表：组内全部来源行按后端给出的序（管理员设置的有效优先序，
  // 与主行选择同一份序）逐行提供自己的字段——先到先得、同值静默去重、
  // 异值并陈。主行字段用当前已取到的 infobox；同组其他行的字段来自并行
  // 拉取的 groupExtras（失败的行静默缺位）。索引未返回时退化为只有主行。
  const displayedSummary = summary || (altSummary?.text ?? '')
  const groupRows = index?.sources ?? []
  const mainFacts = (subject.infobox ?? []).map(f => ({ k: f.key, v: infoValue(f.value) }))
  const fusionRows: FusionRow[] = []
  let mainIncluded = false
  for (const row of groupRows) {
    if (row.id === subjectID) {
      mainIncluded = true
      fusionRows.push({ source: row.source, variant: variantOf(row), facts: mainFacts })
      continue
    }
    const extra = groupExtras[String(row.id)]
    if (!extra || extra.facts.length === 0) continue
    fusionRows.push({ source: row.source, variant: variantOf(row), facts: extra.facts })
  }
  if (!mainIncluded) {
    fusionRows.push({ source: index?.source ?? '', variant: subject.platform || '', facts: mainFacts })
  }
  const fusedFacts = buildFusedFacts(fusionRows)
  // 组内其他来源的简介折叠（同源多条并进一个折叠、段内标变体）；
  // 与主简介或先前来源完全相同的不再重复。
  const summaryGroups = mergeSummaries(
    displayedSummary,
    groupRows
      .filter(row => row.id !== subjectID && (groupExtras[String(row.id)]?.summary ?? '').trim())
      .map(row => ({
        source: row.source,
        variant: variantOf(row),
        text: (groupExtras[String(row.id)]?.summary ?? '').trim(),
      })),
  )

  return (
    <div className="space-y-6">
      <BackLink to="/browse">返回索引</BackLink>

      <SubjectHero
        subject={subject}
        index={index}
        tags={tags}
        counts={counts}
        summary={displayedSummary}
        summaryNote={summary || !altSummary ? undefined : '简介来自 ' + sourceLabel(altSummary.source)}
        sources={index?.sources}
        summaryOpen={summaryOpen}
        onToggleSummary={() => setSummaryOpen(v => !v)}
        token={token}
        onLogin={openLogin}
        summaryGroups={summaryGroups}
        extLink={
          ext && extLinkHref
            ? {
                href: extLinkHref,
                label: sourceLabel(index?.source),
              }
            : undefined
        }
      />

      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="min-w-0">
          {viaMirror && <Notice>当前详情来自镜像站，敏感内容可能不可见。</Notice>}

          {/* 角色/制作/关联/剧集是 bgm 专属区块；扩展源只渲染信息栏 */}
          {!ext && (
            <SubjectTabs
              tab={tab}
              onTabChange={setTab}
              characters={characters}
              persons={persons}
              relations={relations}
              episodes={episodes}
            />
          )}
        </div>

        <SubjectRail
          facts={fusedFacts}
          allInfo={allInfo}
          onToggleInfo={() => setAllInfo(v => !v)}
          locked={!!index?.locked}
        />
      </div>
    </div>
  )
}
