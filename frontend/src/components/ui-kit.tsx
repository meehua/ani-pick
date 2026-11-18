import type { ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { AlertCircle, ArrowRight, RefreshCw, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { ApiError } from '../api/client'

export function PageHead({
  title,
  description,
  aside,
  back,
  className,
}: {
  title: ReactNode
  description?: ReactNode
  aside?: ReactNode
  back?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex flex-wrap items-start justify-between gap-4 pb-4', className)}>
      <div className="min-w-0">
        {back}
        <h1 className="text-[22px] font-semibold leading-tight tracking-tight sm:text-[24px]">
          {title}
        </h1>
        {description && (
          <p className="mt-1 max-w-3xl text-[13px] leading-relaxed text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {aside && <div className="shrink-0">{aside}</div>}
    </div>
  )
}

export function Notice({
  tone = 'info',
  children,
  className,
}: {
  tone?: 'info' | 'success' | 'error'
  children: ReactNode
  className?: string
}) {
  return (
    <Alert
      role={tone === 'error' ? 'alert' : 'status'}
      className={cn(
        tone === 'error' && 'border-destructive/25 bg-destructive/5 text-destructive',
        tone === 'success' && 'border-success/25 bg-success-soft text-success',
        tone === 'info' && 'border-border bg-card text-muted-foreground',
        className,
      )}
    >
      <AlertDescription>{children}</AlertDescription>
    </Alert>
  )
}

export function EmptyState({
  title,
  hint,
  action,
  className,
}: {
  title: string
  hint?: string
  action?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex flex-col items-center justify-center px-6 py-16 text-center', className)}>
      <p className="text-[14px] font-medium">{title}</p>
      {hint && <p className="mt-1 max-w-md text-[12.5px] leading-relaxed text-muted-foreground">{hint}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

// 失败提示：把 ApiError 的 hint 也显示出来，避免只给一个红色标题。
export function ErrorNote({
  error,
  onRetry,
  className,
}: {
  error: unknown
  onRetry?: () => void
  className?: string
}) {
  const hint = error instanceof ApiError ? error.hint : undefined
  const message = error instanceof Error ? error.message : '请求失败，请稍后重试'
  return (
    <div className={cn('flex items-start gap-3 rounded-lg border border-destructive/25 bg-destructive/5 p-3', className)}>
      <AlertCircle size={16} className="mt-0.5 shrink-0 text-destructive" aria-hidden />
      <div className="min-w-0 flex-1">
        <p className="text-[13px] font-medium text-destructive">{message}</p>
        {hint && <p className="mt-0.5 text-[12.5px] leading-relaxed text-muted-foreground">{hint}</p>}
      </div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-border bg-card px-2 text-[12px] transition-colors hover:border-border-strong"
        >
          <RefreshCw size={12} aria-hidden /> 重试
        </button>
      )}
    </div>
  )
}

// 可移除 chip：secondary Badge + 右侧 ×。筛选回显、标签选择、排除标签共用。
export function RemovableChip({
  label,
  onRemove,
  removeLabel,
  maxWidth = 'max-w-36',
}: {
  label: string
  onRemove: () => void
  removeLabel?: string
  maxWidth?: string
}) {
  return (
    <Badge variant="secondary" className="gap-1 pr-1 font-normal">
      <span className={cn('truncate', maxWidth)}>{label}</span>
      <button
        type="button"
        aria-label={removeLabel ?? `移除 ${label}`}
        onClick={onRemove}
        className="rounded-sm p-0.5 text-muted-foreground hover:bg-background hover:text-foreground"
      >
        <X size={11} aria-hidden />
      </button>
    </Badge>
  )
}

export function RouteLoading() {
  return (
    <div className="space-y-3 py-6" aria-busy="true" aria-label="加载中">
      <Skeleton className="h-6 w-40" />
      <Skeleton className="h-10 rounded-lg" />
      <div className="divide-y divide-border rounded-lg border bg-card">
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 p-3">
            <Skeleton className="h-12 w-9" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-3.5 w-1/3" />
              <Skeleton className="h-3 w-1/5" />
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

export function BackLink({ to, children }: { to: string; children: ReactNode }) {
  const navigate = useNavigate()
  const location = useLocation()
  const hasHistory = location.key !== 'default'

  return (
    <button
      type="button"
      onClick={() => (hasHistory ? navigate(-1) : navigate(to))}
      className="mb-2 inline-flex items-center gap-1 text-[12.5px] text-muted-foreground transition-colors hover:text-foreground"
    >
      <ArrowRight size={12} className="rotate-180" aria-hidden />
      {children}
    </button>
  )
}
