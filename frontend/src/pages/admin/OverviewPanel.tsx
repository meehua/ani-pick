import { Panel } from '@/components/panel'
import type { AdminOverview } from '../../api/types'
import { TYPE_LABEL, TYPE_ORDER } from '../../api/types'
import { fmtBytes, fmtDate, fmtDateTime, fmtNum } from '../../lib/format'
import { CrawlStatusCard, RunsTable, Stat } from './parts'

export function OverviewPanel({
  overview,
  onCancelCrawl,
}: {
  overview: AdminOverview | null
  onCancelCrawl: () => void
}) {
  const stats = overview?.stats
  const crawler = overview?.crawler

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-5">
        <Stat
          label="索引条目"
          value={fmtNum(stats?.total ?? 0)}
          sub={`敏感标记 ${fmtNum(stats?.nsfw ?? 0)}`}
        />
        <Stat
          label="标签"
          value={fmtNum(stats?.tags ?? 0)}
          sub={`标签关联 ${fmtNum(stats?.subject_tags ?? 0)}`}
        />
        <Stat label="数据库" value={fmtBytes(stats?.db_size ?? 0)} sub="PostgreSQL" />
        <Stat
          label="待重爬"
          value={fmtNum(overview?.stale ?? 0)}
          sub={`TTL ${overview?.settings.crawl.refresh_ttl_days ?? '—'} 天`}
        />
        <Stat
          label="最近收录"
          value={stats?.last_fetched ? fmtDate(stats.last_fetched) : '—'}
          sub={stats?.last_fetched ? fmtDateTime(stats.last_fetched).slice(11) : '最近一次写入'}
        />
      </div>

      {stats && (
        <Panel contentClassName="flex flex-wrap items-center gap-x-6 gap-y-2">
          <span className="text-xs text-muted-foreground">类型构成</span>
          {TYPE_ORDER.map(t => (
            <span key={t} className="flex items-baseline gap-1.5 text-xs">
              {TYPE_LABEL[t]}
              <b className="num font-medium">{fmtNum(stats.by_type[String(t)] ?? 0)}</b>
            </span>
          ))}
        </Panel>
      )}

      <CrawlStatusCard status={crawler ?? null} onCancel={onCancelCrawl} />
      <RunsTable runs={overview?.runs ?? []} />
    </div>
  )
}
