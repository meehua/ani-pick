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
import { TYPE_LABEL } from '../../api/types'
import { fmtDateTime, MODE_LABEL, STATUS_LABEL } from '../../lib/format'

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

export function CrawlAction({
  index,
  title,
  desc,
  disabled,
  onClick,
}: {
  index: string
  title: string
  desc: string
  disabled?: boolean
  onClick: () => void
}) {
  return (
    <Panel contentClassName="flex h-full flex-col">
      <span className="num text-[11px] text-muted-foreground">{index}</span>
      <p className="mt-1 text-sm font-medium">{title}</p>
      <p className="mt-1 flex-1 text-xs leading-relaxed text-muted-foreground">{desc}</p>
      <Button
        type="button"
        size="sm"
        variant="outline"
        className="mt-3 w-full"
        disabled={disabled}
        onClick={onClick}
      >
        启动
      </Button>
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
            {status.running ? (MODE_LABEL[status.mode] ?? status.mode) : '空闲'}
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
        {p.type > 0 && (
          <div className="min-w-0">
            <dt className="text-[11px] text-muted-foreground">类型</dt>
            <dd className="mt-0.5 truncate text-sm">{TYPE_LABEL[p.type] ?? p.type}</dd>
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

export function RunsTable({ runs }: { runs: Run[] }) {
  return (
    <Panel
      title="最近任务"
      action={<span className="num text-xs text-muted-foreground">{runs.length} 条</span>}
      flush
    >
      {runs.length === 0 ? (
        <p className="py-10 text-center text-sm text-muted-foreground">还没有抓取记录</p>
      ) : (
        <>
          <Table containerClassName="scrollbar-thin hidden sm:block" className="min-w-[680px]">
            <TableHeader>
              <TableRow>
                <Th right className="w-14 px-4">#</Th>
                <Th>模式</Th>
                <Th>触发</Th>
                <Th>状态</Th>
                <Th right>开始时间</Th>
                <Th right>新增 / 更新</Th>
                <Th right className="px-4">错误</Th>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map(r => (
                <TableRow key={r.id}>
                  <Td className="num px-4 text-right text-xs text-muted-foreground">{r.id}</Td>
                  <Td>{MODE_LABEL[r.mode] ?? r.mode}</Td>
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
                  <span className="truncate">{MODE_LABEL[r.mode] ?? r.mode}</span>
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
    </Panel>
  )
}

export function NumField({
  label,
  value,
  onChange,
  hint,
}: {
  label: string
  value: number
  onChange: (v: number) => void
  hint?: string
}) {
  return (
    <div className="space-y-1">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <Input
        type="number"
        value={value}
        onChange={e => onChange(Number(e.target.value) || 0)}
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
