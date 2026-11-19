import { KeyRound, RefreshCw, Server } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Panel } from '@/components/panel'
import { Cover } from '@/components/cover'
import { TagProportion } from '@/components/data-bits'
import { BackLink, ErrorNote, Notice } from '@/components/ui-kit'
import { ApiError } from '../../api/client'
import { TYPE_LABEL } from '../../api/types'
import type { Subject as IndexedSubject } from '../../api/types'
import { titleOf } from './shared'

// 上游详情不可用，但本站索引仍有数据：降级展示。
export function SubjectFallback({
  index,
  loading,
  error,
  onLogin,
  onRetry,
}: {
  index: IndexedSubject | null
  loading: boolean
  error: unknown
  onLogin: () => void
  onRetry: () => void
}) {
  const apiErr = error instanceof ApiError ? error : null

  return (
    <div className="space-y-4">
      <BackLink to="/browse">返回索引</BackLink>
      {index ? (
        <>
          <div className="grid max-sm:grid-cols-[110px_minmax(0,1fr)] gap-4 sm:grid-cols-[150px_minmax(0,1fr)] sm:gap-5">
            <Cover
              src={index.image}
              alt={titleOf(index)}
              className="aspect-[5/7] w-full rounded-lg border"
            />
            <div className="min-w-0">
              <Badge variant="secondary">{TYPE_LABEL[index.type] ?? '其他'}</Badge>
              <h1 className="mt-2 text-xl font-semibold sm:text-2xl">{titleOf(index)}</h1>
              {index.name_cn && index.name_cn !== index.name && (
                <p className="mt-1 text-sm text-muted-foreground">{index.name}</p>
              )}
              <p className="num mt-2 text-xs text-muted-foreground">
                {index.date || '无日期'} · {index.score > 0 ? `${index.score.toFixed(1)} 分` : '暂无评分'}
                {index.rank > 0 ? ` · 排名 #${index.rank}` : ''}
              </p>

              <Notice className="mt-4">
                {loading ? (
                  <span className="inline-flex items-center gap-2">
                    <RefreshCw size={13} className="animate-spin" aria-hidden /> 正在获取条目详情…
                  </span>
                ) : (
                  <>
                    <b className="text-foreground">
                      {apiErr?.status === 404 ? '详情需要登录后查看' : (apiErr?.message ?? '详情加载失败')}
                    </b>
                    <span className="mt-1 block">
                      {apiErr?.hint ?? '条目详情由你的浏览器直接向 Bangumi 获取。'}
                    </span>
                  </>
                )}
                <span className="mt-3 flex flex-wrap gap-2">
                  <Button size="sm" onClick={onLogin} className="gap-1.5">
                    <KeyRound size={14} aria-hidden /> 使用我的账号登录
                  </Button>
                  <Button size="sm" variant="outline" onClick={onLogin} className="gap-1.5">
                    <Server size={14} aria-hidden /> 切换数据通道
                  </Button>
                  {!loading && (
                    <Button size="sm" variant="outline" className="gap-1.5" onClick={onRetry}>
                      <RefreshCw size={14} aria-hidden /> 重试
                    </Button>
                  )}
                </span>
              </Notice>
            </div>
          </div>

          {index.tags && index.tags.length > 0 && (
            <Panel
              title="标签与占比"
              action={<span className="text-xs text-muted-foreground">本站索引</span>}
            >
              <TagProportion tags={index.tags} />
              <p className="mt-3 text-xs leading-relaxed text-muted-foreground">
                占比 = 该标签票数 ÷ 全部标签票数，由后台抓取时计算。
              </p>
            </Panel>
          )}
        </>
      ) : (
        <div className="space-y-3">
          <ErrorNote error={error} onRetry={onRetry} />
          <Button size="sm" variant="outline" onClick={onLogin} className="gap-1.5">
            <KeyRound size={14} aria-hidden /> 登录 Bangumi
          </Button>
        </div>
      )}
    </div>
  )
}
