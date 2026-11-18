import * as React from 'react'
import { cn } from '@/lib/utils'
import { TableCell, TableHead } from '@/components/ui/table'

// 项目表格密度层：表头 strip（muted/50 + text-xs）与单元格内边距在这里统一，
// 各表格只写差异项（宽度、对齐、颜色、num 等宽）。管理端与浏览页表格共用。
export function Th({
  right = false,
  className,
  ...props
}: React.ComponentProps<typeof TableHead> & { right?: boolean }) {
  return (
    <TableHead
      className={cn('h-auto bg-muted/50 px-3 py-2 text-xs', right && 'text-right', className)}
      {...props}
    />
  )
}

export function Td({ className, ...props }: React.ComponentProps<typeof TableCell>) {
  return <TableCell className={cn('px-3 py-1.5', className)} {...props} />
}
