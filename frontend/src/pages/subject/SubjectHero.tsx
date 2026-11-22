import { Fragment } from 'react'
import { ArrowUpRight, KeyRound } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Cover } from '@/components/cover'
import { DarkStage } from '@/components/dark-stage'
import { cn } from '@/lib/utils'
import { sourceLabel, typeLabel } from '../../api/vocab'
import type { BgmSubject, SubjectSourceRow } from '../../api/types'
import type { Subject as IndexedSubject } from '../../api/types'
import type { TagVote } from '../../api/types'
import { img } from '../../api/client'
import { fmtNum } from '../../lib/format'
import { titleOf } from './shared'
import { groupScoreRows } from './fusion'
import type { SummaryGroup } from './fusion'
import { RatingDonut, TagBars } from './SubjectCharts'

// 条目 hero（首页同族舞台语言：DarkStage + 封面模糊背景 + 横向渐变遮罩，
// 浅色/深色材质由 .dark-stage 变量族决定）。
// lg+ 右侧分出磨砂面板：上半评分分布扇形图、下半标签柱状图（可横向滚动）。
// 窄屏封面 110px 与标题并排，数据区全宽换行；面板只在 lg+ 出现。
export function SubjectHero({
  subject,
  index,
  tags,
  counts,
  summary,
  summaryOpen,
  onToggleSummary,
  token,
  onLogin,
  extLink,
  sources,
  summaryNote,
  summaryGroups,
}: {
  subject: BgmSubject
  index: IndexedSubject | null
  tags: TagVote[]
  counts: number[]
  summary: string
  summaryOpen: boolean
  onToggleSummary: () => void
  token?: string
  onLogin: () => void
  /** 扩展源：官方站点外链与展示名（bgm 默认链到 bgm.tv/subject/{id}） */
  extLink?: { href: string; label: string }
  /** 作品组内全部来源（多源评分对比行；单源不渲染） */
  sources?: SubjectSourceRow[]
  /** 简介来自其他来源时的署名（如「简介来自 AniList」） */
  summaryNote?: string
  /** 组内其他来源的简介，按源归并（同源多条并进一个折叠、段内标变体） */
  summaryGroups?: SummaryGroup[]
}) {
  const summaryLong = summary.length > 180
  const collection = subject.collection
  const rank = subject.rating?.rank || index?.rank || 0
  const cover = subject.images?.large || subject.image
  // 舞台背景候选：本行封面优先；本行没有封面时用组内其他来源的封面，
  // 避免黑/空舞台（组内行自带各自的封面图）。
  const groupCover = sources?.map(s => s.image).find(Boolean)
  const backdrop = cover ? img(cover) : groupCover ? img(groupCover) : ''
  // 各源评分按源归组：来源标一次、同源多行（剧场版/TV）并列
  const scoreGroups = sources && sources.length > 1 ? groupScoreRows(sources) : []
  const collectionRows = collection
    ? ([
        ['想看', collection.wish],
        ['看过', collection.collect],
        ['在看', collection.doing],
        ['搁置', collection.on_hold],
        ['抛弃', collection.dropped],
      ] as const)
    : []

  return (
    <DarkStage
      backdropSrc={backdrop}
      imgClassName="opacity-55"
      overlayClassName="bg-gradient-to-r from-white/80 via-white/55 to-white/30 dark:from-black/80 dark:via-black/45 dark:to-black/15"
    >
      <div className="relative grid gap-x-6 gap-y-4 p-5 max-sm:grid-cols-[110px_minmax(0,1fr)] sm:p-7 sm:grid-cols-[190px_minmax(0,1fr)] lg:grid-cols-[190px_minmax(0,1fr)_300px] lg:gap-x-8">
        <Cover
          src={cover}
          alt={titleOf(subject)}
          priority
          className="aspect-[5/7] w-[110px] shrink-0 self-start rounded-lg shadow-2xl ring-1 ring-(--stage-line) sm:row-span-2 sm:w-full sm:rounded-xl"
        />

        <div className="flex min-w-0 flex-col justify-center">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
            {/* 来源徽标：小字号弱化，bgm 与扩展源统一标注 */}
            <span className="text-xs text-(--stage-fg)/55">{sourceLabel(index?.source)}</span>
            {/* 类型徽标用该行所在源的原生叫法（双层词表：bgm 的 6 显示「三次元」） */}
            <span className="rounded-md border border-(--stage-line) bg-(--stage-glass) px-2 py-0.5 text-xs font-medium text-(--stage-fg)">
              {typeLabel(subject.type, index?.source)}
            </span>
            {subject.platform && (
              <span className="rounded-md border border-(--stage-line) bg-(--stage-glass) px-2 py-0.5 text-xs text-(--stage-fg)/85">
                {subject.platform}
              </span>
            )}
            {subject.nsfw && (
              <span className="rounded-md border border-red-400/30 bg-red-400/15 px-2 py-0.5 text-xs text-red-700 dark:text-red-300">
                敏感
              </span>
            )}
            {subject.date && (
              <span className="num text-xs text-(--stage-fg)/55">首播 {subject.date}</span>
            )}
            {index?.fetched_at && (
              <span className="num text-xs text-(--stage-fg)/55">
                更新 {index.fetched_at.slice(0, 10)}
              </span>
            )}
          </div>

          <h1 className="mt-2 text-2xl font-semibold tracking-tight text-(--stage-fg) sm:text-3xl">
            {titleOf(subject)}
          </h1>
          {subject.name_cn && subject.name_cn !== subject.name && (
            <p className="mt-1 text-[13px] text-(--stage-fg)/60">{subject.name}</p>
          )}
        </div>

        <div className="min-w-0 max-sm:col-span-2 sm:col-start-2 sm:row-start-2">
          <div className="flex flex-wrap items-end gap-x-8 gap-y-3">
            <div>
              <p className="text-xs text-(--stage-fg)/55">评分</p>
              <p className="num mt-1 text-3xl font-semibold leading-none text-amber-600 dark:text-amber-400">
                {subject.rating?.score ? subject.rating.score.toFixed(1) : '—'}
                <span className="ml-1 text-sm font-normal text-(--stage-fg)/50">/ 10</span>
              </p>
            </div>
            <div>
              <p className="text-xs text-(--stage-fg)/55">排名</p>
              <p className="num mt-1 text-3xl font-semibold leading-none text-amber-600 dark:text-amber-400">
                {rank > 0 ? `#${rank}` : '—'}
              </p>
            </div>
            <div>
              <p className="text-xs text-(--stage-fg)/55">评分人数</p>
              <p className="num mt-1.5 text-lg font-semibold leading-none text-(--stage-fg)">
                {subject.rating?.total ? fmtNum(subject.rating.total) : '—'}
              </p>
            </div>
            <div>
              <p className="text-xs text-(--stage-fg)/55">标签票</p>
              <p className="num mt-1.5 text-lg font-semibold leading-none text-(--stage-fg)">
                {index ? fmtNum(index.tags_total) : '—'}
              </p>
            </div>
          </div>

          {collectionRows.length > 0 && (
            <p className="num mt-3 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-(--stage-fg)/55">
              {collectionRows.map(([label, value]) => (
                <span key={label}>
                  {label} <span className="font-medium text-(--stage-fg)/85">{fmtNum(value)}</span>
                </span>
              ))}
            </p>
          )}

          <div className="mt-4 flex flex-wrap items-center gap-2">
            {extLink ? (
              <Button
                asChild
                size="sm"
                variant="outline"
                className="border-(--stage-line) bg-(--stage-glass) text-(--stage-fg) hover:bg-white/80 hover:text-(--stage-fg) dark:hover:bg-white/20"
              >
                <a href={extLink.href} target="_blank" rel="noreferrer">
                  在 {extLink.label} 查看 <ArrowUpRight size={12} aria-hidden />
                </a>
              </Button>
            ) : (
              <Button
                asChild
                size="sm"
                variant="outline"
                className="border-(--stage-line) bg-(--stage-glass) text-(--stage-fg) hover:bg-white/80 hover:text-(--stage-fg) dark:hover:bg-white/20"
              >
                <a href={`https://bgm.tv/subject/${subject.id}`} target="_blank" rel="noreferrer">
                  在 Bangumi 查看 <ArrowUpRight size={12} aria-hidden />
                </a>
              </Button>
            )}
            {!token && !extLink && (
              <Button
                size="sm"
                variant="ghost"
                onClick={onLogin}
                className="gap-1.5 text-(--stage-fg)/70 hover:bg-zinc-950/5 hover:text-(--stage-fg) dark:hover:bg-white/10 dark:hover:text-white"
              >
                <KeyRound size={13} aria-hidden /> 登录后可查看受限内容
              </Button>
            )}
          </div>

          {/* 多源评分对比行（按源归组）：来源标只出现一次，同源多行
              （剧场版/TV 各一条）用变体并列区分；每行仍链到官方条目页。 */}
          {scoreGroups.length > 0 && (
            <p className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-(--stage-fg)/55">
              <span>各源评分</span>
              {scoreGroups.map(g => (
                <span key={g.source} className="inline-flex items-baseline gap-1.5">
                  <span className="text-(--stage-fg)/55">{sourceLabel(g.source)}</span>
                  {g.rows.map((r, i) => (
                    <Fragment key={r.id}>
                      {i > 0 && <span className="text-(--stage-fg)/40">·</span>}
                      <a
                        href={r.url}
                        target="_blank"
                        rel="noreferrer"
                        className="num inline-flex items-baseline gap-1 text-(--stage-fg)/70 underline-offset-2 hover:text-(--stage-fg) hover:underline"
                        title={`${sourceLabel(g.source)}${r.variant ? ' · ' + r.variant : ''} · ${r.title} · ${fmtNum(r.ratingTotal)} 人评分`}
                      >
                        {r.variant && <span className="text-(--stage-fg)/55">{r.variant}</span>}
                        <b className="num font-medium text-(--stage-fg)">
                          {r.score > 0 ? r.score.toFixed(1) : '—'}
                        </b>
                      </a>
                    </Fragment>
                  ))}
                </span>
              ))}
            </p>
          )}

          {summary && (
            <p
              className={cn(
                'mt-4 max-w-[72ch] whitespace-pre-line text-[13.5px] leading-relaxed text-(--stage-fg)/70',
                summaryLong && !summaryOpen && 'line-clamp-4',
              )}
            >
              {summary}
              {summaryNote && (
                <span className="ml-1.5 text-[11px] text-(--stage-fg)/45">（{summaryNote}）</span>
              )}
            </p>
          )}
          {/* 组内其他来源的简介折叠（按源归并）：同源多条并进一个折叠、
              段内标变体；与主简介或先前来源完全相同的不再重复。 */}
          {summaryGroups?.map(g => (
            <details key={g.source} className="mt-2 max-w-[72ch] text-xs text-(--stage-fg)/60">
              <summary className="cursor-pointer select-none text-(--stage-fg)/55 hover:text-(--stage-fg)/80">
                {sourceLabel(g.source)} 简介
              </summary>
              {g.segments.map((seg, i) => (
                <div key={i} className="mt-1.5">
                  {seg.variant && (
                    <p className="text-[11px] text-(--stage-fg)/45">{seg.variant}</p>
                  )}
                  <p className="whitespace-pre-line text-[12.5px] leading-relaxed text-(--stage-fg)/65">
                    {seg.text}
                  </p>
                </div>
              ))}
            </details>
          ))}
          {summaryLong && (
            <Button
              type="button"
              variant="link"
              size="sm"
              className="mt-1 h-auto px-0 text-xs text-(--stage-fg)/60 hover:text-(--stage-fg)"
              onClick={onToggleSummary}
            >
              {summaryOpen ? '收起简介' : '展开全部简介'}
            </Button>
          )}
        </div>

        {/* 图形即玻璃：无面板底——扇形自带圆形镜片、柱条自带毛玻璃。
            lg+ 右侧第三列；sm–md hero 底部两列横排；窄屏竖排全宽。 */}
        <div className="grid col-span-full grid-cols-1 gap-3 sm:grid-cols-2 lg:col-span-1 lg:col-start-3 lg:row-span-2 lg:row-start-1 lg:grid-cols-1">
          <div>
            <p className="mb-2 text-xs text-(--stage-fg)/55">评分分布</p>
            <RatingDonut counts={counts} />
          </div>
          <div className="min-w-0">
            <p className="mb-2 text-xs text-(--stage-fg)/55">
              标签 <span className="text-(--stage-fg)/35">左右滑动查看</span>
            </p>
            <TagBars tags={tags} />
          </div>
        </div>
      </div>
    </DarkStage>
  )
}
