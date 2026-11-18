import { Panel } from '@/components/panel'
import type { AdminOverview } from '../../api/types'
import { TYPE_LABEL } from '../../api/types'
import { MODE_LABEL } from '../../lib/format'
import { adminCancelCrawl, adminStartCrawl } from '../../api/client'
import { CrawlAction, CrawlStatusCard, RunsTable } from './parts'

export function CrawlPanel({
  overview,
  onRefresh,
  onNotice,
  onError,
}: {
  overview: AdminOverview | null
  onRefresh: () => Promise<void>
  onNotice: (msg: string) => void
  onError: (msg: string) => void
}) {
  const crawler = overview?.crawler ?? null
  const types = overview?.settings.crawl.types ?? []

  async function startCrawl(mode: 'full' | 'incremental' | 'refresh') {
    if (
      mode === 'full' &&
      !window.confirm(
        '全量抓取会逐类型遍历所有条目，可能持续数十分钟到数小时；结束后会清理本轮未覆盖的旧条目。确定开始？',
      )
    ) {
      return
    }
    try {
      const res = await adminStartCrawl(mode)
      onNotice(`已启动${MODE_LABEL[mode]}（任务 #${res.run_id}）`)
      await onRefresh()
    } catch (e) {
      onError(e instanceof Error ? e.message : '启动失败')
    }
  }

  async function cancelCrawl() {
    try {
      await adminCancelCrawl()
      onNotice('已请求停止任务')
      await onRefresh()
    } catch (e) {
      onError(e instanceof Error ? e.message : '取消失败')
    }
  }

  return (
    <div className="space-y-4">
      <Panel contentClassName="space-y-4">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h2 className="text-sm font-medium">启动抓取</h2>
              <p className="mt-1 max-w-2xl text-xs leading-relaxed text-muted-foreground">
                抓取使用设置中的站长 Bangumi 凭据；未配置凭据时也能抓取公开条目，但覆盖面与敏感内容会受限。
              </p>
            </div>
            <p className="text-xs text-muted-foreground">
              当前类型：
              <span className="text-foreground">
                {types.map(t => TYPE_LABEL[t] ?? t).join(' / ') || '未设置'}
              </span>
            </p>
          </div>

          <div className="grid gap-3 sm:grid-cols-3">
            <CrawlAction
              index="A · FULL"
              title="全量抓取"
              desc="逐类型遍历全部条目，结束后清理本轮未覆盖的旧数据。适合首次建库。"
              disabled={crawler?.running}
              onClick={() => void startCrawl('full')}
            />
            <CrawlAction
              index="B · INCREMENTAL"
              title="增量抓取"
              desc="抓取各类型最新若干页并顺带重爬一批过期条目。日常维护用。"
              disabled={crawler?.running}
              onClick={() => void startCrawl('incremental')}
            />
            <CrawlAction
              index="C · REFRESH"
              title="过期重爬"
              desc="只处理超过 TTL 的条目，按排名优先，数量受刷新批量限制。"
              disabled={crawler?.running}
              onClick={() => void startCrawl('refresh')}
            />
          </div>
      </Panel>

      <CrawlStatusCard status={crawler} onCancel={() => void cancelCrawl()} />
      <RunsTable runs={overview?.runs ?? []} />
    </div>
  )
}
