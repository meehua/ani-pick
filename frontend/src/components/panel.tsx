import * as React from 'react'
import { cn } from '@/lib/utils'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

// 紧凑面板：管理控制台与详情页侧栏的统一卡片，组合原生 Card 而成。
// 收敛各面板手写的 gap-0 py-0 / border-b px-4 py-3 / text-sm 组合。
export function Panel({
  title,
  desc,
  action,
  dense = false,
  flush = false,
  className,
  contentClassName,
  children,
}: {
  title?: React.ReactNode
  desc?: React.ReactNode
  action?: React.ReactNode
  /** 侧栏密度：标题 px-3 py-2.5、内容 p-3 */
  dense?: boolean
  /** 内容贴边：表格 / 列表自带外框时置空内边距 */
  flush?: boolean
  className?: string
  contentClassName?: string
  children?: React.ReactNode
}) {
  return (
    <Card className={cn('gap-0 overflow-hidden py-0', className)}>
      {(title || action) && (
        <CardHeader
          className={cn(
            // 分隔线用任意属性写而不挂 border-b 类——原生 CardHeader 自带的
            // [.border-b]:pb-6 是自匹配选择器（特异性更高），会压过 py-3 导致头部上下留白不对称。
            'flex flex-row flex-wrap items-center justify-between gap-2 py-3 [border-bottom:1px_solid_var(--border)]',
            dense ? 'px-3 py-2.5' : 'px-4',
          )}
        >
          <div className="min-w-0">
            {title && <CardTitle className="text-sm">{title}</CardTitle>}
            {desc && <p className="mt-0.5 text-[11px] text-muted-foreground">{desc}</p>}
          </div>
          {action && <div className="shrink-0">{action}</div>}
        </CardHeader>
      )}
      <CardContent className={cn(!flush && (dense ? 'p-3' : 'p-4'), contentClassName)}>
        {children}
      </CardContent>
    </Card>
  )
}
