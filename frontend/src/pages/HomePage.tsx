import * as React from 'react'
import { Link } from 'react-router-dom'
import { ArrowRight, Database, KeyRound, Layers } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Cover } from '@/components/cover'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import { SearchBox } from '@/components/search-box'
import { DarkStage } from '@/components/dark-stage'
import { PosterCard } from '@/components/subject-views'
import { ErrorNote } from '@/components/ui-kit'
import { getMetaTags, img, searchSubjects } from '../api/client'
import type { Subject, TagStat } from '../api/types'
import { TYPE_LABEL, TYPE_ORDER } from '../api/types'
import { ERAS } from '../lib/eras'
import { fmtCount, fmtDate, fmtNumParts } from '../lib/format'
import { useMeta } from '../lib/use-meta'

const QUICK = [
  { label: '高分口碑', to: '/browse?sort=score&rating_min=50' },
  { label: '近一年', to: `/browse?date_from=${new Date().getFullYear() - 1}-01-01` },
  { label: '剧场版', to: '/browse?tags=%E5%89%A7%E5%9C%BA%E7%89%88' },
  { label: '原创', to: '/browse?tags=%E5%8E%9F%E5%88%9B' },
  { label: '科幻', to: '/browse?tags=%E7%A7%91%E5%B9%BB' },
]

function seededShuffle<T>(arr: T[], seed: number): T[] {
  const a = [...arr]
  let s = seed >>> 0
  for (let i = a.length - 1; i > 0; i--) {
    s = (s * 1664525 + 1013904223) >>> 0
    const j = s % (i + 1)
    ;[a[i], a[j]] = [a[j], a[i]]
  }
  return a
}

const TYPE_HINT: Record<number, string> = {
  1: '漫画 / 小说 / 画集',
  2: 'TV / 剧场版 / OVA',
  3: '专辑 / 单曲 / OST',
  4: 'PC / 主机 / 手游',
  6: '日剧 / 欧美剧 / 综艺',
}

const HOW = [
  {
    icon: Database,
    title: '后台抓取，只存筛选信息',
    text: '站长账号在后台抓取条目的 ID、类型、标签及标签占比、排名、评分与日期；不保存简介、角色等详情。',
  },
  {
    icon: KeyRound,
    title: '浏览器直连详情',
    text: '条目详情由你的浏览器直接向 api.bgm.tv 请求，可携带你自己的凭据。',
  },
  {
    icon: Layers,
    title: '索引不依赖上游分页',
    text: '筛选与排序走本站索引，不受上游分页和限流影响；上游暂时不可用时，索引仍可检索。',
  },
]

function SectionHead({
  title,
  to,
}: {
  title: string
  to?: string
}) {
  return (
    <div className="mb-3 flex items-center justify-between gap-3">
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      {to && (
        <Button asChild variant="ghost" size="sm" className="gap-1 text-muted-foreground">
          <Link to={to}>
            查看全部 <ArrowRight size={13} aria-hidden />
          </Link>
        </Button>
      )}
    </div>
  )
}

function Shelf({
  title,
  to,
  items,
  loading,
}: {
  title: string
  to: string
  items: Subject[]
  loading: boolean
}) {
  return (
    <section>
      <SectionHead title={title} to={to} />
      <div data-testid="subject-grid" className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 lg:grid-cols-6">
        {loading
          ? Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="space-y-2">
                <Skeleton className="aspect-[5/7] w-full rounded-lg" />
                <Skeleton className="h-4 w-4/5" />
                <Skeleton className="h-3 w-2/5" />
              </div>
            ))
          : items
              .filter(s => s.image)
              .slice(0, 6)
              .map(s => <PosterCard key={s.id} subject={s} />)}
      </div>
    </section>
  )
}

export default function HomePage() {
  const meta = useMeta()
  const [doing, setDoing] = React.useState<Subject[]>([])
  const [recent, setRecent] = React.useState<Subject[]>([])
  const [top, setTop] = React.useState<Subject[]>([])
  const [tags, setTags] = React.useState<TagStat[]>([])
  const [seed, setSeed] = React.useState(() => Math.floor(Math.random() * 1e9))
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<unknown>(null)

  React.useEffect(() => {
    let alive = true
    Promise.allSettled([
      searchSubjects({ sort: 'doing', order: 'desc', page_size: 12 }),
      searchSubjects({ sort: 'id', order: 'desc', page_size: 12 }),
      searchSubjects({ types: [2], sort: 'score', order: 'desc', rating_min: 50, page_size: 12 }),
      getMetaTags(0),
    ]).then(([doingRes, recentRes, topRes, tagRes]) => {
      if (!alive) return
      if (doingRes.status === 'fulfilled') setDoing(doingRes.value.items)
      if (recentRes.status === 'fulfilled') setRecent(recentRes.value.items)
      if (topRes.status === 'fulfilled') setTop(topRes.value.items)
      if (tagRes.status === 'fulfilled') setTags(tagRes.value.items.slice(0, 18))
      if (
        doingRes.status === 'rejected' &&
        recentRes.status === 'rejected' &&
        topRes.status === 'rejected'
      ) {
        setError(topRes.reason)
      }
      setLoading(false)
    })
    return () => {
      alive = false
    }
  }, [])

  const total = meta?.stats.total
  const count = total ? fmtNumParts(total) : null

  // 灵感墙素材池：近期热门 + 高分榜，复用两个书架请求的结果，不额外发请求。
  const pool = React.useMemo(() => {
    const seen = new Set<number>()
    return [...doing, ...top].filter(s => {
      if (!s.image || seen.has(s.id)) return false
      seen.add(s.id)
      return true
    })
  }, [doing, top])
  const wall = React.useMemo(() => seededShuffle(pool, seed).slice(0, 6), [pool, seed])
  const backdrop = wall[0] ? img(wall[0].image) : ''

  return (
    <div className="space-y-10">
      <DarkStage
        backdropSrc={backdrop}
        imgClassName="opacity-25"
        overlayClassName="bg-gradient-to-t from-black/80 via-black/50 to-black/30"
      >
        <div className="relative grid gap-6 p-6 sm:p-8 lg:grid-cols-[minmax(0,1fr)_340px] lg:items-end lg:p-10">
          <div>
            <p className="text-[11px] font-medium tracking-[0.22em] text-white/60">
              BANGUMI INDEX
            </p>
            <h1 className="mt-3 text-[clamp(2rem,4.4vw,3.2rem)] font-semibold leading-[1.1] tracking-tight">
              {count ? (
                <>
                  在 {count.value} {count.unit}部作品里，
                </>
              ) : (
                <>在所有作品里，</>
              )}
              <br className="hidden sm:block" />
              找到下一部。
            </h1>
            <p className="mt-3 max-w-xl text-sm leading-relaxed text-white/65">
              筛选条件写入地址栏，可收藏、可分享；返回索引时保留原来的筛选和滚动位置。
            </p>

            <SearchBox size="lg" onDark className="mt-5 max-w-xl" />

            <div className="mt-3 flex flex-wrap items-center gap-1.5">
              <span className="mr-0.5 text-xs text-white/55">试试</span>
              {QUICK.map(q => (
                <Button
                  key={q.label}
                  asChild
                  size="xs"
                  variant="outline"
                  className="border-white/15 bg-white/10 text-white hover:bg-white/20"
                >
                  <Link to={q.to}>{q.label}</Link>
                </Button>
              ))}
            </div>

            {meta && (
              <p className="num mt-5 text-xs text-white/45">
                {fmtCount(meta.stats.tags)}个标签 · {TYPE_ORDER.length} 种类型 · 最近更新{' '}
                {fmtDate(meta.stats.last_fetched)}
              </p>
            )}
          </div>

          <div className="hidden lg:block">
            <div className="mb-2 flex items-center justify-between">
              <p className="text-xs text-white/55">从这里开始</p>
              <Button
                type="button"
                variant="ghost"
                size="xs"
                onClick={() => setSeed(s => s + 1)}
                className="text-white/70 hover:bg-white/10 hover:text-white"
              >
                换一批
              </Button>
            </div>
            <div className="grid grid-cols-3 gap-2">
              {wall.length === 0
                ? Array.from({ length: 6 }).map((_, i) => (
                    <Skeleton key={i} className="aspect-[5/7] rounded-md bg-white/10" />
                  ))
                : wall.map(s => (
                    <Link key={s.id} to={`/subject/${s.id}`} className="group" title={s.name_cn || s.name}>
                      <Cover
                        src={s.image}
                        alt={s.name_cn || s.name}
                        priority
                        className="aspect-[5/7] rounded-md border border-white/15"
                      />
                    </Link>
                  ))}
            </div>
          </div>
        </div>
      </DarkStage>

      {error ? <ErrorNote error={error} /> : null}

      <section>
        <SectionHead title="按类型浏览" />
        <div data-testid="type-grid" className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
          {TYPE_ORDER.map(t => (
            <Link
              key={t}
              to={`/browse?types=${t}&sort=rank`}
              className="group rounded-lg border p-3 transition-colors hover:bg-muted/50"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-medium">{TYPE_LABEL[t]}</span>
                <span className="text-xs tabular-nums text-muted-foreground">
                  {meta ? fmtCount(meta.stats.by_type[String(t)] ?? 0) : '—'}
                </span>
              </div>
              <p className="mt-0.5 text-xs text-muted-foreground">{TYPE_HINT[t]}</p>
            </Link>
          ))}
        </div>
      </section>

      <Shelf
        title="近期热门"
        to="/browse?sort=doing"
        items={doing}
        loading={loading}
      />
      <Shelf
        title="最近收录"
        to="/browse?sort=id&order=desc"
        items={recent}
        loading={loading}
      />
      <Shelf
        title="高分榜"
        to="/browse?types=2&sort=score&rating_min=50"
        items={top}
        loading={loading}
      />

      <section className="border-y py-5">
        <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
          <span className="text-xs font-medium text-muted-foreground">按年代</span>
          {ERAS.map(e => (
            <Link
              key={e.label}
              to={`/browse?date_from=${e.from}&date_to=${e.to}`}
              className="text-sm text-muted-foreground hover:text-foreground hover:underline"
            >
              {e.label}
            </Link>
          ))}
          <Link
            to="/browse"
            className="ml-auto hidden items-center gap-1 text-xs text-muted-foreground hover:text-foreground sm:flex"
          >
            全部索引 <ArrowRight size={13} aria-hidden />
          </Link>
        </div>
      </section>

      <section>
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-lg font-semibold tracking-tight">热门标签</h2>
          <Link
            to="/browse"
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            去索引页筛选 <ArrowRight size={13} aria-hidden />
          </Link>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {tags.length === 0
            ? Array.from({ length: 12 }).map((_, i) => (
                <Skeleton key={i} className="h-6 w-14" />
              ))
            : tags.map(t => (
                <Link key={t.name} to={`/browse?tags=${encodeURIComponent(t.name)}`}>
                  <Badge variant="outline" className="gap-1.5 font-normal hover:bg-accent">
                    {t.name}
                    <span className="num text-xs text-muted-foreground">{fmtCount(t.count)}</span>
                  </Badge>
                </Link>
              ))}
        </div>
      </section>

      <section>
        <SectionHead title="它是怎么工作的" />
        <div className="grid gap-3 sm:grid-cols-3">
          {HOW.map(card => {
            const Icon = card.icon
            return (
              <div key={card.title}>
                <div className="mb-2 inline-flex size-7 items-center justify-center rounded bg-primary/10 text-primary">
                  <Icon size={15} aria-hidden />
                </div>
                <p className="text-sm font-medium">{card.title}</p>
                <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{card.text}</p>
              </div>
            )
          })}
        </div>
      </section>
    </div>
  )
}
