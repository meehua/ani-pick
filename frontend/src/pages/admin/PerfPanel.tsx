import * as React from 'react'
import { ArrowDown, ArrowUp, Check, Copy, Loader2, RefreshCw, RotateCcw, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useMenuPointerClose } from '@/lib/use-menu-pointer-close'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableHeader, TableRow } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Th, Td } from '@/components/table-bits'
import { Panel } from '@/components/panel'
import { MobileRows } from './parts'
import type { MetricsSnapshot } from '../../api/types'
import { fmtBytes, fmtDateTime, fmtNum } from '../../lib/format'

function ms(v: number): string {
  return v >= 100 ? Math.round(v).toString() : v.toFixed(1)
}

// 耗时分级着色：≥1s 红、≥300ms 琥珀——慢的路由一眼可见。
function msTone(v: number): string {
  return v >= 1000 ? 'text-destructive' : v >= 300 ? 'text-amber-600 dark:text-amber-400' : ''
}

// 最近请求样本的 p95（最近序近似）。
function p95Of(values: number[]): number | null {
  if (values.length === 0) return null
  const sorted = [...values].sort((a, b) => a - b)
  return sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * 0.95))]
}

type SortKey = 'count' | 'errors' | 'avg_ms' | 'p95_ms' | 'max_ms'

const SORT_LABEL: Record<SortKey, string> = {
  count: '请求',
  errors: '错误',
  avg_ms: '平均 ms',
  p95_ms: 'p95 ms',
  max_ms: '最大 ms',
}

function StatusBadge({ status }: { status: number }) {
  return (
    <Badge
      variant={status >= 500 ? 'destructive' : status >= 400 ? 'outline' : 'secondary'}
      className={cn(
        'num px-1.5 font-normal',
        status >= 400 && status < 500 && 'text-amber-600 dark:text-amber-400',
      )}
    >
      {status}
    </Badge>
  )
}

// admin 路由是后台自身的轮询噪音（session/overview/metrics 每 4s 一次），默认从统计里剔除。
const isAdminRoute = (route: string) => route.includes('/admin')

export function PerfPanel({
  metrics,
  onRefresh,
  onReset,
}: {
  metrics: MetricsSnapshot | null
  onRefresh: () => void
  onReset: () => void
}) {
  const [sort, setSort] = React.useState<{ key: SortKey; dir: 1 | -1 }>({ key: 'count', dir: -1 })
  const [bucket, setBucket] = React.useState<'all' | '2' | '4' | '5'>('all')
  const [showAdmin, setShowAdmin] = React.useState(false)
  const [copied, setCopied] = React.useState<string | null>(null)
  const copyTimer = React.useRef<number | undefined>(undefined)
  const menuFocus = useMenuPointerClose()

  const routes = React.useMemo(() => {
    const list = (metrics?.routes ?? []).filter(r => showAdmin || !isAdminRoute(r.route))
    list.sort((a, b) => (a[sort.key] - b[sort.key]) * sort.dir)
    return list
  }, [metrics, sort, showAdmin])

  const recent = (metrics?.recent ?? []).filter(rq => showAdmin || !isAdminRoute(rq.route))
  const filtered = recent.filter(
    rq => bucket === 'all' || Math.floor(rq.status / 100) === Number(bucket),
  )
  const bucketCount = (b: '2' | '4' | '5') =>
    recent.filter(rq => Math.floor(rq.status / 100) === Number(b)).length

  if (!metrics) {
    return (
      <p className="py-14 text-center text-sm text-muted-foreground">
        <Loader2 size={14} className="mr-1 inline animate-spin" aria-hidden /> 加载性能数据…
      </p>
    )
  }

  const totalReq = routes.reduce((s, r) => s + r.count, 0)
  const totalErr = routes.reduce((s, r) => s + r.errors, 0)
  const errRate = totalReq > 0 ? ((totalErr / totalReq) * 100).toFixed(1) : '0.0'
  const weightedAvg =
    totalReq > 0 ? routes.reduce((s, r) => s + r.avg_ms * r.count, 0) / totalReq : 0
  const sampleP95 = p95Of(filtered.map(rq => rq.ms))
  const maxP95 = routes.reduce((m, r) => Math.max(m, r.p95_ms), 0)

  // 复制：JSON 为全量快照（含被过滤掉的数据，交给脚本/LLM 分析）；
  // CSV 为当前表格视图（所见即所得，可直接贴进表格软件）。
  const csvEscape = (v: string) => (v.includes(',') || v.includes('"') ? `"${v.replace(/"/g, '""')}"` : v)
  const snapshotJson = () =>
    JSON.stringify(
      {
        captured_at: new Date().toISOString(),
        since: metrics.since,
        uptime_s: metrics.uptime_s,
        routes: metrics.routes,
        recent: metrics.recent,
      },
      null,
      2,
    )
  const routesCsv = () =>
    [
      'route,count,errors,avg_ms,p95_ms,max_ms,avg_bytes,avg_db_ms,avg_gzip_ms',
      ...routes.map(r =>
        [
          csvEscape(r.route), r.count, r.errors, r.avg_ms, r.p95_ms, r.max_ms,
          r.avg_bytes, r.avg_db_ms, r.avg_gzip_ms,
        ].join(','),
      ),
    ].join('\n')
  const recentCsv = () =>
    [
      'at,route,status,ms,db_ms,gzip_ms,bytes',
      ...filtered.map(r =>
        [r.at, csvEscape(r.route), r.status, r.ms, r.db_ms, r.gzip_ms, r.bytes].join(','),
      ),
    ].join('\n')
  const copyText = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied('已复制')
    } catch {
      setCopied('复制失败')
    } finally {
      window.clearTimeout(copyTimer.current)
      copyTimer.current = window.setTimeout(() => setCopied(null), 1800)
    }
  }

  const toggleSort = (key: SortKey) =>
    setSort(s => (s.key === key ? { key, dir: (-s.dir) as 1 | -1 } : { key, dir: -1 }))
  const SortTh = ({ k }: { k: SortKey }) => (
    <Th right>
      <button
        type="button"
        onClick={() => toggleSort(k)}
        title={`按${SORT_LABEL[k]}排序`}
        className="inline-flex items-center gap-0.5 transition-colors hover:text-foreground"
      >
        {SORT_LABEL[k]}
        {sort.key === k &&
          (sort.dir === -1 ? <ArrowDown size={11} aria-hidden /> : <ArrowUp size={11} aria-hidden />)}
      </button>
    </Th>
  )

  return (
    <div className="space-y-4">
      <Panel
        title="路由响应统计"
        desc={`自 ${fmtDateTime(metrics.since)} · 已运行 ${fmtNum(metrics.uptime_s)} 秒`}
        action={
          <div className="flex items-center gap-2">
            <DropdownMenu>
              <DropdownMenuTrigger asChild onPointerDown={menuFocus.onPointerDown}>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className={cn('gap-1.5', copied === '复制失败' && 'text-destructive')}
                >
                  {copied === '复制失败' ? (
                    <X size={13} aria-hidden />
                  ) : copied ? (
                    <Check size={13} aria-hidden />
                  ) : (
                    <Copy size={13} aria-hidden />
                  )}
                  {copied ?? '复制'}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" onCloseAutoFocus={menuFocus.onCloseAutoFocus}>
                <DropdownMenuItem className="gap-2" onClick={() => void copyText(snapshotJson())}>
                  完整快照 JSON
                </DropdownMenuItem>
                <DropdownMenuItem className="gap-2" onClick={() => void copyText(routesCsv())}>
                  路由统计 CSV
                </DropdownMenuItem>
                <DropdownMenuItem className="gap-2" onClick={() => void copyText(recentCsv())}>
                  最近请求 CSV
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button type="button" variant="outline" size="sm" onClick={onRefresh} className="gap-1.5">
              <RefreshCw size={13} aria-hidden /> 刷新
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={onReset} className="gap-1.5">
              <RotateCcw size={13} aria-hidden /> 复位
            </Button>
          </div>
        }
        flush
      >
        <div className="flex flex-wrap items-center justify-between gap-2 px-4 pb-2 pt-3">
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <Switch checked={showAdmin} onCheckedChange={setShowAdmin} />
            显示 admin 路由
          </label>
          <span className="text-xs text-muted-foreground max-sm:hidden">点击表头按该列排序</span>
        </div>

        {/* 统计 KPI：随过滤联动 */}
        <div className="grid grid-cols-2 gap-2 px-4 pb-2 sm:grid-cols-4">
          <div className="rounded-lg bg-muted/50 px-3 py-2">
            <p className="text-[11px] text-muted-foreground">总请求</p>
            <p className="num mt-1 text-lg font-semibold leading-none">{fmtNum(totalReq)}</p>
            <p className="mt-1.5 truncate text-[11px] text-muted-foreground">
              {routes.length} 条路由
            </p>
          </div>
          <div className="rounded-lg bg-muted/50 px-3 py-2">
            <p className="text-[11px] text-muted-foreground">错误</p>
            <p
              className={cn(
                'num mt-1 text-lg font-semibold leading-none',
                totalErr > 0 && 'text-destructive',
              )}
            >
              {fmtNum(totalErr)}
            </p>
            <p className="mt-1.5 truncate text-[11px] text-muted-foreground">错误率 {errRate}%</p>
          </div>
          <div className="rounded-lg bg-muted/50 px-3 py-2">
            <p className="text-[11px] text-muted-foreground">平均耗时</p>
            <p className="num mt-1 text-lg font-semibold leading-none">{ms(weightedAvg)} ms</p>
            <p className="mt-1.5 truncate text-[11px] text-muted-foreground">按请求数加权</p>
          </div>
          <div className="rounded-lg bg-muted/50 px-3 py-2">
            <p className="text-[11px] text-muted-foreground">最近 p95</p>
            <p
              className={cn(
                'num mt-1 text-lg font-semibold leading-none',
                sampleP95 !== null && msTone(sampleP95),
              )}
            >
              {sampleP95 !== null ? `${ms(sampleP95)} ms` : '—'}
            </p>
            <p className="mt-1.5 truncate text-[11px] text-muted-foreground">
              最近 {filtered.length} 条样本
            </p>
          </div>
        </div>

        <Table containerClassName="scrollbar-thin hidden sm:block" className="min-w-[780px]">
          <TableHeader>
            <TableRow>
              <Th className="px-4">路由</Th>
              <SortTh k="count" />
              <SortTh k="errors" />
              <SortTh k="avg_ms" />
              <Th right>
                p95 ms
                {maxP95 > 0 && <span className="ml-1 text-[10px] font-normal">/ 分布</span>}
              </Th>
              <SortTh k="max_ms" />
              <Th right>平均大小</Th>
              <Th right>DB ms</Th>
              <Th right className="px-4">
                gzip ms
              </Th>
            </TableRow>
          </TableHeader>
          <TableBody>
            {routes.map(rt => (
              <TableRow key={rt.route}>
                <Td className="px-4 font-mono text-xs" title={rt.route}>
                  {rt.route}
                </Td>
                <Td className="num text-right">{fmtNum(rt.count)}</Td>
                <Td
                  className={cn(
                    'num text-right',
                    rt.errors > 0 ? 'font-medium text-destructive' : 'text-muted-foreground',
                  )}
                >
                  {rt.errors}
                </Td>
                <Td className={cn('num text-right', msTone(rt.avg_ms))}>{ms(rt.avg_ms)}</Td>
                <Td className="text-right">
                  <span className={cn('num font-medium', msTone(rt.p95_ms))}>{ms(rt.p95_ms)}</span>
                  {maxP95 > 0 && (
                    <div className="ml-auto mt-1 h-1 w-16 rounded-full bg-muted">
                      <div
                        className={cn(
                          'h-full rounded-full',
                          rt.p95_ms >= 1000
                            ? 'bg-destructive'
                            : rt.p95_ms >= 300
                              ? 'bg-amber-500'
                              : 'bg-primary/70',
                        )}
                        style={{ width: `${Math.max(3, (rt.p95_ms / maxP95) * 100)}%` }}
                      />
                    </div>
                  )}
                </Td>
                <Td className={cn('num text-right', msTone(rt.max_ms))}>{ms(rt.max_ms)}</Td>
                <Td className="num text-right">{fmtBytes(rt.avg_bytes)}</Td>
                <Td className="num text-right text-muted-foreground">{ms(rt.avg_db_ms)}</Td>
                <Td className="num px-4 text-right text-muted-foreground">{ms(rt.avg_gzip_ms)}</Td>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        <MobileRows
          rows={routes.map(rt => ({
            key: rt.route,
            head: <span className="min-w-0 truncate font-mono text-xs">{rt.route}</span>,
            side:
              rt.errors > 0 ? (
                <span className="num shrink-0 text-[11px] text-destructive">错误 {rt.errors}</span>
              ) : undefined,
            stats: [
              <span key="c">{fmtNum(rt.count)} 次</span>,
              <span key="a">均 {ms(rt.avg_ms)}ms</span>,
              <span key="p">p95 {ms(rt.p95_ms)}ms</span>,
              <span key="m">最大 {ms(rt.max_ms)}ms</span>,
              <span key="b">{fmtBytes(rt.avg_bytes)}</span>,
            ],
          }))}
        />
        <p className="border-t px-4 py-2 text-[11px] text-muted-foreground">
          总耗时 ≈ DB + gzip + 处理与编码；大小为走线字节（已含压缩效果）。
        </p>
      </Panel>

      <Panel
        title="最近请求"
        action={<span className="num text-xs text-muted-foreground">{recent.length} 条</span>}
        flush
      >
        <div className="flex flex-wrap items-center justify-between gap-2 px-4 pb-1 pt-3">
          <ToggleGroup
            type="single"
            size="sm"
            value={bucket}
            onValueChange={v => v && setBucket(v as 'all' | '2' | '4' | '5')}
            className="gap-1"
          >
            <ToggleGroupItem value="all" className="h-7 px-2.5 text-xs">
              全部 {recent.length}
            </ToggleGroupItem>
            <ToggleGroupItem value="2" className="h-7 px-2.5 text-xs">
              2xx {bucketCount('2')}
            </ToggleGroupItem>
            <ToggleGroupItem value="4" className="h-7 px-2.5 text-xs">
              4xx {bucketCount('4')}
            </ToggleGroupItem>
            <ToggleGroupItem value="5" className="h-7 px-2.5 text-xs">
              5xx {bucketCount('5')}
            </ToggleGroupItem>
          </ToggleGroup>
          <span className="num text-xs text-muted-foreground">
            {filtered.length === recent.length
              ? `共 ${filtered.length} 条`
              : `显示 ${filtered.length} / ${recent.length} 条`}
          </span>
        </div>
        {filtered.length === 0 ? (
          <p className="py-10 text-center text-sm text-muted-foreground">该区间暂无请求</p>
        ) : (
          <>
            {/* 全量展示但限高内滚：不把整页拉长 */}
            <div className="scrollbar-thin max-h-[440px] overflow-y-auto">
              <Table containerClassName="hidden sm:block" className="min-w-[720px]">
                <TableHeader className="sticky top-0 z-10 bg-card">
                  <TableRow>
                    <Th className="px-4">时间</Th>
                    <Th>路由</Th>
                    <Th right>状态</Th>
                    <Th right>总 ms</Th>
                    <Th right>DB ms</Th>
                    <Th right>gzip ms</Th>
                    <Th right className="px-4">
                      大小
                    </Th>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filtered.map((rq, i) => (
                    <TableRow key={`${rq.at}-${i}`}>
                      <Td className="num px-4 text-xs text-muted-foreground">{fmtDateTime(rq.at)}</Td>
                      <Td className="font-mono text-xs" title={rq.route}>
                        {rq.route}
                      </Td>
                      <Td className="text-right">
                        <StatusBadge status={rq.status} />
                      </Td>
                      <Td className={cn('num text-right font-medium', msTone(rq.ms))}>{ms(rq.ms)}</Td>
                      <Td className="num text-right text-muted-foreground">{ms(rq.db_ms)}</Td>
                      <Td className="num text-right text-muted-foreground">{ms(rq.gzip_ms)}</Td>
                      <Td className="num px-4 text-right">{fmtBytes(rq.bytes)}</Td>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            <div className="scrollbar-none max-h-[420px] overflow-y-auto sm:hidden">
              <MobileRows
                rows={filtered.map((rq, i) => ({
                  key: `${rq.at}-${i}`,
                  head: <span className="min-w-0 truncate font-mono text-xs">{rq.route}</span>,
                  side: <StatusBadge status={rq.status} />,
                  stats: [
                    <span key="at">{fmtDateTime(rq.at)}</span>,
                    <span key="ms">{ms(rq.ms)}ms</span>,
                    <span key="db">DB {ms(rq.db_ms)}ms</span>,
                    <span key="b">{fmtBytes(rq.bytes)}</span>,
                  ],
                }))}
              />
            </div>
          </>
        )}
      </Panel>
    </div>
  )
}
