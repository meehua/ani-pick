import { useEffect, useRef } from 'react'
import { saveScroll } from './page-cache'

// 页面滚动记忆：进入时精确还原，离开时保存离开前的 scrollY。
// 与页面数据缓存解耦，Browse/Subject 共用同一套时序。
export function useScrollMemory(cacheKey: string, restoreTop: number | null | undefined) {
  const last = useRef(restoreTop ?? 0)

  useEffect(() => {
    if (restoreTop == null) return
    const top = restoreTop
    requestAnimationFrame(() => window.scrollTo(0, top))
    const timer = window.setTimeout(() => {
      if (Math.abs(window.scrollY - top) > 2) window.scrollTo(0, top)
    }, 80)
    return () => window.clearTimeout(timer)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    let timer: number | undefined
    const onScroll = () => {
      last.current = window.scrollY
      saveScroll(cacheKey, last.current)
      window.clearTimeout(timer)
      timer = window.setTimeout(() => saveScroll(cacheKey, last.current), 250)
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
      window.clearTimeout(timer)
      saveScroll(cacheKey, last.current)
    }
  }, [cacheKey])
}
