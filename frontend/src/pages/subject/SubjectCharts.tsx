import * as React from 'react'
import { Link } from 'react-router-dom'
import { fmtNum } from '../../lib/format'
import type { TagVote } from '../../api/types'

// hero 右侧两张图：评分分布扇形（donut）+ 标签柱状（横向滚动）。
// 玻璃质感由图形自己承担：扇形自带圆形镜片（backdrop-blur + 极淡描边），
// 柱条本身半透明毛玻璃——不再套任何面板底。
// 颜色走 .dark-stage 变量族（浅色浅底深字 / 深色黑底白字），amber 为唯一强调色。
// 滚轮约定：鼠标进图即为图服务——柱状图滚轮横滚（不释放给页面），donut 滚轮在有票档间循环步进。

// 低分段（1–5）几乎隐没的中性色，高分段（6–10）琥珀点亮：图形直接传达「高分占比」。
// 基色由 --stage-seg-rgb 随主题切换（浅色 zinc-900 / 深色白），透明度不变。
function segAlpha(star: number): number {
  return star >= 6 ? 0.3 + 0.7 * ((star - 6) / 4) : 0.1 + 0.1 * ((star - 1) / 4)
}

function segColor(star: number, selected: number | null, topStar: number | null): string {
  let alpha = segAlpha(star)
  if (selected === star) alpha = Math.min(1, alpha + 0.3)
  else if (selected !== null) alpha *= 0.4
  else if (star === topStar) alpha = Math.min(1, alpha + 0.15)
  const rgb = star >= 6 ? '245 158 11' : 'var(--stage-seg-rgb)'
  return `rgb(${rgb} / ${alpha.toFixed(2)})`
}

// 评分分布 donut：counts[0]=1 分 … counts[9]=10 分；10 分段起点 12 点方向顺时针。
// 交互：初始完整呈现分布、仅默认档（票数最高）轻微提亮；hover/点按选定某段并
// **持久保持**（不自动回退），其余段压暗聚焦；滚轮在有票档间循环步进（向上 = 更高分，
// 到端点绕回另一头），同样持久。
export function RatingDonut({ counts }: { counts: number[] }) {
  const sum = counts.reduce((a, b) => a + b, 0)
  const [selected, setSelected] = React.useState<number | null>(null)
  const panelRef = React.useRef<HTMLDivElement>(null)
  const SIZE = 132
  const C = SIZE / 2
  const R = 52
  const STROKE = 13
  const GAP = 2.5 // 相邻段间隔（弧长 px）
  const CIRC = 2 * Math.PI * R

  const segs = [...counts]
    .map((n, i) => ({ star: i + 1, n }))
    .reverse()
    .filter(s => s.n > 0)
  const topSeg = segs.reduce<((typeof segs)[number]) | null>(
    (a, b) => (!a || b.n > a.n ? b : a),
    null,
  )
  const shownSeg = selected ? segs.find(s => s.star === selected) : topSeg

  // 滚轮在有票档间循环步进：向上 = 更高分，到两头绕回。事件只为图服务，页面不滚。
  React.useEffect(() => {
    const el = panelRef.current
    if (!el || segs.length === 0) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const dy = e.deltaMode === 1 ? e.deltaY * 40 : e.deltaY
      if (!dy) return
      const cur = selected ?? segs[0].star
      const idx = segs.findIndex(s => s.star === cur)
      const dir = dy < 0 ? -1 : 1
      const next = segs[(idx + dir + segs.length) % segs.length]
      setSelected(next.star)
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [selected, segs])

  let acc = 0
  const arcs = segs.map(s => {
    const len = (s.n / sum) * CIRC
    const solid = Math.max(len - GAP, 0.75)
    const dash = `${solid} ${CIRC - solid}`
    const offset = -acc
    acc += len
    const isSelected = selected === s.star
    return (
      <circle
        key={s.star}
        cx={C}
        cy={C}
        r={R}
        fill="none"
        stroke={segColor(s.star, selected, topSeg?.star ?? null)}
        strokeWidth={isSelected ? STROKE + 2 : STROKE}
        strokeDasharray={dash}
        strokeDashoffset={offset}
        transform={`rotate(-90 ${C} ${C})`}
        className="cursor-pointer transition-all duration-200"
        onMouseEnter={() => setSelected(s.star)}
        onClick={() => setSelected(s.star)}
      >
        <title>{`${s.star} 分 · ${s.n} 票`}</title>
      </circle>
    )
  })

  return (
    <div ref={panelRef} className="relative mx-auto" style={{ width: SIZE, height: SIZE }}>
      {/* 玻璃镜片：图形自身的毛玻璃，跟随圆盘轮廓，无面板底 */}
      <div
        className="absolute inset-0 rounded-full bg-(--stage-lens) ring-1 ring-(--stage-line) backdrop-blur-md"
        aria-hidden
      />
      <svg
        className="relative"
        width={SIZE}
        height={SIZE}
        role="img"
        aria-label="评分分布扇形图，hover 或滚轮查看各档"
      >
        <circle cx={C} cy={C} r={R} fill="none" stroke="rgb(var(--stage-seg-rgb) / 0.08)" strokeWidth={STROKE} />
        {arcs}
      </svg>
      <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
        {shownSeg ? (
          <>
            <p className="num text-2xl font-semibold leading-none text-amber-600 dark:text-amber-400">
              {shownSeg.star}
              <span className="ml-0.5 text-sm font-normal text-(--stage-fg)/50">分</span>
            </p>
            <p className="num mt-1 text-xs text-(--stage-fg)/60">{fmtNum(shownSeg.n)} 票</p>
          </>
        ) : (
          <p className="text-xs text-(--stage-fg)/50">暂无评分</p>
        )}
      </div>
    </div>
  )
}

// 标签柱状图：按票数降序，最高分默认最左；容器横向滚动。
// 两端虚化只在「该侧还有内容」时出现——滚到头即完整显示最后一根，不再被遮罩糊掉。
// 滚轮策略：鼠标在图内滚轮只为图服务——始终横滚图（含触摸板横滑），滚到端点后继续滚动
// 不释放给页面；离开图区域页面才恢复滚动。
export function TagBars({ tags }: { tags: TagVote[] }) {
  const ref = React.useRef<HTMLDivElement>(null)
  const [edge, setEdge] = React.useState({ left: false, right: false })

  const syncEdge = React.useCallback(() => {
    const el = ref.current
    if (!el) return
    const max = el.scrollWidth - el.clientWidth
    setEdge({ left: el.scrollLeft > 1, right: el.scrollLeft < max - 1 })
  }, [])

  React.useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    syncEdge()
    el.addEventListener('scroll', syncEdge, { passive: true })
    const ro = new ResizeObserver(syncEdge)
    ro.observe(el)
    return () => {
      el.removeEventListener('scroll', syncEdge)
      ro.disconnect()
    }
  }, [syncEdge, tags])

  React.useEffect(() => {
    const el = ref.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      // 水平分量主导（触摸板横滑）：交给容器原生横滚。
      if (Math.abs(e.deltaX) > Math.abs(e.deltaY)) return
      // 垂直滚轮一律捕获为横滚（端点后静默，不放行页面）。
      e.preventDefault()
      const dy = e.deltaMode === 1 ? e.deltaY * 40 : e.deltaY
      el.scrollLeft += dy
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [])

  const sorted = [...tags].sort((a, b) => b.votes - a.votes).slice(0, 40)
  const max = sorted[0]?.votes ?? 1

  // 动态边缘遮罩：左有内容才虚化左侧，右有内容才虚化右侧。
  // 虚化带宽两侧一致，且等于内容留白（px-5 = 20px）——只糊留白区，两端的柱子
  // 都是完整滑入，左右到边的行为对称。
  const stops: string[] = []
  if (edge.left) stops.push('transparent', 'black 20px')
  else stops.push('black')
  if (edge.right) stops.push('black calc(100% - 20px)', 'transparent')
  else stops.push('black')
  const mask = `linear-gradient(to right, ${stops.join(', ')})`

  if (sorted.length === 0) {
    return <p className="py-6 text-center text-xs text-(--stage-fg)/50">暂无标签数据</p>
  }

  return (
    <div
      ref={ref}
      className="scrollbar-thin overflow-x-auto pb-0.5"
      style={{ maskImage: mask, WebkitMaskImage: mask }}
    >
      <div className="flex items-end gap-1 px-5">
        {sorted.map(t => (
          <Link
            key={t.name}
            to={`/browse?tags=${encodeURIComponent(t.name)}`}
            className="group flex w-12 shrink-0 flex-col items-center justify-end gap-1"
            title={`${t.name} · ${t.votes} 票 · ${t.pct.toFixed(1)}%`}
          >
            <span className="num text-xs leading-none text-(--stage-fg)/60 group-hover:text-(--stage-fg)">
              {fmtNum(t.votes)}
            </span>
            <div
              className="w-7 rounded-t-sm bg-amber-500/45 backdrop-blur-[3px] transition-colors group-hover:bg-amber-500/80 dark:bg-amber-400/55 dark:group-hover:bg-amber-400/90"
              style={{ height: `${Math.max(6, Math.round((t.votes / max) * 62))}px` }}
            />
            <span className="w-full truncate text-center text-xs leading-tight text-(--stage-fg)/55 group-hover:text-(--stage-fg)">
              {t.name}
            </span>
          </Link>
        ))}
      </div>
    </div>
  )
}
