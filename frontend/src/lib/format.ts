export function fmtNum(n: number): string {
  if (!n) return '0'
  if (n >= 100000000) return `${(n / 100000000).toFixed(1)}亿`
  if (n >= 10000) return `${(n / 10000).toFixed(1)}万`
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}

// 动态量级：<1 万保留原始千分位；>=1 万用「万」；>=1 亿用「亿」。
// 单位与数字之间留一个空格，单位与量词（部/个）之间不留。
export function fmtNumParts(n: number): { value: string; unit: '' | '万' | '亿' } {
  if (!n) return { value: '0', unit: '' }
  if (n >= 100000000) return { value: (n / 100000000).toFixed(1), unit: '亿' }
  if (n >= 10000) return { value: (n / 10000).toFixed(1), unit: '万' }
  return { value: n.toLocaleString(), unit: '' }
}

export function fmtCount(n: number): string {
  const p = fmtNumParts(n)
  return p.unit ? `${p.value} ${p.unit}` : p.value
}

export function fmtBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function fmtDate(iso?: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function fmtDateTime(iso?: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${fmtDate(iso)} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

export function yearOf(date?: string): string {
  return date && date.length >= 4 ? date.slice(0, 4) : '—'
}

export const MODE_LABEL: Record<string, string> = {
  full: '全量抓取',
  incremental: '增量抓取',
  refresh: '过期重爬',
}

export const STATUS_LABEL: Record<string, string> = {
  running: '进行中',
  done: '完成',
  failed: '失败',
  canceled: '已取消',
  interrupted: '已中断',
}
