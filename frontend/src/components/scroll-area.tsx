import * as React from 'react'
import { cn } from '@/lib/utils'

const HIDE_MS = 1000 // 静止多久后隐藏
const THUMB_H = 40 // 拇指固定长度：一条短指示条，不随内容比例拉长
const INSET = 4 // 距容器边缘的留白

// 细滚动指示条：隐藏原生滚动条，在左缘渲染一条固定 40px 的短拇指，
// 滚动后静止 HIDE_MS 即淡出。拇指是纯指示层（pointer-events-none，不可拖），
// 滚轮 / 键盘 / 触摸等滚动交互全部由原生容器承担。
// className 交给外框（位置 / 外观 / max-h 高度上限）；内部 flex + min-h-0
// 保证高度上限落在滚动容器上。
export function ScrollArea({
  className,
  children,
}: {
  className?: string
  children: React.ReactNode
}) {
  const ref = React.useRef<HTMLDivElement>(null)
  const timer = React.useRef<number | undefined>(undefined)
  const [thumb, setThumb] = React.useState<number | null>(null)
  const [active, setActive] = React.useState(false)

  const sync = React.useCallback(() => {
    const el = ref.current
    if (!el) return
    const max = el.scrollHeight - el.clientHeight
    if (max <= 0) {
      setThumb(null)
      return
    }
    const travel = Math.max(1, el.clientHeight - THUMB_H - INSET * 2)
    setThumb(INSET + (el.scrollTop / max) * travel)
    setActive(true)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setActive(false), HIDE_MS)
  }, [])

  React.useEffect(() => {
    const el = ref.current
    if (!el) return
    sync()
    const ro = new ResizeObserver(sync)
    ro.observe(el)
    // 内容高度变化（如筛选展开收起）不改变容器尺寸，需一并观察子元素
    for (const child of el.children) ro.observe(child)
    return () => {
      ro.disconnect()
      window.clearTimeout(timer.current)
    }
  }, [sync])

  return (
    <div className={cn('relative flex flex-col overflow-hidden', className)}>
      <div
        ref={ref}
        onScroll={sync}
        className="scrollbar-none min-h-0 flex-1 overflow-y-auto overscroll-contain"
      >
        {children}
      </div>
      <div
        aria-hidden
        className={cn(
          'pointer-events-none absolute left-1 z-20 w-1 rounded-full bg-foreground/20 transition-opacity duration-300',
          active && thumb !== null ? 'opacity-100' : 'opacity-0',
        )}
        style={thumb !== null ? { top: thumb, height: THUMB_H } : undefined}
      />
    </div>
  )
}
