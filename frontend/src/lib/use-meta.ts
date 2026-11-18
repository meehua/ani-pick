import { useEffect, useState } from 'react'
import { getMeta } from '../api/client'
import type { MetaResponse } from '../api/types'

// 站点元数据：侧栏、首页、结果计数共用，30 秒内不重复请求。
let cache: MetaResponse | null = null
let cachedAt = 0
let inflight: Promise<MetaResponse> | null = null

export function useMeta(): MetaResponse | null {
  const [meta, setMeta] = useState<MetaResponse | null>(cache)

  useEffect(() => {
    if (cache && Date.now() - cachedAt < 30_000) {
      setMeta(cache)
      return
    }
    if (!inflight) {
      inflight = getMeta()
        .then(m => {
          cache = m
          cachedAt = Date.now()
          return m
        })
        .finally(() => {
          inflight = null
        })
    }
    let alive = true
    inflight.then(m => alive && setMeta(m)).catch(() => undefined)
    return () => {
      alive = false
    }
  }, [])

  return meta
}
