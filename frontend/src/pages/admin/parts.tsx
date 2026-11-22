import * as React from 'react'
import { Square } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableHeader, TableRow } from '@/components/ui/table'
import { Th, Td } from '@/components/table-bits'
import { Panel } from '@/components/panel'
import type { AdminOverview, CrawlProgress, Run } from '../../api/types'
import { adminRuns } from '../../api/client'
import { manifestSources, modeLabel, sourceLabel, typeLabel } from '../../api/vocab'
import { fmtDateTime, STATUS_LABEL } from '../../lib/format'

export function Stat({
  label,
  value,
  sub,
}: {
  label: string
  value: React.ReactNode
  sub?: string
}) {
  return (
    <Panel>
      <p className="truncate text-xs text-muted-foreground">{label}</p>
      <p className="num mt-2 truncate text-xl font-semibold leading-none">{value}</p>
      {sub && <p className="mt-1.5 truncate text-[11px] text-muted-foreground">{sub}</p>}
    </Panel>
  )
}

export function CrawlStatusCard({
  status,
  onCancel,
}: {
  status: AdminOverview['crawler'] | null
  onCancel: () => void
}) {
  if (!status) return null
  const p = status.progress
  const pct = p.total > 0 ? Math.min(100, Math.round((p.offset / p.total) * 100)) : undefined

  return (
    <Panel
      title={
        <span className="flex items-center gap-2">
          抓取状态
          <Badge variant={status.running ? 'default' : 'secondary'} className="font-normal">
            {status.running ? modeLabel(status.mode, status.progress?.source) : '空闲'}
          </Badge>
          {status.running && status.trigger === 'auto' && (
            <Badge variant="outline" className="font-normal">
              自动
            </Badge>
          )}
        </span>
      }
      action={
        status.running && (
          <Button type="button" size="sm" variant="outline" onClick={onCancel} className="gap-1.5">
            <Square size={12} aria-hidden /> 停止
          </Button>
        )
      }
    >
      {/* 空闲时只留一行摘要，不占整块面板（密度红线：消灭大块垂直空白） */}
      {!status.running && (
        <p className="text-xs text-muted-foreground">
          没有正在运行的抓取任务；启动入口在上方「启动抓取」，每个来源的按钮在自己卡里。
        </p>
      )}
      {status.running && (
        <>
          <div className="flex items-center gap-2">
            {/* 总数未知时进度不可算：占三分之一宽度并脉动示意 */}
            <Progress
              value={pct ?? 33}
              aria-label="抓取进度"
              className={cn('h-1.5 flex-1', pct === undefined && 'animate-pulse')}
            />
            <span className="num w-10 text-right text-xs text-muted-foreground">
              {pct === undefined ? '—' : `${pct}%`}
            </span>
          </div>

          <dl className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
        <div className="min-w-0">
          <dt className="text-[11px] text-muted-foreground">阶段</dt>
          <dd className="mt-0.5 truncate text-sm">{p.phase || '—'}</dd>
        </div>
        {p.source && (
          <div className="min-w-0">
            <dt className="text-[11px] text-muted-foreground">来源</dt>
            <dd className="mt-0.5 truncate text-sm">{sourceLabel(p.source)}</dd>
          </div>
        )}
        {p.type > 0 && (
          <div className="min-w-0">
            <dt className="text-[11px] text-muted-foreground">类型</dt>
            <dd className="mt-0.5 truncate text-sm">{typeLabel(p.type, p.source)}</dd>
          </div>
        )}
        <div className="min-w-0">
          <dt className="text-[11px] text-muted-foreground">进度</dt>
          <dd className="num mt-0.5 truncate text-sm">
            {p.total > 0 ? `${p.offset}/${p.total}` : '—'}
          </dd>
        </div>
        <div className="min-w-0">
          <dt className="text-[11px] text-muted-foreground">请求</dt>
          <dd className="num mt-0.5 text-sm">{p.requests}</dd>
        </div>
        <div className="min-w-0">
          <dt className="text-[11px] text-muted-foreground">新增</dt>
          <dd className="num mt-0.5 text-sm">{p.inserted}</dd>
        </div>
        <div className="min-w-0">
          <dt className="text-[11px] text-muted-foreground">更新</dt>
          <dd className="num mt-0.5 text-sm">{p.updated}</dd>
        </div>
        <div className="min-w-0">
          <dt className="text-[11px] text-muted-foreground">重爬</dt>
          <dd className="num mt-0.5 text-sm">{p.refreshed}</dd>
        </div>
        {p.pruned > 0 && (
          <div className="min-w-0">
            <dt className="text-[11px] text-muted-foreground">清理</dt>
            <dd className="num mt-0.5 text-sm">{p.pruned}</dd>
          </div>
        )}
        {p.errors > 0 && (
          <div className="min-w-0 text-destructive">
            <dt className="text-[11px]">错误</dt>
            <dd className="num mt-0.5 text-sm">{p.errors}</dd>
          </div>
        )}
        {p.skipped > 0 && (
          <div className="min-w-0">
            <dt className="text-[11px] text-muted-foreground">跳过</dt>
            <dd className="num mt-0.5 text-sm">{p.skipped}</dd>
          </div>
        )}
        {status.running && status.started_at && (
          <div className="min-w-0">
            <dt className="text-[11px] text-muted-foreground">开始</dt>
            <dd className="num mt-0.5 truncate text-sm">{fmtDateTime(status.started_at)}</dd>
          </div>
        )}
          </dl>
          {p.message && <p className="mt-2 text-xs text-muted-foreground">{p.message}</p>}
        </>
      )}
    </Panel>
  )
}

// 移动端表格替代：窄屏下的两行式列表——首行标题 + 右侧状态，次行等宽数字摘要。
// 管理端各表格共用；桌面宽度下不渲染（sm:hidden）。
export function MobileRows({
  rows,
}: {
  rows: {
    key: React.Key
    head: React.ReactNode
    side?: React.ReactNode
    stats?: React.ReactNode[]
  }[]
}) {
  if (rows.length === 0) return null
  return (
    <div className="divide-y sm:hidden">
      {rows.map(r => (
        <div key={r.key} className="px-4 py-2.5">
          <div className="flex items-center justify-between gap-2">
            {r.head}
            {r.side}
          </div>
          {r.stats && r.stats.length > 0 && (
            <div className="num mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
              {r.stats}
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

function runNum(run: Run, key: keyof CrawlProgress): number {
  const s = run.stats as Partial<CrawlProgress>
  const v = s?.[key]
  return typeof v === 'number' ? v : 0
}

function StatusBadge({ status }: { status: string }) {
  return (
    <Badge
      variant={status === 'done' ? 'secondary' : status === 'failed' ? 'destructive' : 'outline'}
      className="font-normal"
    >
      {STATUS_LABEL[status] ?? status}
    </Badge>
  )
}

// 任务表筛选分组（来源/模式/状态）：chips 点选即筛，再点一次取消。
const RUN_STATUSES = ['done', 'running', 'failed', 'canceled', 'interrupted', 'skipped'] as const
const RUN_MODES = ['full', 'incremental', 'refresh', 'harvest', 'harvest_incremental'] as const
const RUN_PAGE_SIZES = [10, 20, 50] as const

function FilterChips({
  label,
  options,
  value,
  onChange,
  labelOf,
}: {
  label: string
  options: readonly string[]
  value: string
  onChange: (v: string) => void
  labelOf: (v: string) => string
}) {
  return (
    <span className="flex flex-wrap items-center gap-1">
      <span className="text-[11px] text-muted-foreground">{label}</span>
      <Button
        type="button"
        size="xs"
        variant={value === '' ? 'default' : 'outline'}
        className={cn('h-6 px-2 text-[11px]')}
        onClick={() => onChange('')}
      >
        全部
      </Button>
      {options.map(o => (
        <Button
          key={o}
          type="button"
          size="xs"
          variant={value === o ? 'default' : 'outline'}
          className="h-6 px-2 text-[11px]"
          onClick={() => onChange(value === o ? '' : o)}
        >
          {labelOf(o)}
        </Button>
      ))}
    </span>
  )
}

// 可排序表头：点按列排序，再点反向。
function SortTh({
  label,
  k,
  sort,
  dir,
  onSort,
  right,
}: {
  label: string
  k: string
  sort: string
  dir: 'asc' | 'desc'
  onSort: (k: string) => void
  right?: boolean
}) {
  const active = sort === k
  return (
    <Th right={right}>
      <button
        type="button"
        className={cn('inline-flex items-center gap-0.5', active && 'text-foreground')}
        onClick={() => onSort(k)}
        title={`按${label}排序`}
      >
        {label}
        {active && <span className="text-[10px]">{dir === 'asc' ? '↑' : '↓'}</span>}
      </button>
    </Th>
  )
}

// RunsTable 任务历史（概览与抓取设置共用）：自拉数据，支持来源/模式/状态
// 筛选、列头排序与分页（后端 /api/admin/runs 同参数），不再固定只看十几条。
export function RunsTable() {
  const [source, setSource] = React.useState('')
  const [mode, setMode] = React.useState('')
  const [status, setStatus] = React.useState('')
  const [sort, setSort] = React.useState('started_at')
  const [dir, setDir] = React.useState<'asc' | 'desc'>('desc')
  const [pageSize, setPageSize] = React.useState<number>(20)
  const [page, setPage] = React.useState(1)
  const [data, setData] = React.useState<{ items: Run[]; total: number } | null>(null)
  const [error, setError] = React.useState('')

  const reload = React.useCallback(async () => {
    try {
      setData(await adminRuns({
        source, mode, status, sort, order: dir, limit: pageSize, offset: (page - 1) * pageSize,
      }))
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
    }
  }, [source, mode, status, sort, dir, pageSize, page])

  React.useEffect(() => {
    if (!document.hidden) void reload()
  }, [reload])

  // 筛选/页大小变化回到第一页
  React.useEffect(() => setPage(1), [source, mode, status, pageSize])

  const onSort = (k: string) => {
    if (sort === k) setDir(d => (d === 'asc' ? 'desc' : 'asc'))
    else { setSort(k); setDir(k === 'started_at' ? 'desc' : 'asc') }
  }
  const runs = data?.items ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <Panel
      title="任务历史"
      action={
        <span className="flex items-center gap-2">
          {error && <span className="text-xs text-destructive">{error}</span>}
          <span className="num text-xs text-muted-foreground">共 {total} 条</span>
          <Button type="button" variant="outline" size="xs" onClick={() => void reload()}>刷新</Button>
        </span>
      }
      contentClassName="space-y-2"
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
        <FilterChips
          label="来源"
          options={manifestSources().map(s => s.id)}
          value={source}
          onChange={setSource}
          labelOf={sourceLabel}
        />
        <FilterChips
          label="模式"
          options={RUN_MODES}
          value={mode}
          onChange={setMode}
          labelOf={m => modeLabel(m)}
        />
        <FilterChips
          label="状态"
          options={RUN_STATUSES}
          value={status}
          onChange={setStatus}
          labelOf={s => STATUS_LABEL[s] ?? s}
        />
        <span className="ml-auto flex items-center gap-1 text-[11px] text-muted-foreground">
          每页
          <select
            value={pageSize}
            onChange={e => setPageSize(Number(e.target.value))}
            className="h-6 rounded border bg-background px-1 text-[11px]"
            aria-label="每页条数"
          >
            {RUN_PAGE_SIZES.map(n => (
              <option key={n} value={n}>{n}</option>
            ))}
          </select>
          条
        </span>
      </div>

      {runs.length === 0 && !error ? (
        <p className="py-8 text-center text-sm text-muted-foreground">当前筛选下没有抓取记录</p>
      ) : (
        <>
          <Table containerClassName="scrollbar-thin hidden sm:block" className="min-w-[720px]">
            <TableHeader>
              <TableRow>
                <Th right className="w-14 px-4">#</Th>
                <SortTh label="模式" k="mode" sort={sort} dir={dir} onSort={onSort} />
                <SortTh label="来源" k="source" sort={sort} dir={dir} onSort={onSort} />
                <Th>触发</Th>
                <SortTh label="状态" k="status" sort={sort} dir={dir} onSort={onSort} />
                <SortTh label="开始时间" k="started_at" sort={sort} dir={dir} onSort={onSort} right />
                <Th right>新增 / 更新</Th>
                <Th right className="px-4">错误</Th>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map(r => (
                <TableRow key={r.id}>
                  <Td className="num px-4 text-right text-xs text-muted-foreground">{r.id}</Td>
                  <Td>{modeLabel(r.mode, r.source)}</Td>
                  <Td className="text-muted-foreground">{sourceLabel(r.source)}</Td>
                  <Td className="text-muted-foreground">{r.trigger === 'auto' ? '自动' : '手动'}</Td>
                  <Td>
                    <StatusBadge status={r.status} />
                  </Td>
                  <Td className="num text-right text-xs text-muted-foreground">{fmtDateTime(r.started_at)}</Td>
                  <Td className="num text-right text-xs">
                    {runNum(r, 'inserted')} / {runNum(r, 'updated')}
                  </Td>
                  <Td className={runNum(r, 'errors') ? 'num px-4 text-right text-xs text-destructive' : 'num px-4 text-right text-xs text-muted-foreground'}>
                    {runNum(r, 'errors')}
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          <MobileRows
            rows={runs.map(r => ({
              key: r.id,
              head: (
                <span className="flex min-w-0 items-center gap-2 text-sm">
                  <span className="num text-xs text-muted-foreground">#{r.id}</span>
                  <span className="truncate">{modeLabel(r.mode, r.source)}</span>
                  <span className="text-xs text-muted-foreground">{sourceLabel(r.source)}</span>
                  <span className="text-xs text-muted-foreground">
                    {r.trigger === 'auto' ? '自动' : '手动'}
                  </span>
                </span>
              ),
              side: <StatusBadge status={r.status} />,
              stats: [
                <span key="at">{fmtDateTime(r.started_at)}</span>,
                <span key="n">
                  新增 {runNum(r, 'inserted')} / 更新 {runNum(r, 'updated')}
                </span>,
                runNum(r, 'errors') > 0 ? (
                  <span key="err" className="text-destructive">
                    错误 {runNum(r, 'errors')}
                  </span>
                ) : null,
              ],
            }))}
          />
        </>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-between gap-2 border-t pt-2">
          <span className="num text-xs text-muted-foreground">第 {page} / {totalPages} 页</span>
          <span className="flex gap-1">
            <Button type="button" size="xs" variant="outline" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>
              上一页
            </Button>
            <Button type="button" size="xs" variant="outline" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>
              下一页
            </Button>
          </span>
        </div>
      )}
    </Panel>
  )
}

export function NumField({
  label,
  value,
  onChange,
  hint,
  placeholder,
}: {
  label: string
  value: number
  onChange: (v: number) => void
  hint?: string
  /** value 为 0/空时的占位提示（如「0 = 使用默认」场景显示默认值） */
  placeholder?: string
}) {
  return (
    <div className="space-y-1">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <Input
        type="number"
        value={value === 0 ? '' : value}
        onChange={e => onChange(Number(e.target.value) || 0)}
        placeholder={placeholder}
        className="h-9"
      />
      {hint && <p className="text-[11px] text-muted-foreground">{hint}</p>}
    </div>
  )
}

export function Toggle({
  label,
  checked,
  onChange,
  hint,
}: {
  label: string
  checked: boolean
  onChange: (v: boolean) => void
  hint?: string
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <Switch checked={checked} onCheckedChange={onChange} />
      <span>
        {label}
        {hint && <span className="mt-0.5 block text-[11px] text-muted-foreground">{hint}</span>}
      </span>
    </label>
  )
}
