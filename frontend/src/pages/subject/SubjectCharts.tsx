import * as React from 'react'
import { Link } from 'react-router-dom'
import { fmtNum } from '../../lib/format'
import type { TagVote } from '../../api/types'

// hero 磨砂面板内的两张图：评分分布扇形（donut）+ 标签柱状（横向滚动、两端虚化）。
// 均为深底白字体系，amber 为唯一强调色。
// 滚轮约定：鼠标进图即为图服务——柱状图滚轮横滚（不释放给页面），donut 滚轮在评分档间步进。

// 评分分布 donut：counts[0]=1 分 … counts[9]=10 分；10 分段起点 12 点方向顺时针。
// 高分段更亮（amber 不透明度随分数衰减）。
// 交互：默认突出票数最高的一档；hover/点按选定某段并**持久保持**（不自动回退）；
// 滚轮在有票档间步进（向上 = 更高分），同样持久。
export function RatingDonut({ counts }: { counts: number[] }) {
  const sum = counts.reduce((a, b) => a + b, 0)
  const [selected, setSelected] = React.useState<number | null>(null)
  const panelRef = React.useRef<HTMLDivElement>(null)
  const SIZE = 132
  const C = SIZE / 2
  const R = 52
  const CIRC = 2 * Math.PI * R

  const segs = [...counts]
    .map((n, i) => ({ star: i + 1, n }))
    .reverse()
    .filter(s => s.n > 0)
  const topSeg = segs.reduce<((typeof segs)[number]) | null>(
    (a, b) => (!a || b.n > a.n ? b : a),
    null,
  )
  const shown = selected ?? topSeg?.star ?? null
  const shownSeg = segs.find(s => s.star === shown)

  // 滚轮在有票档间步进：向上 = 更高分。事件只为图服务，页面不滚。
  React.useEffect(() => {
    const el = panelRef.current
    if (!el || segs.length === 0) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const dy = e.deltaMode === 1 ? e.deltaY * 40 : e.deltaY
      if (!dy) return
      const cur = shown ?? segs[0].star
      const idx = segs.findIndex(s => s.star === cur)
      const next = dy < 0 ? segs[Math.max(0, idx - 1)] : segs[Math.min(segs.length - 1, idx + 1)]
      if (next && next.star !== cur) setSelected(next.star)
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [shown, segs])

  let acc = 0
  const arcs = segs.map(s => {
    const len = (s.n / sum) * CIRC
    const solid = Math.max(len - 2, 0.5)
    const dash = `${solid} ${CIRC - solid}`
    const offset = -acc
    acc += len
    const baseAlpha = 0.22 + 0.78 * ((s.star - 1) / 9)
    const isShown = s.star === shown
    const alpha = isShown ? Math.min(1, baseAlpha + 0.35) : baseAlpha * 0.4
    return (
      <circle
        key={s.star}
        cx={C}
        cy={C}
        r={R}
        fill="none"
        stroke={`rgb(245 158 11 / ${alpha.toFixed(2)})`}
        strokeWidth={isShown ? 17 : 13}
        strokeDasharray={dash}
        strokeDashoffset={offset}
        transform={`rotate(-90 ${C} ${C})`}
        className="cursor-pointer transition-all duration-150"
        onMouseEnter={() => setSelected(s.star)}
        onClick={() => setSelected(s.star)}
      >
        <title>{`${s.star} 分 · ${s.n} 票`}</title>
      </circle>
    )
  })

  return (
    <div ref={panelRef} className="relative mx-auto" style={{ width: SIZE, height: SIZE }}>
      <svg width={SIZE} height={SIZE} role="img" aria-label="评分分布扇形图，hover 或滚轮查看各档">
        <circle cx={C} cy={C} r={R} fill="none" stroke="rgb(255 255 255 / 0.08)" strokeWidth={13} />
        {arcs}
      </svg>
      <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
        {shownSeg ? (
          <>
            <p className="num text-2xl font-semibold leading-none text-amber-400">
              {shownSeg.star} <span className="text-sm font-normal text-white/50">分</span>
            </p>
            <p className="num mt-1 text-xs text-white/60">{fmtNum(shownSeg.n)} 票</p>
          </>
        ) : (
          <p className="text-xs text-white/50">暂无评分</p>
        )}
      </div>
    </div>
  )
}

// 标签柱状图：按票数降序，最高分默认最左；容器横向滚动，两端 mask 虚化提示还有数据。
// 滚轮策略：鼠标在图内滚轮只为图服务——始终横滚图（含触摸板横滑），滚到端点后继续滚动
// 不释放给页面；离开图区域页面才恢复滚动。
export function TagBars({ tags }: { tags: TagVote[] }) {
  const ref = React.useRef<HTMLDivElement>(null)

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
  const mask =
    'linear-gradient(to right, transparent, black 18px, black calc(100% - 26px), transparent)'

  if (sorted.length === 0) {
    return <p className="py-6 text-center text-xs text-white/50">暂无标签数据</p>
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
            <span className="num text-xs leading-none text-white/60 group-hover:text-white">
              {fmtNum(t.votes)}
            </span>
            <div
              className="w-7 rounded-t-sm bg-amber-400/75 transition-colors group-hover:bg-amber-400"
              style={{ height: `${Math.max(6, Math.round((t.votes / max) * 62))}px` }}
            />
            <span className="w-full truncate text-center text-xs leading-tight text-white/55 group-hover:text-white">
              {t.name}
            </span>
          </Link>
        ))}
      </div>
    </div>
  )
}
