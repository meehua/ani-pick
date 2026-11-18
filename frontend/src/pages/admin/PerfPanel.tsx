import { Loader2, RefreshCw, RotateCcw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableHeader, TableRow } from '@/components/ui/table'
import { Th, Td } from '@/components/table-bits'
import { Panel } from '@/components/panel'
import { MobileRows } from './parts'
import type { MetricsSnapshot } from '../../api/types'
import { fmtBytes, fmtDateTime, fmtNum } from '../../lib/format'

function ms(v: number): string {
  return v >= 100 ? Math.round(v).toString() : v.toFixed(1)
}

export function PerfPanel({
  metrics,
  onRefresh,
  onReset,
}: {
  metrics: MetricsSnapshot | null
  onRefresh: () => void
  onReset: () => void
}) {
  if (!metrics) {
    return (
      <p className="py-14 text-center text-sm text-muted-foreground">
        <Loader2 size={14} className="mr-1 inline animate-spin" aria-hidden /> 加载性能数据…
      </p>
    )
  }

  return (
    <div className="space-y-4">
      <Panel
        title="路由响应统计"
        desc={`自 ${fmtDateTime(metrics.since)} · 已运行 ${fmtNum(metrics.uptime_s)} 秒`}
        action={
          <div className="flex items-center gap-2">
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
        {metrics.routes.length === 0 ? (
          <p className="py-10 text-center text-sm text-muted-foreground">暂无请求样本</p>
        ) : (
          <>
            <Table containerClassName="scrollbar-thin hidden sm:block" className="min-w-[780px]">
              <TableHeader>
                <TableRow>
                  <Th className="px-4">路由</Th>
                  <Th right>请求</Th>
                  <Th right>错误</Th>
                  <Th right>平均 ms</Th>
                  <Th right>p95 ms</Th>
                  <Th right>最大 ms</Th>
                  <Th right>平均大小</Th>
                  <Th right>DB ms</Th>
                  <Th right className="px-4">gzip ms</Th>
                </TableRow>
              </TableHeader>
              <TableBody>
                {metrics.routes.map(rt => (
                  <TableRow key={rt.route}>
                    <Td className="px-4 font-mono text-xs">{rt.route}</Td>
                    <Td className="num text-right">{fmtNum(rt.count)}</Td>
                    <Td
                      className={
                        rt.errors
                          ? 'num text-right text-destructive'
                          : 'num text-right text-muted-foreground'
                      }
                    >
                      {rt.errors}
                    </Td>
                    <Td className="num text-right">{ms(rt.avg_ms)}</Td>
                    <Td className="num text-right">{ms(rt.p95_ms)}</Td>
                    <Td className="num text-right">{ms(rt.max_ms)}</Td>
                    <Td className="num text-right">{fmtBytes(rt.avg_bytes)}</Td>
                    <Td className="num text-right text-muted-foreground">{ms(rt.avg_db_ms)}</Td>
                    <Td className="num px-4 text-right text-muted-foreground">{ms(rt.avg_gzip_ms)}</Td>
                  </TableRow>
                ))}
              </TableBody>
            </Table>

            <MobileRows
              rows={metrics.routes.map(rt => ({
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
          </>
        )}
        <p className="border-t px-4 py-2 text-[11px] text-muted-foreground">
          总耗时 ≈ DB + gzip + 处理与编码；大小为走线字节（已含压缩效果）。
        </p>
      </Panel>

      <Panel
        title="最近请求"
        action={<span className="num text-xs text-muted-foreground">{metrics.recent.length} 条</span>}
        flush
      >
        {metrics.recent.length === 0 ? (
          <p className="py-10 text-center text-sm text-muted-foreground">暂无记录</p>
        ) : (
          <>
            <Table containerClassName="scrollbar-thin hidden sm:block" className="min-w-[720px]">
              <TableHeader>
                <TableRow>
                  <Th className="px-4">时间</Th>
                  <Th>路由</Th>
                  <Th right>状态</Th>
                  <Th right>总 ms</Th>
                  <Th right>DB ms</Th>
                  <Th right>gzip ms</Th>
                  <Th right className="px-4">大小</Th>
                </TableRow>
              </TableHeader>
              <TableBody>
                {metrics.recent.slice(0, 80).map((rq, i) => (
                  <TableRow key={`${rq.at}-${i}`}>
                    <Td className="num px-4 text-xs text-muted-foreground">{fmtDateTime(rq.at)}</Td>
                    <Td className="font-mono text-xs">{rq.route}</Td>
                    <Td
                      className={
                        rq.status >= 500
                          ? 'num text-right text-destructive'
                          : rq.status >= 400
                            ? 'num text-right text-amber-600 dark:text-amber-400'
                            : 'num text-right text-muted-foreground'
                      }
                    >
                      {rq.status}
                    </Td>
                    <Td className="num text-right">{ms(rq.ms)}</Td>
                    <Td className="num text-right text-muted-foreground">{ms(rq.db_ms)}</Td>
                    <Td className="num text-right text-muted-foreground">{ms(rq.gzip_ms)}</Td>
                    <Td className="num px-4 text-right">{fmtBytes(rq.bytes)}</Td>
                  </TableRow>
                ))}
              </TableBody>
            </Table>

            <MobileRows
              rows={metrics.recent.slice(0, 40).map((rq, i) => ({
                key: `${rq.at}-${i}`,
                head: <span className="min-w-0 truncate font-mono text-xs">{rq.route}</span>,
                side: (
                  <span
                    className={
                      rq.status >= 500
                        ? 'num shrink-0 text-destructive'
                        : rq.status >= 400
                          ? 'num shrink-0 text-amber-600 dark:text-amber-400'
                          : 'num shrink-0'
                    }
                  >
                    {rq.status}
                  </span>
                ),
                stats: [
                  <span key="at">{fmtDateTime(rq.at)}</span>,
                  <span key="ms">{ms(rq.ms)}ms</span>,
                  <span key="db">DB {ms(rq.db_ms)}ms</span>,
                  <span key="b">{fmtBytes(rq.bytes)}</span>,
                ],
              }))}
            />
          </>
        )}
      </Panel>
    </div>
  )
}
