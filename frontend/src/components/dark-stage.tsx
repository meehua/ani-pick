import * as React from 'react'
import { cn } from '@/lib/utils'

// 深色舞台：zinc-950 圆角容器 + 可选的模糊封面背景 + 渐变遮罩。
// 首页 hero、条目 hero 与条目骨架共用同一族视觉语言。
export function DarkStage({
  backdropSrc,
  className,
  imgClassName,
  overlayClassName,
  children,
}: {
  backdropSrc?: string
  className?: string
  imgClassName?: string
  overlayClassName?: string
  children: React.ReactNode
}) {
  return (
    <section className={cn('relative overflow-hidden rounded-2xl bg-zinc-950 text-zinc-50', className)}>
      {backdropSrc && (
        <img
          src={backdropSrc}
          alt=""
          aria-hidden
          className={cn('absolute inset-0 h-full w-full scale-110 object-cover blur-2xl', imgClassName)}
        />
      )}
      {overlayClassName && <div className={cn('absolute inset-0', overlayClassName)} />}
      {children}
    </section>
  )
}
