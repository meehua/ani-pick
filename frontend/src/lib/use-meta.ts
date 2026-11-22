import { useEffect, useState } from 'react'
import { getMeta } from '../api/client'
import { applyManifest, applyQuickLinks } from '../api/vocab'
import type { MetaResponse } from '../api/types'

// 站点元数据：侧栏、首页、结果计数共用，30 秒内不重复请求。
// meta 里的来源 manifest 每次取到都重放进词表（api/vocab.ts），
// 来源/类型词表、详情分派策略都跟它走。
let cache: MetaResponse | null = null
let cachedAt = 0
let inflight: Promise<MetaResponse> | null = null

function adopt(m: MetaResponse): MetaResponse {
  cache = m
  cachedAt = Date.now()
  applyManifest(m.sources, m.kinds)
  applyQuickLinks(m.quick_links)
  return m
}

export function useMeta(): MetaResponse | null {
  const [meta, setMeta] = useState<MetaResponse | null>(cache)

  useEffect(() => {
    if (cache && Date.now() - cachedAt < 30_000) {
      setMeta(cache)
      return
    }
    if (!inflight) {
      inflight = getMeta()
        .then(adopt)
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

/** 丢弃本地 meta 缓存（管理端保存设置后调用）：下一次挂载重新拉取，
 *  来源 manifest 同步重放。已在挂载中的组件需要各自重新触发拉取。 */
export function resetMetaCache(): void {
  cache = null
  cachedAt = 0
}
