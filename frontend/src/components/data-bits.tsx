import { Link } from 'react-router-dom'
import type { TagVote } from '../api/types'

// 标签占比：名称 / 比例条 / 百分比，三列对齐。仅详情页降级视图使用。
export function TagProportion({ tags, limit = 16 }: { tags: TagVote[]; limit?: number }) {
  const list = tags.slice(0, limit)
  if (list.length === 0) {
    return <p className="text-sm text-muted-foreground">暂无标签</p>
  }
  return (
    <ul className="space-y-2.5">
      {list.map(tag => (
        <li key={tag.name} title={`${tag.name} · ${tag.votes} 票 · ${tag.pct.toFixed(1)}%`}>
          <div className="flex items-baseline justify-between gap-3">
            <Link
              to={`/browse?tags=${encodeURIComponent(tag.name)}`}
              className="min-w-0 truncate text-[13px] hover:underline"
            >
              {tag.name}
            </Link>
            <span className="num shrink-0 text-xs text-muted-foreground">{tag.pct.toFixed(1)}%</span>
          </div>
          <div className="mt-1 bar" style={{ height: 4 }}>
            <span style={{ width: `${Math.max(1.5, Math.min(100, tag.pct))}%` }} />
          </div>
        </li>
      ))}
    </ul>
  )
}
