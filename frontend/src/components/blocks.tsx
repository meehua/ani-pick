import type { ReactNode } from 'react'
import { CircleAlert, SearchX, RotateCcw, ChevronRight } from 'lucide-react'
import { Link } from 'react-router-dom'
import { ApiError } from '../api/client'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { cn } from 'cn'

export function RatingHistogram({ counts, total }: { counts: number[]; total: number }) {
  const max = Math.max(...counts, 1)
  return (
    <div className="space-y-1.5" role="img" aria-label={`评分分布共 ${total} 人`}>
      {counts.map((count, i) => {
        const star = i + 1
        return (
          <div key={star} className="flex items-center gap-2.5">
            <span className="w-8 shrink-0 text-right text-xs tabular-nums text-muted-foreground">{star}★</span>
            <div className="h-3 flex-1 overflow-hidden rounded-sm bg-muted">
              <div
                className={cn(
                  'h-full min-w-px rounded-sm bg-primary transition-[width] duration-500',
                  star >= 8 && 'bg-emerald-500',
                  star <= 4 && 'bg-red-400',
                )}
                style={{ width: `${(count / max) * 100}%` }}
              />
            </div>
            <span className="w-12 shrink-0 text-xs tabular-nums text-muted-foreground">{count}</span>
          </div>
        )
      })}
      <p className="pt-1.5 text-xs text-muted-foreground">共 {total.toLocaleString()} 人评分</p>
    </div>
  )
}

// 标签认可度占比：该标签票数 ÷ 该条目全部标签票数（pct 0-100）
export function TagProportion({
  tags,
  max = 14,
}: {
  tags: { name: string; votes: number; pct: number }[]
  max?: number
}) {
  const list = tags.slice(0, max)
  const maxPct = Math.max(...list.map(t => t.pct), 0.01)
  return (
    <div className="space-y-2">
      {list.map(t => (
        <div key={t.name} className="flex items-center gap-2.5">
          <span className="w-[104px] shrink-0 truncate text-[12.5px]" title={t.name}>{t.name}</span>
          <div className="h-2 flex-1 overflow-hidden rounded-sm bg-muted">
            <div
              className="h-full min-w-0.5 rounded-sm bg-primary transition-[width] duration-500"
              style={{ width: `${(t.pct / maxPct) * 100}%` }}
            />
          </div>
          <span className="w-[88px] shrink-0 text-right text-xs tabular-nums">
            {t.pct.toFixed(1)}%
            <span className="block text-[10px] text-muted-foreground">{t.votes} 票</span>
          </span>
        </div>
      ))}
      {tags.length > max && (
        <p className="pt-1 text-xs text-muted-foreground">还有 {tags.length - max} 个标签未展示</p>
      )}
    </div>
  )
}

export function ErrorView({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const apiErr = error instanceof ApiError ? error : null
  return (
    <div className="py-16 text-center">
      <CircleAlert size={28} strokeWidth={1.6} className="mx-auto mb-3 text-muted-foreground" />
      <p className="mb-1 text-[15px] font-semibold">{apiErr?.message ?? '加载失败'}</p>
      {apiErr?.hint && <p className="mx-auto mb-4 max-w-md text-xs text-muted-foreground">{apiErr.hint}</p>}
      {onRetry && apiErr && apiErr.status !== 404 && (
        <Button variant="outline" size="sm" onClick={onRetry}>
          <RotateCcw size={13} /> 重试
        </Button>
      )}
    </div>
  )
}

export function EmptySearch({ keyword }: { keyword?: string }) {
  return (
    <div className="py-16 text-center">
      <SearchX size={28} strokeWidth={1.6} className="mx-auto mb-3 text-muted-foreground" />
      <p className="mb-1 text-[15px] font-semibold">本站索引中暂无匹配结果</p>
      <p className="mb-4 text-xs text-muted-foreground">
        {keyword ? '换个关键词，或放宽标签 / 日期 / 排名条件。' : '尝试放宽筛选条件，或稍后再来（索引会持续更新）。'}
      </p>
      <Link to="/browse" className="text-sm text-primary underline-offset-4 hover:underline">
        重置筛选
      </Link>
    </div>
  )
}

export function Notice({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <Alert className={cn('mb-4', className)}>
      <AlertDescription className="text-xs text-muted-foreground">{children}</AlertDescription>
    </Alert>
  )
}

export function SectionTitle({ children, extra }: { children: ReactNode; extra?: ReactNode }) {
  return (
    <div className="mb-4 mt-8 flex flex-wrap items-center justify-between gap-3 first:mt-0">
      <h2 className="text-lg font-semibold tracking-tight">{children}</h2>
      {extra}
    </div>
  )
}

export function MoreLink({ to }: { to: string }) {
  return (
    <Link
      to={to}
      className="inline-flex items-center gap-0.5 text-xs text-muted-foreground hover:text-foreground"
    >
      查看更多 <ChevronRight size={12} />
    </Link>
  )
}
