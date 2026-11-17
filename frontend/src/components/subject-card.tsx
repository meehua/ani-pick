import * as React from 'react'
import { Link } from 'react-router-dom'
import { Trophy } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { img, imgOriginal } from '../api/client'
import { TYPE_LABEL, type Subject } from '../api/types'
import { fmtNum, yearOf } from '../lib/format'

export function SubjectCard({ subject }: { subject: Subject }) {
  const [imgFailed, setImgFailed] = React.useState(false)
  const [imgLoaded, setImgLoaded] = React.useState(false)
  const title = subject.name_cn || subject.name

  // 网络被阻断时 <img> 可能长时间挂起；超时后直接显示占位。
  React.useEffect(() => {
    if (!subject.image || imgLoaded || imgFailed) return
    const t = window.setTimeout(() => setImgFailed(true), 20000)
    return () => window.clearTimeout(t)
  }, [subject.image, imgLoaded, imgFailed])
  const alt = subject.name_cn && subject.name_cn !== subject.name ? subject.name : ''

  return (
    <Link
      to={`/subject/${subject.id}`}
      className="group flex flex-col outline-none"
      data-subject-id={subject.id}
    >
      <div className="relative aspect-[5/7] w-full overflow-hidden rounded-lg border bg-muted">
        {subject.image && !imgFailed ? (
          <img
            referrerPolicy="no-referrer"
            src={img(subject.image)}
            alt={title}
            loading="lazy"
            onLoad={() => setImgLoaded(true)}
            onError={e => {
              const original = imgOriginal(subject.image)
              if (e.currentTarget.src !== original) e.currentTarget.src = original
              else setImgFailed(true)
            }}
            className="h-full w-full object-cover transition duration-300 group-hover:scale-[1.03]"
          />
        ) : (
          <div className="flex h-full items-center justify-center text-xs text-muted-foreground">无封面</div>
        )}
        <div className="absolute left-1.5 top-1.5 flex gap-1">
          <Badge className="bg-black/70 px-1.5 py-0 text-[10px] font-medium text-white backdrop-blur hover:bg-black/70">
            {TYPE_LABEL[subject.type] ?? '其他'}
          </Badge>
          {subject.nsfw && (
            <Badge variant="destructive" className="px-1.5 py-0 text-[10px]">
              敏感
            </Badge>
          )}
        </div>
        {subject.rank > 0 && subject.rank <= 1000 && (
          <div className="absolute right-1.5 top-1.5 inline-flex items-center gap-0.5 rounded bg-black/70 px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-amber-300 backdrop-blur">
            <Trophy size={9} />#{subject.rank}
          </div>
        )}
      </div>

      <div className="mt-2 min-w-0 flex-1">
        <h3 className="line-clamp-2 text-[13.5px] font-medium leading-snug" title={title}>
          {title}
        </h3>
        {alt && <p className="mt-0.5 line-clamp-1 text-[11px] text-muted-foreground" title={alt}>{alt}</p>}
        <div className="mt-1 flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
          <span className="tabular-nums">{subject.date ? yearOf(subject.date) : '—'}</span>
          <span className="inline-flex items-baseline gap-1">
            {subject.score > 0 ? (
              <>
                <b className="text-[12.5px] font-semibold tabular-nums text-foreground">{subject.score.toFixed(1)}</b>
                <span className="tabular-nums">{fmtNum(subject.rating_total)}人</span>
              </>
            ) : (
              <span>暂无评分</span>
            )}
          </span>
        </div>
        {subject.tags && subject.tags.length > 0 && (
          <div className="mt-1.5 flex flex-wrap gap-1">
            {subject.tags.slice(0, 4).map(t => (
              <span
                key={t.name}
                className="inline-flex max-w-full items-center gap-1 truncate rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground"
                title={`${t.name}：${t.votes} 票，占该条目标签票数 ${t.pct.toFixed(1)}%`}
              >
                <span className="truncate">{t.name}</span>
                <span className="tabular-nums opacity-70">{t.pct.toFixed(0)}%</span>
              </span>
            ))}
          </div>
        )}
      </div>
    </Link>
  )
}

export function SubjectCardSkeleton() {
  return (
    <div>
      <Skeleton className="aspect-[5/7] w-full rounded-lg" />
      <Skeleton className="mt-2 h-4 w-4/5" />
      <Skeleton className="mt-1.5 h-3 w-2/5" />
    </div>
  )
}
