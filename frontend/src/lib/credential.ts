// 用户 Bangumi 凭据只保存在本浏览器 localStorage：
// - 不写入 AniPick 后端，也不经过任何第三方服务器
// - 仅用于浏览器直连 https://api.bgm.tv，不经过本站服务端
import { useSyncExternalStore } from 'react'

export interface Credential {
  token: string
  savedAt: number
  user?: { id: number; username?: string; nickname?: string }
}

const KEY = 'anipick.credential.v1'
const EVENT = 'anipick:credential'

let cache: Credential | null | undefined
const listeners = new Set<() => void>()

function read(): Credential | null {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Credential
    if (!parsed?.token) return null
    return parsed
  } catch {
    return null
  }
}

function snapshot(): Credential | null {
  if (cache === undefined) cache = read()
  return cache
}

function notify() {
  cache = read()
  listeners.forEach(fn => fn())
}

function subscribe(fn: () => void) {
  listeners.add(fn)
  const onStorage = (e: StorageEvent) => {
    if (e.key === KEY) notify()
  }
  window.addEventListener('storage', onStorage)
  window.addEventListener(EVENT, notify)
  return () => {
    listeners.delete(fn)
    window.removeEventListener('storage', onStorage)
    window.removeEventListener(EVENT, notify)
  }
}

export function getCredential(): Credential | null {
  return snapshot()
}

export function saveCredential(token: string, user?: Credential['user']) {
  localStorage.setItem(KEY, JSON.stringify({ token, savedAt: Date.now(), user } satisfies Credential))
  notify()
  window.dispatchEvent(new Event(EVENT))
}

export function clearCredential() {
  localStorage.removeItem(KEY)
  notify()
  window.dispatchEvent(new Event(EVENT))
}

export function useCredential(): Credential | null {
  return useSyncExternalStore(subscribe, snapshot, () => null)
}
