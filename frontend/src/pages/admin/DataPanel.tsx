import * as React from 'react'
import { useSearchParams } from 'react-router-dom'
import { Search, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableHeader, TableRow } from '@/components/ui/table'
import { Th, Td } from '@/components/table-bits'
import { Panel } from '@/components/panel'
import { adminDeleteSubject, adminRefreshSubject, adminSearchSubjects } from '../../api/client'
import { TYPE_LABEL, TYPE_ORDER, type SearchResult } from '../../api/types'
import { fmtDateTime, fmtNum } from '../../lib/format'

// 索引数据管理：关键词 / 类型 / 页码写进 URL（q / tp / pg）。
export function DataPanel({ onChange }: { onChange: () => void }) {
  const [sp, setSp] = useSearchParams()
  const q = sp.get('q') ?? ''
  const types = (sp.get('tp') ?? '').split(',').map(Number).filter(n => !Number.isNaN(n) && n > 0)
  const page = Math.max(1, Number(sp.get('pg')) || 1)
  const [draft, setDraft] = React.useState(q)
  const [data, setData] = React.useState<SearchResult | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [msg, setMsg] = React.useState('')

  const patch = React.useCallback(
    (next: Record<string, string | number | number[] | undefined>) => {
      const p = new URLSearchParams(sp)
      for (const [k, v] of Object.entries(next)) {
        const empty =
          v === undefined ||
          v === '' ||
          (Array.isArray(v) && v.length === 0) ||
          (typeof v === 'number' && v === 0)
        if (empty) p.delete(k)
        else p.set(k, Array.isArray(v) ? v.join(',') : String(v))
      }
      setSp(p, { replace: true })
    },
    [sp, setSp],
  )

  React.useEffect(() => {
    setDraft(q)
  }, [q])

  React.useEffect(() => {
    let alive = true
    setBusy(true)
    adminSearchSubjects({ keyword: q, types, page, page_size: 20 })
      .then(r => {
        if (alive) setData(r)
      })
      .catch(() => undefined)
      .finally(() => {
        if (alive) setBusy(false)
      })
    return () => {
      alive = false
    }
  }, [q, types.join(','), page]) // eslint-disable-line react-hooks/exhaustive-deps

  async function refresh(id: number) {
    setMsg('')
    try {
      await adminRefreshSubject(id)
      setMsg(`条目 ${id} 已刷新`)
      onChange()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : '刷新失败')
    }
  }

  async function remove(id: number) {
    if (!window.confirm(`从本站索引中删除条目 ${id}？`)) return
    setMsg('')
    try {
      await adminDeleteSubject(id)
      setMsg(`条目 ${id} 已删除`)
      onChange()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : '删除失败')
    }
  }

  const totalPages = data ? Math.max(1, Math.ceil(data.total / data.page_size)) : 1

  return (
    <Panel contentClassName="space-y-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <form
            className="flex items-center gap-2"
            onSubmit={e => {
              e.preventDefault()
              patch({ q: draft.trim() || undefined, pg: undefined })
            }}
          >
            <div className="relative">
              <Search
                size={14}
                aria-hidden
                className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground"
              />
              <Input
                value={draft}
                onChange={e => setDraft(e.target.value)}
                placeholder="按名称搜索索引"
                aria-label="搜索索引"
                className="h-9 w-64 pl-8"
              />
            </div>
            <Button type="submit" size="sm" variant="outline">
              检索
            </Button>
          </form>

          <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
            {TYPE_ORDER.map(t => (
              <label key={t} className="flex items-center gap-1.5 text-xs">
                <Checkbox
                  checked={types.includes(t)}
                  onCheckedChange={() =>
                    patch({
                      tp: types.includes(t) ? types.filter(x => x !== t) : [...types, t],
                      pg: undefined,
                    })
                  }
                />
                {TYPE_LABEL[t]}
              </label>
            ))}
            {busy ? <span className="text-xs text-muted-foreground">加载中…</span> : null}
          </div>
        </div>

        {msg && <p className="text-xs text-muted-foreground">{msg}</p>}

        <Table containerClassName="scrollbar-thin rounded-lg border" className="min-w-[780px]">
          <TableHeader>
            <TableRow>
              <Th right className="w-16">ID</Th>
              <Th>名称</Th>
              <Th className="w-16">类型</Th>
              <Th right className="w-16">排名</Th>
              <Th right className="w-14">评分</Th>
              <Th right className="w-14">标签</Th>
              <Th right className="w-40">抓取时间</Th>
              <Th right className="w-24">操作</Th>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(data?.items ?? []).map(s => (
              <TableRow key={s.id}>
                <Td className="num text-right text-xs text-muted-foreground">{s.id}</Td>
                <Td className="max-w-[320px]">
                  <span className="block truncate" title={s.name_cn || s.name}>
                    {s.name_cn || s.name}
                  </span>
                </Td>
                <Td className="text-muted-foreground">{TYPE_LABEL[s.type] ?? s.type}</Td>
                <Td className="num text-right">{s.rank || '—'}</Td>
                <Td className="num text-right">{s.score || '—'}</Td>
                <Td className="num text-right text-muted-foreground">{s.tags_total}</Td>
                <Td className="num text-right text-xs text-muted-foreground">{fmtDateTime(s.fetched_at)}</Td>
                <Td>
                  <div className="flex items-center justify-end gap-1">
                    <Button
                      type="button"
                      variant="ghost"
                      size="xs"
                      onClick={() => void refresh(s.id)}
                    >
                      刷新
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-xs"
                      aria-label={`删除条目 ${s.id}`}
                      onClick={() => void remove(s.id)}
                    >
                      <Trash2 size={13} aria-hidden />
                    </Button>
                  </div>
                </Td>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        {data && (
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="num text-xs text-muted-foreground">
              共 {fmtNum(data.total)} 条 · 第 {page} / {totalPages} 页
            </span>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={page <= 1}
                onClick={() => patch({ pg: page - 1 })}
              >
                上一页
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={page >= totalPages}
                onClick={() => patch({ pg: page + 1 })}
              >
                下一页
              </Button>
            </div>
          </div>
        )}
    </Panel>
  )
}
