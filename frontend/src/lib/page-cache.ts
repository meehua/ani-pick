// 页面级缓存：按 react-router 的 location.key 记住离开时的结果与滚动位置。
// 返回（POP 回到同一个 key）时瞬间还原数据和滚动，避免重新挂载闪骨架屏。
// 内存有界：最多 12 个条目，FIFO 淘汰（1GB VPS 的红线同样适用于访客浏览器低端机）。

interface Entry {
  data: unknown
  scrollTop: number
}

// 上限是管理设置（page_cache_max，管理员改后经 setPageCacheMax 生效；0 = 关闭缓存）。
let MAX = 12
const store = new Map<string, Entry>()

/** 调整返回缓存页数上限（管理设置加载/保存后调用；0 = 关闭并清空）。 */
export function setPageCacheMax(max: number): void {
  MAX = Math.max(0, Math.min(max, 50))
  if (MAX === 0) {
    store.clear()
    return
  }
  while (store.size > MAX) {
    const oldest = store.keys().next().value
    if (oldest === undefined) break
    store.delete(oldest)
  }
}

export function savePage(key: string, data: unknown): void {
  if (!key || data == null) return
  const prev = store.get(key)
  if (prev) {
    prev.data = data
    store.delete(key)
    store.set(key, prev)
    return
  }
  store.set(key, { data, scrollTop: 0 })
  if (store.size > MAX) {
    const oldest = store.keys().next().value
    if (oldest !== undefined) store.delete(oldest)
  }
}

export function saveScroll(key: string, top: number): void {
  const e = store.get(key)
  if (e) e.scrollTop = top
}

export function loadPage<T>(key: string): { data: T; scrollTop: number } | null {
  const e = store.get(key)
  if (!e) return null
  // 读到即续期：返回访问的页面短期不会被淘汰
  store.delete(key)
  store.set(key, e)
  return { data: e.data as T, scrollTop: e.scrollTop }
}
