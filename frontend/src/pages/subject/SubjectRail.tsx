import { Button } from '@/components/ui/button'
import { Panel } from '@/components/panel'
import { Notice } from '@/components/ui-kit'
import { cn } from '@/lib/utils'
import type { FusedEntry, FusedFactRow } from './fusion'
import { sourceLabel } from '../../api/vocab'

// 同一来源在同一条目行里有多个值（剧场版/TV 各一条）时才给该值标注变体，
// 单个值的来源标不带变体——避免变体出现在不会混淆的地方。
function showVariant(row: FusedFactRow, e: FusedEntry): boolean {
  return !!e.variant && row.entries.filter(x => x.source === e.source).length > 1
}

function SourceBadge({ label }: { label: string }) {
  return (
    <span className="shrink-0 self-center rounded border border-border/60 bg-muted/60 px-1 py-px text-[10px] leading-3 text-muted-foreground">
      {label}
    </span>
  )
}

// 右侧资料栏：一张按来源顺序融合的信息表（buildFusedFacts 产出）。
// 只有表里出现多个来源时才标注来源徽标，且只标注在「异值并陈」的追加值上
//（每行第一个值 = 顺序最先提供该字段的来源，先到先得，不需要徽标）。
// 评分分布只在 hero 磨砂扇形图、标签只在 hero 柱状图，均不在此重复。
export function SubjectRail({
  facts,
  allInfo,
  onToggleInfo,
  locked,
}: {
  /** 信息表：组内全部来源行按有效优先序融合后的字段（无数据时不渲染） */
  facts?: FusedFactRow[]
  allInfo: boolean
  onToggleInfo: () => void
  locked: boolean
}) {
  const sources = new Set((facts ?? []).flatMap(r => r.entries.map(e => e.source)))
  const multi = sources.size > 1
  return (
    <aside className="space-y-4">
      {facts && facts.length > 0 && (
        <Panel dense title="信息" contentClassName="text-xs">
          {multi && (
            <p className="mb-2 text-[11px] leading-relaxed text-muted-foreground">
              同名字段按来源顺序取第一个值；其余来源的不同值并列并标注来源。
            </p>
          )}
          <dl className="space-y-1">
            {(allInfo ? facts : facts.slice(0, 10)).map(row => (
              <div key={row.key} className="flex gap-2">
                <span className="w-20 shrink-0 text-muted-foreground">{row.key}</span>
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  {row.entries.map((e, i) => (
                    <span key={i} className="flex items-baseline gap-1.5">
                      <span
                        className={cn(
                          'min-w-0 flex-1 break-words',
                          !allInfo && 'line-clamp-3',
                        )}
                      >
                        {e.value}
                      </span>
                      {multi && i > 0 && (
                        <SourceBadge
                          label={
                            showVariant(row, e)
                              ? `${sourceLabel(e.source)} · ${e.variant}`
                              : sourceLabel(e.source)
                          }
                        />
                      )}
                    </span>
                  ))}
                </span>
              </div>
            ))}
          </dl>
          {facts.length > 10 && (
            <Button
              type="button"
              variant="link"
              size="sm"
              className="mt-1 h-auto px-0 text-xs"
              onClick={onToggleInfo}
            >
              {allInfo ? '收起' : `展开全部（共 ${facts.length} 项）`}
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
