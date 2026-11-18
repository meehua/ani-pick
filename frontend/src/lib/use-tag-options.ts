import { useEffect, useState } from 'react'
import { getTags } from '../api/client'
import type { TagStat } from '../api/types'

// 标签候选的唯一数据入口：防抖 + 中断，按类型收敛。
// 组件只消费 items/loading，不再各自写一份请求逻辑。
export function useTagOptions(
  query: string,
  types: number[],
  limit = 30,
  enabled = true,
): { items: TagStat[]; loading: boolean } {
  const [items, setItems] = useState<TagStat[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!enabled) return
    const ctrl = new AbortController()
    const timer = window.setTimeout(() => {
      setLoading(true)
      getTags(query.trim(), types, limit, ctrl.signal)
        .then(r => setItems(r.items))
        .catch(() => undefined)
        .finally(() => setLoading(false))
    }, 150)
    return () => {
      window.clearTimeout(timer)
      ctrl.abort()
    }
  }, [query, types.join(','), limit, enabled]) // eslint-disable-line react-hooks/exhaustive-deps

  return { items, loading }
}
