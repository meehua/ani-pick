import * as React from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  ArrowDownWideNarrow, ArrowUpNarrowWide, ChevronLeft, ChevronRight, Filter, RotateCcw, Search, X,
} from 'lucide-react'
import { cn } from 'cn'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { SubjectCard, SubjectCardSkeleton } from '@/components/subject-card'
import { EmptySearch, ErrorView } from '@/components/blocks'
import { TagPicker } from '@/components/tag-picker'
import { MultiCombobox } from '@/components/combobox'
import { DateRangePicker } from '@/components/date-range'
import { getMetaTags, searchSubjects } from '../api/client'
import { TYPE_LABEL, TYPE_ORDER, type SearchResult, type TagStat } from '../api/types'

const SORTS = [
  { value: 'rank', label: '全站排名' },
  { value: 'score', label: '评分' },
  { value: 'rating_total', label: '评分人数' },
  { value: 'date', label: '开播 / 发售日期' },
  { value: 'collect', label: '看过人数' },
  { value: 'wish', label: '想看人数' },
  { value: 'doing', label: '在看人数' },
  { value: 'id', label: '收录顺序' },
]

const NSFW_OPTIONS = [
  { value: 'hide', label: '不含敏感内容' },
  { value: 'include', label: '包含敏感内容' },
  { value: 'only', label: '仅敏感内容' },
]

function csv(sp: URLSearchParams, key: string): string[] {
  return (sp.get(key) ?? '').split(',').map(s => s.trim()).filter(Boolean)
}

function num(sp: URLSearchParams, key: string): number | undefined {
  const v = sp.get(key)
  if (!v) return undefined
  const n = Number(v)
  return Number.isFinite(n) ? n : undefined
}

function NumField({
  value, placeholder, onCommit, min, max,
}: {
  value?: number
  placeholder: string
  onCommit: (v: number | undefined) => void
  min?: number
  max?: number
}) {
  const [text, setText] = React.useState(value === undefined ? '' : String(value))
  React.useEffect(() => {
    setText(value === undefined ? '' : String(value))
  }, [value])
  const commit = () => {
    const t = text.trim()
    if (t === '') {
      onCommit(undefined)
      return
    }
    const n = Number(t)
    if (!Number.isFinite(n)) {
      setText(value === undefined ? '' : String(value))
      return
    }
    onCommit(min !== undefined || max !== undefined
      ? Math.min(max ?? Number.MAX_SAFE_INTEGER, Math.max(min ?? 0, n))
      : n)
  }
  return (
    <Input
      value={text}
      inputMode="numeric"
      placeholder={placeholder}
      onChange={e => setText(e.target.value.replace(/[^\d.]/g, ''))}
      onBlur={commit}
      onKeyDown={e => {
        if (e.key === 'Enter') {
          e.preventDefault()
          commit()
        }
      }}
      className="h-8 text-xs tabular-nums"
    />
  )
}

export default function BrowsePage() {
  const [sp, setSp] = useSearchParams()
  const key = sp.toString()
  const [data, setData] = React.useState<SearchResult | null>(null)
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<unknown>(null)
  const [filtersOpen, setFiltersOpen] = React.useState(false)
  const [keyword, setKeyword] = React.useState(sp.get('keyword') ?? '')
  const [metaTags, setMetaTags] = React.useState<TagStat[]>([])

  const types = React.useMemo(() => csv(sp, 'types').map(Number).filter(n => !Number.isNaN(n)), [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const tags = React.useMemo(() => csv(sp, 'tags'), [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const exclude = React.useMemo(() => csv(sp, 'exclude'), [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const selectedMeta = React.useMemo(() => csv(sp, 'meta_tags'), [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const tagMode = sp.get('tag_mode') ?? 'and'
  const nsfw = sp.get('nsfw') ?? 'hide'
  const sort = sp.get('sort') ?? 'rank'
  const order = sp.get('order') ?? (sort === 'rank' ? 'asc' : 'desc')
  const page = num(sp, 'page') ?? 1
  const pctMin = num(sp, 'pct_min')
  const dateFrom = sp.get('date_from') ?? undefined
  const dateTo = sp.get('date_to') ?? undefined
  const rankMin = num(sp, 'rank_min')
  const rankMax = num(sp, 'rank_max')
  const scoreMin = num(sp, 'score_min')
  const scoreMax = num(sp, 'score_max')
  const ratingMin = num(sp, 'rating_min')

  const patch = React.useCallback(
    (next: Record<string, string | number | (string | number)[] | undefined>, resetPage = true) => {
      const p = new URLSearchParams(key)
      for (const [k, v] of Object.entries(next)) {
        const empty =
          v === undefined || v === '' || (Array.isArray(v) && v.length === 0) ||
          (typeof v === 'number' && v === 0)
        if (empty) p.delete(k)
        else p.set(k, Array.isArray(v) ? v.join(',') : String(v))
      }
      if (resetPage) p.delete('page')
      setSp(p, { replace: true })
    },
    [key, setSp],
  )

  React.useEffect(() => {
    setKeyword(sp.get('keyword') ?? '')
  }, [key]) // eslint-disable-line react-hooks/exhaustive-deps

  React.useEffect(() => {
    const ctrl = new AbortController()
    const timer = window.setTimeout(async () => {
      setLoading(true)
      setError(null)
      try {
        const res = await searchSubjects({
          keyword: sp.get('keyword') ?? undefined,
          types,
          tags,
          exclude,
          tag_mode: tagMode,
          pct_min: pctMin,
          meta_tags: selectedMeta,
          date_from: dateFrom,
          date_to: dateTo,
          rank_min: rankMin,
          rank_max: rankMax,
          score_min: scoreMin,
          score_max: scoreMax,
          rating_min: ratingMin,
          nsfw,
          sort,
          order,
          page,
          page_size: 24,
        }, ctrl.signal)
        setData(res)
      } catch (e) {
        if ((e as Error).name !== 'AbortError') setError(e)
      } finally {
        setLoading(false)
      }
    }, 120)
    return () => {
      window.clearTimeout(timer)
      ctrl.abort()
    }
  }, [key]) // eslint-disable-line react-hooks/exhaustive-deps

  React.useEffect(() => {
    const ctrl = new AbortController()
    getMetaTags(types.length === 1 ? types[0] : 0, ctrl.signal)
      .then(r => setMetaTags(r.items))
      .catch(() => undefined)
    return () => ctrl.abort()
  }, [types.join(',')]) // eslint-disable-line react-hooks/exhaustive-deps

  const totalPages = data ? Math.max(1, Math.ceil(data.total / data.page_size)) : 1
  const items = data?.items ?? []
  const hasFilters = ['keyword', 'types', 'tags', 'exclude', 'meta_tags', 'date_from', 'date_to',
    'rank_min', 'rank_max', 'score_min', 'score_max', 'rating_min', 'pct_min'].some(k => sp.get(k))

  const pageItems = paginationWindow(page, totalPages)

  const toggleType = (t: number) => {
    const next = types.includes(t) ? types.filter(x => x !== t) : [...types, t]
    patch({ types: next })
  }

  return (
    <div className="flex flex-col gap-4 lg:flex-row lg:gap-6">
      <aside className={cn('w-full shrink-0 lg:w-64', !filtersOpen && 'hidden lg:block')}>
        <div className="space-y-4 rounded-lg border p-4 lg:sticky lg:top-20 lg:max-h-[calc(100dvh-6rem)] lg:overflow-y-auto">
          <div className="flex items-center justify-between">
            <p className="text-sm font-medium">筛选</p>
            <div className="flex items-center gap-1">
              <Button
                variant="ghost" size="sm" className="h-7 px-2 text-xs"
                disabled={!hasFilters}
                onClick={() => setSp(new URLSearchParams(), { replace: true })}
              >
                <RotateCcw size={12} /> 重置
              </Button>
              <Button
                variant="ghost" size="icon" className="size-7 lg:hidden" aria-label="关闭筛选"
                onClick={() => setFiltersOpen(false)}
              >
                <X size={14} />
              </Button>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">作品类型</Label>
            <div className="flex flex-wrap gap-1.5">
              {TYPE_ORDER.map(t => (
                <button
                  key={t}
                  type="button"
                  onClick={() => toggleType(t)}
                  className={cn(
                    'rounded-md border px-2 py-1 text-xs transition-colors',
                    types.includes(t)
                      ? 'border-primary bg-primary text-primary-foreground'
                      : 'hover:bg-muted',
                  )}
                  aria-pressed={types.includes(t)}
                >
                  {TYPE_LABEL[t]}
                </button>
              ))}
            </div>
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <Label className="text-xs text-muted-foreground">标签</Label>
              <div className="flex rounded-md border p-0.5 text-[10px]">
                {[{ v: 'and', l: '全部' }, { v: 'or', l: '任一' }].map(m => (
                  <button
                    key={m.v}
                    type="button"
                    className={cn('rounded px-1.5 py-0.5', tagMode === m.v ? 'bg-secondary' : 'text-muted-foreground')}
                    onClick={() => patch({ tag_mode: m.v === 'and' ? undefined : m.v })}
                  >
                    {m.l}
                  </button>
                ))}
              </div>
            </div>
            <TagPicker
              value={tags}
              onChange={v => patch({ tags: v })}
              types={types}
              placeholder="选择标签…"
              ariaLabel="标签"
            />
            <div className="flex items-center gap-1.5 pt-0.5">
              <span className="shrink-0 text-[11px] text-muted-foreground">占比 ≥</span>
              <NumField
                value={pctMin}
                placeholder="不限"
                min={0}
                max={100}
                onCommit={v => patch({ pct_min: v })}
              />
              <span className="text-[11px] text-muted-foreground">%</span>
            </div>
            <p className="text-[10px] leading-relaxed text-muted-foreground">
              占比 = 该标签票数 ÷ 条目标签总票数；设置后只有达到门槛的标签才算命中。
            </p>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">排除标签</Label>
            <TagPicker
              value={exclude}
              onChange={v => patch({ exclude: v })}
              types={types}
              placeholder="不含这些标签…"
              ariaLabel="排除标签"
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">分类标签</Label>
            <MultiCombobox
              value={selectedMeta}
              onChange={v => patch({ meta_tags: v })}
              options={metaTags.map(m => ({ label: m.name, value: m.name, hint: String(m.count) }))}
              placeholder="TV / 剧场版 / 漫画改…"
              allowCreate={false}
              className="min-h-8 text-xs"
              ariaLabel="分类标签"
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">日期范围</Label>
            <DateRangePicker
              from={dateFrom}
              to={dateTo}
              onChange={(from, to) => patch({ date_from: from, date_to: to })}
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">全站排名</Label>
            <div className="flex items-center gap-1.5">
              <NumField value={rankMin} placeholder="最小" min={1} onCommit={v => patch({ rank_min: v })} />
              <span className="text-xs text-muted-foreground">–</span>
              <NumField value={rankMax} placeholder="最大" min={1} onCommit={v => patch({ rank_max: v })} />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">评分 / 评分人数</Label>
            <div className="flex items-center gap-1.5">
              <NumField value={scoreMin} placeholder="最低分" min={0} max={10} onCommit={v => patch({ score_min: v })} />
              <span className="text-xs text-muted-foreground">–</span>
              <NumField value={scoreMax} placeholder="最高分" min={0} max={10} onCommit={v => patch({ score_max: v })} />
            </div>
            <NumField value={ratingMin} placeholder="至少多少人评分" min={0} onCommit={v => patch({ rating_min: v })} />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">内容范围</Label>
            <Select value={nsfw} onValueChange={v => patch({ nsfw: v === 'hide' ? undefined : v })}>
              <SelectTrigger className="h-8 w-full text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                {NSFW_OPTIONS.map(o => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
        </div>
      </aside>

      <section className="min-w-0 flex-1">
        <div className="mb-4 flex flex-wrap items-center gap-2">
          <Button
            variant="outline" size="sm" className="lg:hidden"
            onClick={() => setFiltersOpen(v => !v)}
          >
            <Filter size={13} /> 筛选
            {hasFilters && <Badge variant="secondary" className="ml-1 px-1 text-[10px]">已启用</Badge>}
          </Button>

          <form
            className="flex min-w-0 flex-1 items-center gap-1.5"
            onSubmit={e => {
              e.preventDefault()
              patch({ keyword: keyword.trim() || undefined })
            }}
          >
            <div className="relative min-w-0 flex-1">
              <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={keyword}
                onChange={e => setKeyword(e.target.value)}
                placeholder="在本站索引中搜索作品名"
                className="h-8 pl-8 text-xs"
                aria-label="关键词"
              />
            </div>
            <Button type="submit" size="sm" variant="secondary" className="h-8">搜索</Button>
          </form>

          <Select value={sort} onValueChange={v => patch({ sort: v === 'rank' ? undefined : v, order: undefined })}>
            <SelectTrigger className="h-8 w-[130px] text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {SORTS.map(s => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <Button
            variant="outline" size="icon" className="size-8"
            aria-label={order === 'asc' ? '升序' : '降序'}
            title={order === 'asc' ? '升序' : '降序'}
            onClick={() => patch({ order: order === 'asc' ? 'desc' : 'asc' })}
          >
            {order === 'asc' ? <ArrowUpNarrowWide size={14} /> : <ArrowDownWideNarrow size={14} />}
          </Button>
        </div>

        <div className="mb-3 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
          {loading && !data ? '检索中…' : data ? `共 ${data.total.toLocaleString()} 条结果` : ''}
          {data && totalPages > 1 && <span>· 第 {page} / {totalPages} 页</span>}
          {tags.length > 0 && (
            <span>· 标签{pctMin ? `（占比 ≥ ${pctMin}%）` : ''}：{tags.join(' + ')}</span>
          )}
        </div>

        {error ? (
          <ErrorView error={error} onRetry={() => setSp(new URLSearchParams(key), { replace: true })} />
        ) : loading && !data ? (
          <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {Array.from({ length: 15 }).map((_, i) => <SubjectCardSkeleton key={i} />)}
          </div>
        ) : items.length === 0 ? (
          <EmptySearch keyword={sp.get('keyword') ?? undefined} />
        ) : (
          <div className={cn('grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5',
            loading && 'pointer-events-none opacity-60')}>
            {items.map(s => <SubjectCard key={s.id} subject={s} />)}
          </div>
        )}

        {data && totalPages > 1 && (
          <div className="mt-8 flex flex-wrap items-center justify-center gap-1">
            <Button
              variant="outline" size="icon" className="size-8" aria-label="上一页"
              disabled={page <= 1} onClick={() => patch({ page: page - 1 }, false)}
            >
              <ChevronLeft size={14} />
            </Button>
            {pageItems.map((p, i) =>
              p === '…' ? (
                <span key={`gap-${i}`} className="px-1 text-xs text-muted-foreground">…</span>
              ) : (
                <Button
                  key={p}
                  variant={p === page ? 'default' : 'outline'}
                  size="icon"
                  className="size-8 text-xs tabular-nums"
                  onClick={() => patch({ page: p === 1 ? undefined : p }, false)}
                >
                  {p}
                </Button>
              ),
            )}
            <Button
              variant="outline" size="icon" className="size-8" aria-label="下一页"
              disabled={page >= totalPages} onClick={() => patch({ page: page + 1 }, false)}
            >
              <ChevronRight size={14} />
            </Button>
          </div>
        )}
      </section>
    </div>
  )
}

function paginationWindow(page: number, totalPages: number): (number | '…')[] {
  if (totalPages <= 7) return Array.from({ length: totalPages }, (_, i) => i + 1)
  const out: (number | '…')[] = [1]
  const start = Math.max(2, page - 1)
  const end = Math.min(totalPages - 1, page + 1)
  if (start > 2) out.push('…')
  for (let i = start; i <= end; i++) out.push(i)
  if (end < totalPages - 1) out.push('…')
  out.push(totalPages)
  return out
}
