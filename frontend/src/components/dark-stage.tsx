import * as React from 'react'
import { cn } from '@/lib/utils'

// 舞台容器：圆角底座 + 可选的模糊封面背景 + 渐变遮罩。
// 首页 hero、条目 hero 与条目骨架共用同一族视觉语言；材质（浅色浅底深字 /
// 深色黑底白字）由 index.css 的 .dark-stage 变量族决定，子元素引用 stage-* 变量。
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
    <section className={cn('dark-stage relative overflow-hidden rounded-2xl', className)}>
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
