import * as React from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, ExternalLink, KeyRound, RefreshCw, Server, Sparkles, Trophy } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ErrorView, Notice, RatingHistogram, TagProportion } from '@/components/blocks'
import { useAuthDialog } from '@/components/auth-dialog-provider'
import {
  ApiError, bgmCharacters, bgmEpisodes, bgmPersons, bgmRelations, bgmSubject, getSubjectIndex, img,
  imgOriginal, resetBgmCircuit,
} from '../api/client'
import type {
  BgmCharacter, BgmEpisode, BgmPerson, BgmRelation, BgmSubject, Subject as IndexedSubject,
} from '../api/types'
import { TYPE_LABEL } from '../api/types'
import { useCredential } from '../lib/credential'
import { getLastUsedEndpoint } from '../net/endpoints'
import { fmtNum } from '../lib/format'

type InfoValue = string | { k?: string; v?: string } | Array<string | { k?: string; v?: string }>

function infoValue(value: InfoValue): string {
  if (Array.isArray(value)) return value.map(infoValue).filter(Boolean).join(' / ')
  if (value && typeof value === 'object') {
    const obj = value as { v?: string; k?: string }
    return obj.v ?? obj.k ?? ''
  }
  return value == null ? '' : String(value)
}

export default function SubjectPage() {
  const { id } = useParams()
  const subjectID = Number(id)
  const credential = useCredential()
  const token = credential?.token
  const { openLogin } = useAuthDialog()
  const [reload, setReload] = React.useState(0)

  const [index, setIndex] = React.useState<IndexedSubject | null>(null)
  const [subject, setSubject] = React.useState<BgmSubject | null>(null)
  const [characters, setCharacters] = React.useState<BgmCharacter[]>([])
  const [persons, setPersons] = React.useState<BgmPerson[]>([])
  const [relations, setRelations] = React.useState<BgmRelation[]>([])
  const [episodes, setEpisodes] = React.useState<BgmEpisode[]>([])
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<unknown>(null)
  const [allInfo, setAllInfo] = React.useState(false)
  const [coverFailed, setCoverFailed] = React.useState(false)
  const [viaMirror, setViaMirror] = React.useState(false)

  React.useEffect(() => {
    if (!Number.isFinite(subjectID) || subjectID <= 0) return
    let alive = true
    const ctrl = new AbortController()
    setLoading(true)
    setError(null)
    setSubject(null)

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
  }, [subjectID, token, reload])

  if (!Number.isFinite(subjectID) || subjectID <= 0) {
    return <ErrorView error={new ApiError(400, 'bad_id', '无效的条目 ID')} />
  }

  if (!subject && !index && loading) {
    return (
      <div className="space-y-6">
        <div className="flex flex-col gap-6 sm:flex-row">
          <Skeleton className="aspect-[5/7] w-40 shrink-0 rounded-lg sm:w-48" />
          <div className="flex-1 space-y-3">
            <Skeleton className="h-7 w-2/3" />
            <Skeleton className="h-4 w-1/3" />
            <Skeleton className="h-24 w-full" />
          </div>
        </div>
      </div>
    )
  }

  if (!subject) {
    const apiErr = error instanceof ApiError ? error : null
    return (
      <div className="mx-auto max-w-3xl">
        <div className="mb-4">
          <Button asChild variant="ghost" size="sm" className="gap-1">
            <Link to="/browse"><ArrowLeft size={13} /> 返回浏览</Link>
          </Button>
        </div>
        {index ? (
          <div>
            <div className="flex flex-col gap-6 sm:flex-row">
              {index.image && !coverFailed ? (
                <img
                  referrerPolicy="no-referrer"
                  onError={e => {
                    const original = imgOriginal(index.image)
                    if (e.currentTarget.src !== original) e.currentTarget.src = original
                    else setCoverFailed(true)
                  }}
                  src={img(index.image)}
                  alt={index.name_cn || index.name}
                  className="aspect-[5/7] w-40 shrink-0 rounded-lg border object-cover sm:w-48"
                />
              ) : (
                <div className="flex aspect-[5/7] w-40 shrink-0 items-center justify-center rounded-lg border bg-muted text-xs text-muted-foreground sm:w-48">
                  封面不可用
                </div>
              )}
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5">
                  <Badge variant="outline">{TYPE_LABEL[index.type] ?? '其他'}</Badge>
                  {index.nsfw && <Badge variant="destructive">敏感</Badge>}
                  {index.rank > 0 && <Badge variant="secondary">#{index.rank}</Badge>}
                </div>
                <h1 className="mt-2 text-2xl font-semibold tracking-tight">{index.name_cn || index.name}</h1>
                {index.name_cn && index.name_cn !== index.name && (
                  <p className="mt-1 text-sm text-muted-foreground">{index.name}</p>
                )}
                <p className="mt-2 text-xs text-muted-foreground">
                  本站索引：{index.date || '无日期'} · {index.score > 0 ? `${index.score.toFixed(1)} 分` : '暂无评分'}
                  {index.tags_total > 0 && ` · ${index.tags_total} 票标签`}
                </p>
                <div className="mt-4 rounded-lg border border-dashed p-4">
                  {loading ? (
                    <p className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
                      <RefreshCw size={13} className="animate-spin" /> 正在获取条目详情…
                    </p>
                  ) : (
                    <>
                      <p className="text-sm font-medium">
                        {apiErr?.status === 404 ? '详情需要登录后查看' : (apiErr?.message ?? '详情加载失败')}
                      </p>
                      <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                        {apiErr?.hint ?? '条目详情由你的浏览器直接向 Bangumi 获取。'}
                      </p>
                    </>
                  )}
                  <div className="mt-3 flex flex-wrap gap-2">
                    <Button size="sm" onClick={openLogin} className="gap-1.5">
                      <KeyRound size={13} /> 使用我的账号登录
                    </Button>
                    <Button size="sm" variant="outline" onClick={openLogin} className="gap-1.5">
                      <Server size={13} /> 切换数据通道
                    </Button>
                    {!loading && (
                      <Button size="sm" variant="outline" onClick={() => { resetBgmCircuit(); setReload(v => v + 1) }} className="gap-1.5">
                        <RefreshCw size={13} /> 重试
                      </Button>
                    )}
                  </div>
                </div>
              </div>
            </div>
            {index.tags && index.tags.length > 0 && (
              <Card className="mt-6">
                <CardHeader><CardTitle className="text-sm">标签与占比</CardTitle></CardHeader>
                <CardContent>
                  <TagProportion tags={index.tags} />
                </CardContent>
              </Card>
            )}
          </div>
        ) : (
          <>
            <ErrorView error={error} onRetry={() => setReload(v => v + 1)} />
            <div className="text-center">
              <Button size="sm" variant="outline" onClick={openLogin} className="gap-1.5">
                <KeyRound size={13} /> 登录 Bangumi
              </Button>
            </div>
          </>
        )}
      </div>
    )
  }

  const tags = index?.tags?.length
    ? index.tags
    : (subject.tags ?? []).map(t => {
        const total = (subject.tags ?? []).reduce((sum, x) => sum + x.count, 0)
        return { name: t.name, votes: t.count, pct: total ? (t.count / total) * 100 : 0 }
      })

  const counts = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10].map(star => subject.rating?.count?.[String(star)] ?? 0)
  const collection = subject.collection
  const summary = (subject.summary ?? '').trim()

  return (
    <div className="space-y-6">
      <Button asChild variant="ghost" size="sm" className="gap-1">
        <Link to="/browse"><ArrowLeft size={13} /> 返回浏览</Link>
      </Button>

      <div className="flex flex-col gap-6 sm:flex-row">
        <div className="shrink-0">
          {subject.images?.large || subject.image ? (
            <img
              referrerPolicy="no-referrer"
              onError={e => { e.currentTarget.style.visibility = 'hidden' }}
              src={img(subject.images?.large || subject.image)}
              alt={subject.name_cn || subject.name}
              className="aspect-[5/7] w-44 rounded-lg border object-cover shadow-sm sm:w-52"
            />
          ) : (
            <div className="flex aspect-[5/7] w-44 items-center justify-center rounded-lg border bg-muted text-xs text-muted-foreground sm:w-52">
              无封面
            </div>
          )}
        </div>

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge variant="outline">{TYPE_LABEL[subject.type] ?? '其他'}</Badge>
            {subject.platform && <Badge variant="secondary">{subject.platform}</Badge>}
            {subject.nsfw && <Badge variant="destructive">敏感</Badge>}
            {index?.rank ? (
              <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                <Trophy size={11} className="text-amber-500" /> 全站 #{index.rank}
              </span>
            ) : null}
          </div>

          <h1 className="mt-2 text-2xl font-semibold tracking-tight sm:text-3xl">{subject.name_cn || subject.name}</h1>
          {subject.name_cn && subject.name_cn !== subject.name && (
            <p className="mt-1 text-sm text-muted-foreground">{subject.name}</p>
          )}

          <div className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
            <div className="flex items-baseline gap-1.5">
              <b className="text-2xl font-semibold tabular-nums">
                {subject.rating?.score ? subject.rating.score.toFixed(1) : '—'}
              </b>
              <span className="text-xs text-muted-foreground">
                {subject.rating?.total ? `${fmtNum(subject.rating.total)} 人评分` : '暂无评分'}
              </span>
            </div>
            {subject.date && <span className="text-xs text-muted-foreground">{subject.date}</span>}
            {index && (
              <span className="text-xs text-muted-foreground">
                标签 {fmtNum(index.tags_total)} 票 · 更新于 {index.fetched_at.slice(0, 10)}
              </span>
            )}
          </div>

          {collection && (
            <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
              {([
                ['想看', collection.wish],
                ['看过', collection.collect],
                ['在看', collection.doing],
                ['搁置', collection.on_hold],
                ['抛弃', collection.dropped],
              ] as const).map(([label, value]) => (
                <span key={label} className="inline-flex items-baseline gap-1">
                  <b className="text-[13px] font-semibold tabular-nums text-foreground">{fmtNum(value)}</b>
                  {label}
                </span>
              ))}
            </div>
          )}

          {summary && (
            <p className="mt-4 whitespace-pre-line text-[13px] leading-relaxed text-muted-foreground">
              {summary}
            </p>
          )}

          <div className="mt-4 flex flex-wrap gap-2">
            <Button asChild size="sm" variant="outline">
              <a href={`https://bgm.tv/subject/${subject.id}`} target="_blank" rel="noreferrer">
                在 Bangumi 查看 <ExternalLink size={12} />
              </a>
            </Button>
            {!token && (
              <Button size="sm" variant="ghost" onClick={openLogin} className="gap-1.5">
                <KeyRound size={13} /> 登录后可查看受限内容
              </Button>
            )}
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="min-w-0">
          {viaMirror && (
            <Notice>
              当前详情来自镜像站（公开数据），登录凭据未发送；敏感内容可能不可见。
            </Notice>
          )}

          <Tabs
            key={characters.length ? 'characters' : persons.length ? 'staff' : relations.length ? 'related' : 'episodes'}
            defaultValue={characters.length ? 'characters' : persons.length ? 'staff' : relations.length ? 'related' : 'episodes'}
          >
            <TabsList className="mb-4">
              <TabsTrigger value="characters">角色 ({characters.length})</TabsTrigger>
              <TabsTrigger value="staff">制作人员 ({persons.length})</TabsTrigger>
              <TabsTrigger value="related">关联 ({relations.length})</TabsTrigger>
              <TabsTrigger value="episodes">剧集 ({episodes.length})</TabsTrigger>
            </TabsList>

            <TabsContent value="characters">
              {characters.length === 0 ? (
                <p className="py-8 text-center text-sm text-muted-foreground">暂无角色信息</p>
              ) : (
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4">
                  {characters.slice(0, 40).map(c => (
                    <div key={c.id} className="flex gap-2 rounded-lg border p-2">
                      {c.images?.grid || c.images?.medium ? (
                        <img
                          referrerPolicy="no-referrer"
                          src={img(c.images?.grid || c.images?.medium)}
                          alt={c.name}
                          loading="lazy"
                          className="size-14 shrink-0 rounded object-cover"
                        />
                      ) : (
                        <div className="size-14 shrink-0 rounded bg-muted" />
                      )}
                      <div className="min-w-0">
                        <p className="truncate text-xs font-medium" title={c.name}>{c.name}</p>
                        <p className="truncate text-[10px] text-muted-foreground">{c.relation}</p>
                        {c.actors?.[0] && (
                          <p className="truncate text-[10px] text-muted-foreground" title={c.actors[0].name}>
                            声优 {c.actors[0].name}
                          </p>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </TabsContent>

            <TabsContent value="staff">
              {persons.length === 0 ? (
                <p className="py-8 text-center text-sm text-muted-foreground">暂无制作人员信息</p>
              ) : (
                <div className="grid grid-cols-2 gap-2 text-xs sm:grid-cols-3">
                  {persons.map(p => (
                    <div key={`${p.id}-${p.relation}`} className="rounded-md border px-2.5 py-2">
                      <p className="truncate font-medium" title={p.name}>{p.name}</p>
                      <p className="truncate text-[10px] text-muted-foreground">
                        {p.relation}{p.career?.length ? ` · ${p.career.join(' / ')}` : ''}
                      </p>
                    </div>
                  ))}
                </div>
              )}
            </TabsContent>

            <TabsContent value="related">
              {relations.length === 0 ? (
                <p className="py-8 text-center text-sm text-muted-foreground">暂无关联条目</p>
              ) : (
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                  {relations.map(r => (
                    <Link
                      key={`${r.id}-${r.relation}`}
                      to={`/subject/${r.id}`}
                      className="group flex gap-2 rounded-lg border p-2 transition-colors hover:bg-muted/50"
                    >
                      {r.images?.grid ? (
                        <img referrerPolicy="no-referrer" onError={e => { e.currentTarget.style.visibility = 'hidden' }} src={img(r.images.grid)} alt={r.name} loading="lazy" className="h-16 w-12 shrink-0 rounded object-cover" />
                      ) : (
                        <div className="h-16 w-12 shrink-0 rounded bg-muted" />
                      )}
                      <div className="min-w-0">
                        <p className="line-clamp-2 text-xs font-medium">{r.name_cn || r.name}</p>
                        <p className="mt-1 text-[10px] text-muted-foreground">{r.relation}</p>
                        {r.date && <p className="text-[10px] text-muted-foreground">{r.date}</p>}
                      </div>
                    </Link>
                  ))}
                </div>
              )}
            </TabsContent>

            <TabsContent value="episodes">
              {episodes.length === 0 ? (
                <p className="py-8 text-center text-sm text-muted-foreground">暂无剧集信息</p>
              ) : (
                <div className="divide-y rounded-lg border">
                  {episodes.map(ep => (
                    <div key={ep.id} className="flex items-baseline gap-3 px-3 py-2 text-xs">
                      <span className="w-10 shrink-0 text-right font-mono text-muted-foreground">
                        {ep.sort ?? ep.ep ?? '—'}
                      </span>
                      <span className="min-w-0 flex-1 truncate">
                        {ep.name_cn || ep.name || '未命名'}
                        {ep.name_cn && ep.name && (
                          <span className="ml-2 text-[10px] text-muted-foreground">{ep.name}</span>
                        )}
                      </span>
                      <span className="shrink-0 text-[10px] text-muted-foreground">{ep.airdate || ''}</span>
                    </div>
                  ))}
                </div>
              )}
            </TabsContent>
          </Tabs>
        </div>

        <aside className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-sm">评分分布</CardTitle></CardHeader>
            <CardContent>
              <RatingHistogram counts={counts} total={subject.rating?.total ?? 0} />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center justify-between text-sm">
                <span className="inline-flex items-center gap-1.5">
                  标签与占比 <Sparkles size={12} className="text-muted-foreground" />
                </span>
                {index ? (
                  <span className="text-[10px] font-normal text-muted-foreground">本站索引</span>
                ) : (
                  <span className="text-[10px] font-normal text-muted-foreground">Bangumi 票数</span>
                )}
              </CardTitle>
            </CardHeader>
            <CardContent>
              {tags.length ? <TagProportion tags={tags} /> : (
                <p className="text-xs text-muted-foreground">暂无标签</p>
              )}
              <p className="mt-3 text-[10px] leading-relaxed text-muted-foreground">
                占比 = 该标签票数 ÷ 该条目全部标签票数，由后台抓取时计算。
              </p>
            </CardContent>
          </Card>

          {subject.infobox && subject.infobox.length > 0 && (
            <Card>
              <CardHeader><CardTitle className="text-sm">信息</CardTitle></CardHeader>
              <CardContent className="space-y-1.5 text-xs">
                {(allInfo ? subject.infobox : subject.infobox.slice(0, 10)).map((row, i) => (
                  <div key={`${row.key}-${i}`} className="flex gap-2">
                    <span className="w-20 shrink-0 text-muted-foreground">{row.key}</span>
                    <span className={allInfo ? 'min-w-0 flex-1 break-words' : 'line-clamp-3 min-w-0 flex-1 break-words'}>
                      {infoValue(row.value)}
                    </span>
                  </div>
                ))}
                {subject.infobox.length > 10 && (
                  <button
                    type="button"
                    className="pt-1 text-[11px] text-primary hover:underline"
                    onClick={() => setAllInfo(v => !v)}
                  >
                    {allInfo ? '收起' : `展开全部（共 ${subject.infobox.length} 项）`}
                  </button>
                )}
              </CardContent>
            </Card>
          )}

          {index?.locked && (
            <Notice>
              该条目在最近一次抓取中不可访问，本站索引信息可能不完整；稍后会自动重试。
            </Notice>
          )}
        </aside>
      </div>
    </div>
  )
}
