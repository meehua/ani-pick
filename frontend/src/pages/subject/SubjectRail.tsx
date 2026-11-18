import { Button } from '@/components/ui/button'
import { Panel } from '@/components/panel'
import { Notice } from '@/components/ui-kit'
import { cn } from '@/lib/utils'
import type { BgmSubject } from '../../api/types'
import { infoValue } from './shared'

// 右侧资料栏：信息表。评分分布只在 hero 磨砂扇形图、标签只在 hero 柱状图，均不在此重复。
export function SubjectRail({
  infobox,
  allInfo,
  onToggleInfo,
  locked,
}: {
  infobox: BgmSubject['infobox']
  allInfo: boolean
  onToggleInfo: () => void
  locked: boolean
}) {
  return (
    <aside className="space-y-4">
      {infobox && infobox.length > 0 && (
        <Panel dense title="信息" contentClassName="text-xs">
          <dl className="space-y-1">
            {(allInfo ? infobox : infobox.slice(0, 10)).map((row, i) => (
              <div key={`${row.key}-${i}`} className="flex gap-2">
                <span className="w-20 shrink-0 text-muted-foreground">{row.key}</span>
                <span
                  className={cn(
                    'min-w-0 flex-1 break-words',
                    !allInfo && 'line-clamp-3',
                  )}
                >
                  {infoValue(row.value)}
                </span>
              </div>
            ))}
          </dl>
          {infobox.length > 10 && (
            <Button
              type="button"
              variant="link"
              size="sm"
              className="mt-1 h-auto px-0 text-xs"
              onClick={onToggleInfo}
            >
              {allInfo ? '收起' : `展开全部（共 ${infobox.length} 项）`}
            </Button>
          )}
        </Panel>
      )}

      {locked && (
        <Notice>
          该条目在最近一次抓取中不可访问，本站索引信息可能不完整；稍后会自动重试。
        </Notice>
      )}
    </aside>
  )
}
