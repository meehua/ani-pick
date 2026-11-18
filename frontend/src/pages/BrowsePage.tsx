import * as React from 'react'
import { useLocation, useSearchParams } from 'react-router-dom'
import {
  ArrowDownWideNarrow, ArrowUpNarrowWide, LayoutGrid, List, PanelLeftClose,
  SlidersHorizontal, Table2,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { ctl } from '@/lib/ctl'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Pagination, PaginationContent, PaginationItem } from '@/components/ui/pagination'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { PosterCard, DenseRow, SubjectTable } from '@/components/subject-views'
import { SearchInput } from '@/components/search-input'
import {
  ActiveFilters, FilterPanel, FilterSheet, type ActiveFilterState, type PatchFn,
} from '@/components/filters'
import { PageHead, EmptyState, ErrorNote } from '@/components/ui-kit'
import { getMetaTags, searchSubjects } from '../api/client'
import type { SearchResult, TagStat } from '../api/types'
import { loadPage, savePage } from '../lib/page-cache'
import { useScrollMemory } from '../lib/use-scroll-memory'

// 搜索框就在结果工具条里：输入即搜（350ms 防抖），回车立即；`/` 聚焦由 SearchInput 提供。
function BrowseSearch({
  keyword,
  onCommit,
}: {
  keyword: string
  onCommit: (v: string) => void
}) {
  const [value, setValue] = React.useState(keyword)

  React.useEffect(() => {
    setValue(keyword)
  }, [keyword])

  React.useEffect(() => {
    const timer = window.setTimeout(() => {
      if (value.trim() !== keyword) onCommit(value.trim())
    }, 350)
    return () => window.clearTimeout(timer)
  }, [value]) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <form
      className="relative min-w-0 flex-1 sm:max-w-xs"
      onSubmit={e => {
        e.preventDefault()
        onCommit(value.trim())
      }}
    >
      <SearchInput
        value={value}
        onChange={setValue}
        onClear={() => {
          setValue('')
          onCommit('')
        }}
        placeholder="搜索作品名或原名"
        inputClassName={ctl.field}
      />
    </form>
  )
}

const SORTS = [
  { value: 'rank', label: '全站排名' },
  { value: 'score', label: '评分' },
  { value: 'rating_total', label: '评分人数' },
  { value: 'date', label: '开播 / 发售' },
  { value: 'collect', label: '看过人数' },
  { value: 'wish', label: '想看人数' },
  { value: 'doing', label: '在看人数' },
  { value: 'id', label: '收录顺序' },
]

const VIEWS = [
  { value: 'grid', label: '网格', icon: LayoutGrid },
  { value: 'list', label: '列表', icon: List },
  { value: 'table', label: '表格', icon: Table2 },
] as const

type ViewMode = (typeof VIEWS)[number]['value']

function csv(sp: URLSearchParams, key: string): string[] {
  return (sp.get(key) ?? '').split(',').map(s => s.trim()).filter(Boolean)
}

function num(sp: URLSearchParams, key: string): number | undefined {
  const v = sp.get(key)
  if (!v) return undefined
  const n = Number(v)
  return Number.isFinite(n) ? n : undefined
}

export default function BrowsePage() {
  const [sp, setSp] = useSearchParams()
  const location = useLocation()
  const key = sp.toString()

  const mountHit = React.useMemo(() => loadPage<SearchResult>(location.key), []) // eslint-disable-line react-hooks/exhaustive-deps
  const [data, setData] = React.useState<SearchResult | null>(mountHit?.data ?? null)
  const [loading, setLoading] = React.useState(!mountHit?.data)
  const [error, setError] = React.useState<unknown>(null)
  const [sheetOpen, setSheetOpen] = React.useState(false)
  const [collapsed, setCollapsed] = React.useState(
    () => typeof localStorage !== 'undefined' && localStorage.getItem('anipick.filters.collapsed') === '1',
  )
  const [metaTags, setMetaTags] = React.useState<TagStat[]>([])
  const dataRef = React.useRef<SearchResult | null>(null)
  dataRef.current = data

  useScrollMemory(location.key, mountHit?.scrollTop ?? null)

  const types = React.useMemo(() => csv(sp, 'types').map(Number).filter(n => !Number.isNaN(n)), [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const tags = React.useMemo(() => csv(sp, 'tags'), [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const exclude = React.useMemo(() => csv(sp, 'exclude'), [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const selectedMeta = React.useMemo(() => csv(sp, 'meta_tags'), [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const tagMode: 'and' | 'or' = sp.get('tag_mode') === 'or' ? 'or' : 'and'
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
  const ratingMin = num(sp, 'rating_min')
  const view: ViewMode = VIEWS.some(v => v.value === sp.get('view'))
    ? (sp.get('view') as ViewMode)
    : 'grid'

  // 用函数式更新：连续改两个筛选（如先填起始再填结束）不会用旧 URL 覆盖前一次
  const patch = React.useCallback<PatchFn>(
    (next, resetPage = true) => {
      setSp(prev => {
        const p = new URLSearchParams(prev)
        for (const [k, v] of Object.entries(next)) {
          const empty =
            v === undefined ||
            v === '' ||
            (Array.isArray(v) && v.length === 0) ||
            (typeof v === 'number' && v === 0)
          if (empty) p.delete(k)
          else p.set(k, Array.isArray(v) ? v.join(',') : String(v))
        }
        if (resetPage) p.delete('page')
        return p
      }, { replace: true })
    },
    [setSp],
  )

  React.useEffect(() => {
    const ctrl = new AbortController()
    const timer = window.setTimeout(async () => {
      setLoading(true)
      setError(null)
      try {
        const res = await searchSubjects(
          {
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
            rating_min: ratingMin,
            nsfw,
            sort,
            order,
            page,
            page_size: 30,
          },
          ctrl.signal,
        )
        setData(res)
      } catch (e) {
        if ((e as Error).name !== 'AbortError') setError(e)
      } finally {
        setLoading(false)
      }
    }, 160)
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

  React.useEffect(() => {
    const cacheKey = location.key
    if (dataRef.current) savePage(cacheKey, dataRef.current)
    let timer: number | undefined
    const save = () => {
      if (dataRef.current) savePage(cacheKey, dataRef.current)
    }
    const onScroll = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(save, 250)
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
      window.clearTimeout(timer)
      save()
    }
  }, [location.key])

  const totalPages = data ? Math.max(1, Math.ceil(data.total / data.page_size)) : 1
  const items = data?.items ?? []

  const filters: ActiveFilterState = {
    keyword: sp.get('keyword') ?? undefined,
    types,
    tags,
    exclude,
    metaTags: selectedMeta,
    tagMode,
    pctMin,
    dateFrom,
    dateTo,
    rankMin,
    rankMax,
    scoreMin,
    ratingMin,
    nsfw,
  }

  const activeCount =
    (filters.keyword ? 1 : 0) +
    types.length +
    tags.length +
    exclude.length +
    selectedMeta.length +
    (pctMin ? 1 : 0) +
    (dateFrom || dateTo ? 1 : 0) +
    (rankMin || rankMax ? 1 : 0) +
    (scoreMin ? 1 : 0) +
    (ratingMin ? 1 : 0) +
    (nsfw !== 'hide' ? 1 : 0)

  const resetAll = () => setSp(new URLSearchParams(), { replace: true })
  const goPage = (p: number) => {
    patch({ page: p === 1 ? undefined : p }, false)
    window.scrollTo(0, 0)
  }

  function toggleCollapsed() {
    setCollapsed(c => {
      const next = !c
      localStorage.setItem('anipick.filters.collapsed', next ? '1' : '0')
      return next
    })
  }

  return (
    <div>
      <PageHead
        title="作品索引"
        description="按标签、评分、排名与年代筛选，条件写入 URL。"
      />

      <div className="flex gap-6">
        {!collapsed && (
          <aside className="hidden w-64 shrink-0 lg:block">
            <div className="sticky top-20 max-h-[calc(100dvh-6rem)] overflow-y-auto rounded-xl border bg-card">
              <div className="sticky top-0 z-10 flex items-center justify-between border-b bg-card px-4 py-3">
                <h2 className="text-sm font-semibold">筛选</h2>
                <div className="flex items-center gap-2">
                  {activeCount > 0 && (
                    <span className="num text-xs text-muted-foreground">{activeCount} 项</span>
                  )}
                  <Button
                    type="button"
                    variant="ghost"
                    size="xs"
                    onClick={resetAll}
                    disabled={activeCount === 0}
                  >
                    重置
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label="收起筛选"
                    title="收起筛选"
                    onClick={toggleCollapsed}
                  >
                    <PanelLeftClose size={16} aria-hidden />
                  </Button>
                </div>
              </div>
              <div className="p-4">
                <FilterPanel filters={filters} patch={patch} metaTagOptions={metaTags} />
              </div>
            </div>
          </aside>
        )}

        <section className="min-w-0 flex-1">
          <div className="sticky top-16 z-20 -mx-4 border-b bg-background/95 px-4 py-2 backdrop-blur sm:-mx-6 sm:px-6 lg:mx-0 lg:px-0">
            <div className="flex flex-wrap items-center gap-2 max-sm:grid max-sm:grid-cols-[auto_1fr]">
              {collapsed && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="hidden gap-1.5 lg:inline-flex"
                  onClick={toggleCollapsed}
                >
                  <SlidersHorizontal size={14} aria-hidden />
                  筛选
                  {activeCount > 0 && (
                    <span className="num text-[11px] text-muted-foreground">{activeCount}</span>
                  )}
                </Button>
              )}
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-11 gap-1.5 lg:hidden"
                onClick={() => setSheetOpen(true)}
              >
                <SlidersHorizontal size={14} aria-hidden />
                筛选
                {activeCount > 0 && (
                  <span className="num text-[11px] text-muted-foreground">{activeCount}</span>
                )}
              </Button>

              <BrowseSearch
                keyword={sp.get('keyword') ?? ''}
                onCommit={v => patch({ keyword: v || undefined })}
              />

              <div className="flex items-center gap-1.5 max-sm:col-span-2 max-sm:w-full">
                <Select
                  value={sort}
                  onValueChange={v => patch({ sort: v === 'rank' ? undefined : v, order: undefined })}
                >
                  <SelectTrigger className={cn(ctl.bar, 'w-[122px]')} aria-label="排序">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align="end">
                    {SORTS.map(s => (
                      <SelectItem key={s.value} value={s.value}>
                        {s.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>

                <Button
                  type="button"
                  variant="outline"
                  size="icon-sm"
                  className={ctl.square}
                  aria-label={order === 'asc' ? '升序' : '降序'}
                  title={order === 'asc' ? '升序' : '降序'}
                  onClick={() => patch({ order: order === 'asc' ? 'desc' : 'asc' })}
                >
                  {order === 'asc' ? (
                    <ArrowUpNarrowWide size={14} aria-hidden />
                  ) : (
                    <ArrowDownWideNarrow size={14} aria-hidden />
                  )}
                </Button>


              </div>

              <div className="ml-auto flex items-center gap-2 max-sm:col-span-2 max-sm:ml-0 max-sm:w-full max-sm:justify-end">
              <p className="hidden text-sm text-muted-foreground sm:block" aria-live="polite">
                {loading && !data ? '检索中…' : data ? `共 ${data.total.toLocaleString()} 条结果` : ''}
              </p>
                <ToggleGroup
                  type="single"
                  value={view}
                  onValueChange={v => {
                    if (v) patch({ view: v === 'grid' ? undefined : (v as ViewMode) }, false)
                  }}
                  aria-label="视图切换"
                  className="gap-0.5"
                >
                  {VIEWS.map(v => {
                    const Icon = v.icon
                    return (
                      <ToggleGroupItem
                        key={v.value}
                        value={v.value}
                        aria-label={`视图：${v.label}`}
                        title={`视图：${v.label}`}
                        className={cn(ctl.square, ctl.on)}
                      >
                        <Icon size={14} aria-hidden />
                      </ToggleGroupItem>
                    )
                  })}
                </ToggleGroup>
              </div>
            </div>

            {activeCount > 0 && (
              <div className={collapsed ? 'mt-2' : 'mt-2 lg:hidden'}>
                <ActiveFilters filters={filters} patch={patch} onResetAll={resetAll} />
              </div>
            )}
          </div>

          <div className="mb-3 mt-3 flex items-center justify-between gap-2">
            <span className="num text-xs text-muted-foreground" aria-live="polite">
              {loading && !data ? (
                '检索中…'
              ) : data ? (
                <>
                  <span className="sm:hidden">共 {data.total.toLocaleString()} 条 · </span>第 {page} /{' '}
                  {totalPages} 页
                </>
              ) : (
                ''
              )}
            </span>
            <span className="hidden text-xs text-muted-foreground sm:inline">
              每页 {data?.page_size ?? 30} 条
            </span>
          </div>

          {error ? (
            <ErrorNote error={error} onRetry={() => setSp(new URLSearchParams(key), { replace: true })} />
          ) : loading && !data ? (
            <div className="grid grid-cols-2 gap-x-3 gap-y-6 sm:grid-cols-3 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
              {Array.from({ length: 12 }).map((_, i) => (
                <div key={i} className="space-y-2">
                  <Skeleton className="aspect-[5/7] w-full rounded-md" />
                  <Skeleton className="h-3 w-3/4" />
                </div>
              ))}
            </div>
          ) : items.length === 0 ? (
            <EmptyState
              title="没有符合条件的条目"
              hint={
                sp.get('keyword')
                  ? `没有找到与「${sp.get('keyword')}」匹配的记录。换一个关键词，或放宽筛选条件。`
                  : '尝试减少标签、放宽占比门槛或清除年代限制。'
              }
              action={
                activeCount > 0 ? (
                  <Button size="sm" variant="outline" onClick={resetAll}>
                    清空筛选
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <div className={cn('transition-opacity', loading && 'pointer-events-none opacity-50')}>
              {view === 'grid' && (
                <div
                  data-testid="subject-grid"
                  className={cn(
                    'grid grid-cols-2 gap-x-3 gap-y-6 sm:grid-cols-3 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5',
                  )}
                >
                  {items.map(s => (
                    <PosterCard key={s.id} subject={s} />
                  ))}
                </div>
              )}
              {view === 'list' && (
                <div className="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-x-6">
                  {items.map(s => (
                    <DenseRow key={s.id} subject={s} />
                  ))}
                </div>
              )}
              {view === 'table' && <SubjectTable subjects={items} />}
            </div>
          )}

          {data && totalPages > 1 && (
            <Pagination className="mt-8 border-t pt-4">
              <PaginationContent>
                <PaginationItem>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={ctl.field}
                    disabled={page <= 1}
                    onClick={() => goPage(page - 1)}
                  >
                    上一页
                  </Button>
                </PaginationItem>
                <PaginationItem>
                  <span className="num px-2 text-xs text-muted-foreground">
                    第 {page} / {totalPages} 页
                  </span>
                </PaginationItem>
                <PaginationItem>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={ctl.field}
                    disabled={page >= totalPages}
                    onClick={() => goPage(page + 1)}
                  >
                    下一页
                  </Button>
                </PaginationItem>
              </PaginationContent>
            </Pagination>
          )}
        </section>
      </div>

      <FilterSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        filters={filters}
        patch={patch}
        metaTagOptions={metaTags}
        total={data?.total}
        onResetAll={resetAll}
      />
    </div>
  )
}
