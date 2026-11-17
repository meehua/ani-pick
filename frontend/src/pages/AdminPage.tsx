import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Activity, Database, HardDrive, Loader2, LogOut, Play, RefreshCw, Save, Square, Tags, Timer, Trash2,
} from 'lucide-react'
import { ApiError, adminCancelCrawl, adminChangePassword, adminDeleteSubject, adminGetSettings, adminNetwork,
  adminLogout, adminOverview, adminRefreshSubject, adminSaveSettings, adminSearchSubjects,
  adminSession, adminStartCrawl, adminTestCredential } from '../api/client'
import type { AdminOverview, AdminSettings, CrawlProgress, CrawlSettings, NetworkStatus, Run, SearchResult } from '../api/types'
import { TYPE_LABEL, TYPE_ORDER } from '../api/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { fmtBytes, fmtDateTime, fmtNum, MODE_LABEL, STATUS_LABEL } from '../lib/format'

export default function AdminPage() {
  const navigate = useNavigate()
  const [ready, setReady] = React.useState(false)
  const [overview, setOverview] = React.useState<AdminOverview | null>(null)
  const [settings, setSettings] = React.useState<AdminSettings | null>(null)
  const [notice, setNotice] = React.useState('')
  const [error, setError] = React.useState('')

  const loadOverview = React.useCallback(async () => {
    try {
      setOverview(await adminOverview())
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) navigate('/admin/login', { replace: true })
    }
  }, [navigate])

  React.useEffect(() => {
    adminSession()
      .then(async () => {
        setReady(true)
        await loadOverview()
        setSettings(await adminGetSettings())
      })
      .catch(() => navigate('/admin/login', { replace: true }))
  }, [loadOverview, navigate])

  React.useEffect(() => {
    if (!ready) return
    const t = window.setInterval(() => {
      // 页面不可见时跳过轮询，避免后台标签页持续打数据库
      if (!document.hidden) void loadOverview()
    }, 4000)
    return () => window.clearInterval(t)
  }, [ready, loadOverview])

  async function startCrawl(mode: 'full' | 'incremental' | 'refresh') {
    if (mode === 'full' && !window.confirm(
      '全量抓取会逐类型遍历所有条目，可能持续数十分钟到数小时；结束后会清理本轮未覆盖的旧条目。确定开始？',
    )) return
    setError('')
    setNotice('')
    try {
      const res = await adminStartCrawl(mode)
      setNotice(`已启动${MODE_LABEL[mode]}（任务 #${res.run_id}）`)
      await loadOverview()
    } catch (e) {
      setError(e instanceof Error ? e.message : '启动失败')
    }
  }

  async function cancelCrawl() {
    try {
      await adminCancelCrawl()
      setNotice('已请求停止任务')
      await loadOverview()
    } catch (e) {
      setError(e instanceof Error ? e.message : '取消失败')
    }
  }

  async function logout() {
    await adminLogout().catch(() => undefined)
    navigate('/admin/login', { replace: true })
  }

  if (!ready) {
    return (
      <div className="flex items-center justify-center py-24 text-sm text-muted-foreground">
        <Loader2 size={16} className="mr-2 animate-spin" /> 正在验证管理员会话…
      </div>
    )
  }

  const stats = overview?.stats
  const crawler = overview?.crawler

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">管理后台</h1>
          <p className="text-xs text-muted-foreground">索引抓取、访问凭据与站点设置</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void logout()} className="gap-1.5">
          <LogOut size={13} /> 退出登录
        </Button>
      </div>

      {notice && <div className="rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-200">{notice}</div>}
      {error && <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">{error}</div>}

      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="crawl">抓取</TabsTrigger>
          <TabsTrigger value="data">数据</TabsTrigger>
          <TabsTrigger value="settings">设置</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="pt-4">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <StatCard icon={<Database size={14} />} label="索引条目" value={fmtNum(stats?.total ?? 0)} sub={`敏感内容 ${fmtNum(stats?.nsfw ?? 0)}`} />
            <StatCard icon={<Tags size={14} />} label="标签" value={fmtNum(stats?.tags ?? 0)} sub={`关联 ${fmtNum(stats?.subject_tags ?? 0)}`} />
            <StatCard icon={<HardDrive size={14} />} label="数据库" value={fmtBytes(stats?.db_size ?? 0)} sub="PostgreSQL" />
            <StatCard icon={<Timer size={14} />} label="待重爬" value={fmtNum(overview?.stale ?? 0)} sub={`TTL ${overview?.settings.crawl.refresh_ttl_days ?? '-'} 天`} />
            <StatCard icon={<Activity size={14} />} label="最近收录" value={stats?.last_fetched ? fmtDateTime(stats.last_fetched) : '—'} sub="最近一次成功写入" />
            <StatCard
              icon={<RefreshCw size={14} />}
              label="抓取状态"
              value={crawler?.running ? MODE_LABEL[crawler.mode] ?? '运行中' : '空闲'}
              sub={crawler?.running ? crawler.progress.message : '可手动启动'}
            />
          </div>

          {stats && (
            <div className="mt-3 flex flex-wrap gap-1.5">
              {TYPE_ORDER.map(t => (
                <Badge key={t} variant="outline" className="gap-1 font-normal">
                  {TYPE_LABEL[t]} <span className="tabular-nums text-muted-foreground">{fmtNum(stats.by_type[String(t)] ?? 0)}</span>
                </Badge>
              ))}
            </div>
          )}

          <CrawlStatusCard status={overview?.crawler ?? null} onCancel={() => void cancelCrawl()} />

          <RunsTable runs={overview?.runs ?? []} />
        </TabsContent>

        <TabsContent value="crawl" className="space-y-4 pt-4">
          <Card>
            <CardHeader><CardTitle className="text-sm">启动抓取</CardTitle></CardHeader>
            <CardContent className="space-y-3">
              <p className="text-xs leading-relaxed text-muted-foreground">
                抓取使用「设置」里配置的站长 Bangumi 凭据；未配置凭据时也能抓取公开条目，但覆盖面与敏感内容会受限。
              </p>
              <p className="text-xs text-muted-foreground">
                当前抓取类型：
                <span className="text-foreground">
                  {(overview?.settings.crawl.types ?? []).map(t => TYPE_LABEL[t] ?? t).join(' / ') || '未设置'}
                </span>
                （在「设置」中修改）
              </p>
              <div className="grid gap-2 sm:grid-cols-3">
                <CrawlAction
                  title="全量抓取"
                  desc="逐类型遍历全部条目，结束后清理本轮未覆盖的旧数据。适合首次建库。"
                  disabled={crawler?.running}
                  onClick={() => void startCrawl('full')}
                />
                <CrawlAction
                  title="增量抓取"
                  desc="抓取各类型最新若干页并顺带重爬一批过期条目。日常维护用。"
                  disabled={crawler?.running}
                  onClick={() => void startCrawl('incremental')}
                />
                <CrawlAction
                  title="过期重爬"
                  desc="只处理超过 TTL 的条目，按排名优先，数量受刷新批量限制。"
                  disabled={crawler?.running}
                  onClick={() => void startCrawl('refresh')}
                />
              </div>
            </CardContent>
          </Card>

          <CrawlStatusCard status={overview?.crawler ?? null} onCancel={() => void cancelCrawl()} detailed />
          <RunsTable runs={overview?.runs ?? []} />
        </TabsContent>

        <TabsContent value="data" className="pt-4">
          <DataPanel onChange={() => void loadOverview()} />
        </TabsContent>

        <TabsContent value="settings" className="pt-4">
          {settings && (
            <SettingsPanel
              settings={settings}
              onSaved={async msg => {
                setNotice(msg)
                setSettings(await adminGetSettings())
                await loadOverview()
              }}
              onError={setError}
            />
          )}
        </TabsContent>
      </Tabs>
    </div>
  )
}

function StatCard({ icon, label, value, sub }: { icon: React.ReactNode; label: string; value: string; sub?: string }) {
  return (
    <Card>
      <CardContent className="pt-4">
        <p className="flex items-center gap-1.5 text-[11px] text-muted-foreground">{icon}{label}</p>
        <p className="mt-1 truncate text-lg font-semibold tabular-nums" title={value}>{value}</p>
        {sub && <p className="mt-0.5 truncate text-[10px] text-muted-foreground" title={sub}>{sub}</p>}
      </CardContent>
    </Card>
  )
}

function CrawlAction({ title, desc, disabled, onClick }: { title: string; desc: string; disabled?: boolean; onClick: () => void }) {
  return (
    <div className="rounded-lg border p-3">
      <p className="text-sm font-medium">{title}</p>
      <p className="mt-1 min-h-10 text-[11px] leading-relaxed text-muted-foreground">{desc}</p>
      <Button size="sm" variant="outline" className="mt-2 w-full gap-1.5" disabled={disabled} onClick={onClick}>
        <Play size={12} /> 开始
      </Button>
    </div>
  )
}

function CrawlStatusCard({ status, onCancel, detailed }: {
  status: AdminOverview['crawler'] | null
  onCancel: () => void
  detailed?: boolean
}) {
  if (!status) return null
  const p = status.progress
  const pct = p.total > 0 ? Math.min(100, Math.round((p.offset / p.total) * 100)) : undefined
  return (
    <Card className={detailed ? '' : 'mt-3'}>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="text-sm">
          抓取状态 · {status.running ? (MODE_LABEL[status.mode] ?? status.mode) : '空闲'}
          {status.running && status.trigger === 'auto' && <span className="ml-1 text-[10px] text-muted-foreground">（自动）</span>}
        </CardTitle>
        {status.running && (
          <Button size="sm" variant="outline" onClick={onCancel} className="gap-1.5">
            <Square size={12} /> 停止
          </Button>
        )}
      </CardHeader>
      <CardContent className="space-y-2 text-xs">
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-muted-foreground">
          <span>阶段 {p.phase || '—'}</span>
          {p.type > 0 && <span>类型 {TYPE_LABEL[p.type] ?? p.type}</span>}
          {p.total > 0 && <span className="tabular-nums">{p.offset}/{p.total}</span>}
          <span>请求 {p.requests}</span>
          <span>新增 {p.inserted}</span>
          <span>更新 {p.updated}</span>
          <span>重爬 {p.refreshed}</span>
          {p.pruned > 0 && <span>清理 {p.pruned}</span>}
          {p.errors > 0 && <span className="text-destructive">错误 {p.errors}</span>}
          {p.skipped > 0 && <span className="text-amber-600">跳过 {p.skipped}</span>}
          {status.running && status.started_at && <span>开始于 {fmtDateTime(status.started_at)}</span>}
        </div>
        {(status.running || (p.phase && p.phase !== 'done')) && (
          <div className="h-1.5 overflow-hidden rounded bg-muted">
            <div
              className={pct === undefined ? 'h-full w-1/3 animate-pulse rounded bg-primary/60' : 'h-full rounded bg-primary transition-[width]'}
              style={pct === undefined ? undefined : { width: `${pct}%` }}
            />
          </div>
        )}
        {p.message && <p className="text-[11px] text-muted-foreground">{p.message}</p>}
      </CardContent>
    </Card>
  )
}

function runNum(run: Run, key: keyof CrawlProgress): number {
  const s = run.stats as Partial<CrawlProgress>
  const v = s?.[key]
  return typeof v === 'number' ? v : 0
}

function RunsTable({ runs }: { runs: Run[] }) {
  return (
    <Card className="mt-4">
      <CardHeader><CardTitle className="text-sm">最近任务</CardTitle></CardHeader>
      <CardContent className="overflow-x-auto">
        {runs.length === 0 ? (
          <p className="py-4 text-center text-xs text-muted-foreground">还没有抓取记录</p>
        ) : (
          <table className="w-full min-w-[640px] text-xs">
            <thead className="text-left text-muted-foreground">
              <tr className="border-b">
                <th className="py-1.5 pr-2 font-medium">#</th>
                <th className="py-1.5 pr-2 font-medium">模式</th>
                <th className="py-1.5 pr-2 font-medium">触发</th>
                <th className="py-1.5 pr-2 font-medium">状态</th>
                <th className="py-1.5 pr-2 font-medium">时间</th>
                <th className="py-1.5 pr-2 font-medium">新增 / 更新</th>
                <th className="py-1.5 pr-2 font-medium">错误</th>
              </tr>
            </thead>
            <tbody>
              {runs.map(r => (
                <tr key={r.id} className="border-b last:border-0">
                  <td className="py-1.5 pr-2 tabular-nums">{r.id}</td>
                  <td className="py-1.5 pr-2">{MODE_LABEL[r.mode] ?? r.mode}</td>
                  <td className="py-1.5 pr-2">{r.trigger === 'auto' ? '自动' : '手动'}</td>
                  <td className="py-1.5 pr-2">
                    <Badge
                      variant={r.status === 'done' ? 'secondary' : r.status === 'failed' ? 'destructive' : 'outline'}
                      className="font-normal"
                    >
                      {STATUS_LABEL[r.status] ?? r.status}
                    </Badge>
                  </td>
                  <td className="py-1.5 pr-2 text-muted-foreground">{fmtDateTime(r.started_at)}</td>
                  <td className="py-1.5 pr-2 tabular-nums">{runNum(r, 'inserted')} / {runNum(r, 'updated')}</td>
                  <td className="py-1.5 pr-2 tabular-nums">{runNum(r, 'errors')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </CardContent>
    </Card>
  )
}

function DataPanel({ onChange }: { onChange: () => void }) {
  const [keyword, setKeyword] = React.useState('')
  const [applied, setApplied] = React.useState('')
  const [types, setTypes] = React.useState<number[]>([])
  const [page, setPage] = React.useState(1)
  const [data, setData] = React.useState<SearchResult | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [msg, setMsg] = React.useState('')

  const load = React.useCallback(async () => {
    setBusy(true)
    try {
      setData(await adminSearchSubjects({ keyword: applied, types, page, page_size: 20 }))
    } finally {
      setBusy(false)
    }
  }, [applied, types, page])

  React.useEffect(() => {
    void load()
  }, [load])

  async function refresh(id: number) {
    setMsg('')
    try {
      await adminRefreshSubject(id)
      setMsg(`条目 ${id} 已刷新`)
      await load()
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
      await load()
      onChange()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : '删除失败')
    }
  }

  const totalPages = data ? Math.max(1, Math.ceil(data.total / data.page_size)) : 1

  return (
    <Card>
      <CardHeader><CardTitle className="text-sm">索引数据</CardTitle></CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap items-end gap-2">
          <form
            className="flex flex-1 gap-2"
            onSubmit={e => {
              e.preventDefault()
              setPage(1)
              setApplied(keyword.trim())
            }}
          >
            <Input value={keyword} onChange={e => setKeyword(e.target.value)} placeholder="按名称搜索索引" className="h-8 max-w-xs text-xs" />
            <Button type="submit" size="sm" variant="secondary" className="h-8">搜索</Button>
          </form>
          <div className="flex items-center gap-2">
            {TYPE_ORDER.map(t => (
              <label key={t} className="flex items-center gap-1 text-[11px]">
                <Checkbox
                  checked={types.includes(t)}
                  onCheckedChange={() => {
                    setPage(1)
                    setTypes(v => (v.includes(t) ? v.filter(x => x !== t) : [...v, t]))
                  }}
                />
                {TYPE_LABEL[t]}
              </label>
            ))}
          </div>
        </div>

        {msg && <p className="text-[11px] text-muted-foreground">{msg}</p>}

        <div className="overflow-x-auto">
          <table className="w-full min-w-[720px] text-xs">
            <thead className="text-left text-muted-foreground">
              <tr className="border-b">
                <th className="py-1.5 pr-2 font-medium">ID</th>
                <th className="py-1.5 pr-2 font-medium">名称</th>
                <th className="py-1.5 pr-2 font-medium">类型</th>
                <th className="py-1.5 pr-2 font-medium">排名</th>
                <th className="py-1.5 pr-2 font-medium">评分</th>
                <th className="py-1.5 pr-2 font-medium">标签</th>
                <th className="py-1.5 pr-2 font-medium">抓取时间</th>
                <th className="py-1.5 pr-2 font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {(data?.items ?? []).map(s => (
                <tr key={s.id} className="border-b last:border-0">
                  <td className="py-1.5 pr-2 font-mono tabular-nums">{s.id}</td>
                  <td className="max-w-[220px] truncate py-1.5 pr-2" title={s.name_cn || s.name}>
                    {s.name_cn || s.name}
                  </td>
                  <td className="py-1.5 pr-2">{TYPE_LABEL[s.type] ?? s.type}</td>
                  <td className="py-1.5 pr-2 tabular-nums">{s.rank || '—'}</td>
                  <td className="py-1.5 pr-2 tabular-nums">{s.score || '—'}</td>
                  <td className="py-1.5 pr-2 tabular-nums">{s.tags_total}</td>
                  <td className="py-1.5 pr-2 text-muted-foreground">{fmtDateTime(s.fetched_at)}</td>
                  <td className="py-1.5 pr-2">
                    <div className="flex gap-1">
                      <Button size="sm" variant="ghost" className="h-6 px-1.5 text-[10px]" onClick={() => void refresh(s.id)}>
                        刷新
                      </Button>
                      <Button size="sm" variant="ghost" className="h-6 px-1.5 text-[10px] text-destructive" onClick={() => void remove(s.id)}>
                        <Trash2 size={11} />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {data && (
          <div className="flex items-center justify-between text-[11px] text-muted-foreground">
            <span>
              共 {fmtNum(data.total)} 条{busy ? ' · 加载中…' : ''}
            </span>
            <div className="flex items-center gap-1">
              <Button size="sm" variant="outline" className="h-7 px-2" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>上一页</Button>
              <span className="tabular-nums">{page} / {totalPages}</span>
              <Button size="sm" variant="outline" className="h-7 px-2" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>下一页</Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function NumInput({ label, value, onChange, hint }: {
  label: string
  value: number
  onChange: (v: number) => void
  hint?: string
}) {
  return (
    <div className="space-y-1">
      <Label className="text-[11px] text-muted-foreground">{label}</Label>
      <Input
        type="number"
        value={value}
        onChange={e => onChange(Number(e.target.value) || 0)}
        className="h-8 text-xs tabular-nums"
      />
      {hint && <p className="text-[10px] text-muted-foreground">{hint}</p>}
    </div>
  )
}

function Toggle({ label, checked, onChange, hint }: {
  label: string
  checked: boolean
  onChange: (v: boolean) => void
  hint?: string
}) {
  return (
    <label className="flex items-start gap-2">
      <Checkbox checked={checked} onCheckedChange={v => onChange(v === true)} className="mt-0.5" />
      <span>
        <span className="text-xs">{label}</span>
        {hint && <span className="block text-[10px] text-muted-foreground">{hint}</span>}
      </span>
    </label>
  )
}

function SettingsPanel({ settings, onSaved, onError }: {
  settings: AdminSettings
  onSaved: (msg: string) => Promise<void>
  onError: (msg: string) => void
}) {
  const [crawl, setCrawl] = React.useState<CrawlSettings>(settings.crawl)
  const [token, setToken] = React.useState('')
  const [cookie, setCookie] = React.useState('')
  const [clearToken, setClearToken] = React.useState(false)
  const [clearCookie, setClearCookie] = React.useState(false)
  const [busy, setBusy] = React.useState(false)
  const [testMsg, setTestMsg] = React.useState('')

  const [currentPw, setCurrentPw] = React.useState('')
  const [nextPw, setNextPw] = React.useState('')
  const [pwMsg, setPwMsg] = React.useState('')

  const [net, setNet] = React.useState<NetworkStatus | null>(null)
  const [netBusy, setNetBusy] = React.useState(false)
  const loadNet = React.useCallback(async () => {
    setNetBusy(true)
    try {
      setNet(await adminNetwork())
    } catch {
      /* 诊断失败不阻塞设置页 */
    } finally {
      setNetBusy(false)
    }
  }, [])
  React.useEffect(() => {
    void loadNet()
  }, [loadNet])

  React.useEffect(() => {
    setCrawl(settings.crawl)
  }, [settings])

  function set<K extends keyof CrawlSettings>(key: K, value: CrawlSettings[K]) {
    setCrawl(c => ({ ...c, [key]: value }))
  }

  async function save() {
    setBusy(true)
    try {
      await adminSaveSettings({
        crawl,
        bgm: {
          token: token.trim() || undefined,
          cookie: cookie.trim() || undefined,
          clear_token: clearToken,
          clear_cookie: clearCookie,
        },
      })
      setToken('')
      setCookie('')
      setClearToken(false)
      setClearCookie(false)
      await onSaved('设置已保存')
    } catch (e) {
      onError(e instanceof Error ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  async function test() {
    setTestMsg('校验中…')
    try {
      const res = await adminTestCredential()
      setTestMsg(`校验通过：${JSON.stringify(res)}`)
    } catch (e) {
      setTestMsg(e instanceof Error ? e.message : '校验失败')
    }
  }

  async function changePassword() {
    setPwMsg('')
    try {
      await adminChangePassword(currentPw, nextPw)
      setCurrentPw('')
      setNextPw('')
      setPwMsg('密码已更新，旧会话已失效')
    } catch (e) {
      setPwMsg(e instanceof Error ? e.message : '修改失败')
    }
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader><CardTitle className="text-sm">抓取设置</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-1.5">
            <Label className="text-[11px] text-muted-foreground">抓取类型</Label>
            <div className="flex flex-wrap gap-3">
              {TYPE_ORDER.map(t => (
                <label key={t} className="flex items-center gap-1.5 text-xs">
                  <Checkbox
                    checked={crawl.types.includes(t)}
                    onCheckedChange={() => set('types', crawl.types.includes(t)
                      ? crawl.types.filter(x => x !== t)
                      : [...crawl.types, t])}
                  />
                  {TYPE_LABEL[t]}
                </label>
              ))}
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <NumInput label="每页条数" value={crawl.page_size} onChange={v => set('page_size', v)} hint="10–100" />
            <NumInput label="请求间隔 (ms)" value={crawl.delay_ms} onChange={v => set('delay_ms', v)} hint="越大越礼貌" />
            <NumInput label="请求超时 (s)" value={crawl.request_timeout_sec} onChange={v => set('request_timeout_sec', v)} />
            <NumInput label="重爬 TTL (天)" value={crawl.refresh_ttl_days} onChange={v => set('refresh_ttl_days', v)} hint="超过则视为过期" />
            <NumInput label="重爬批量" value={crawl.refresh_batch} onChange={v => set('refresh_batch', v)} hint="0 = 不自动重爬" />
            <NumInput label="增量页数 / 类型" value={crawl.incremental_pages} onChange={v => set('incremental_pages', v)} />
            <NumInput label="增量间隔 (分钟)" value={crawl.incremental_interval_min} onChange={v => set('incremental_interval_min', v)} />
            <div className="space-y-2 pt-1">
              <Toggle label="自动增量" checked={crawl.auto_incremental} onChange={v => set('auto_incremental', v)} />
              <Toggle label="自动重爬过期" checked={crawl.auto_refresh} onChange={v => set('auto_refresh', v)} />
              <Toggle label="全量后清理旧数据" checked={crawl.prune_on_full} onChange={v => set('prune_on_full', v)} />
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-sm">站长 Bangumi 凭据（抓取用）</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          <p className="text-[11px] leading-relaxed text-muted-foreground">
            仅用于后台抓取。配置后可以抓取需要登录的条目；凭据保存在本机数据库（配置
            <code className="mx-1">ANIPICK_SECRET</code> 时加密）。
          </p>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1">
              <Label className="text-[11px] text-muted-foreground">
                Access Token {settings.bgm.token_configured ? `（已配置：${settings.bgm.token}）` : '（未配置）'}
              </Label>
              <Input
                value={token}
                onChange={e => setToken(e.target.value)}
                placeholder={settings.bgm.token_configured ? '留空表示不修改' : '粘贴 Access Token'}
                className="h-8 font-mono text-xs"
              />
              {settings.bgm.token_configured && (
                <Toggle label="清除 Token" checked={clearToken} onChange={setClearToken} />
              )}
            </div>
            <div className="space-y-1">
              <Label className="text-[11px] text-muted-foreground">
                Cookie {settings.bgm.cookie_configured ? `（已配置：${settings.bgm.cookie}）` : '（未配置）'}
              </Label>
              <Input
                value={cookie}
                onChange={e => setCookie(e.target.value)}
                placeholder={settings.bgm.cookie_configured ? '留空表示不修改' : 'chii_sid=…; chii_auth=…'}
                className="h-8 font-mono text-xs"
              />
              {settings.bgm.cookie_configured && (
                <Toggle label="清除 Cookie" checked={clearCookie} onChange={setClearCookie} />
              )}
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => void test()} className="gap-1.5">
              <Activity size={12} /> 校验凭据
            </Button>
            {testMsg && <span className="text-[11px] text-muted-foreground">{testMsg}</span>}
          </div>
        </CardContent>
      </Card>

      <div className="flex justify-end">
        <Button onClick={() => void save()} disabled={busy} className="gap-1.5">
          {busy ? <Loader2 size={13} className="animate-spin" /> : <Save size={13} />} 保存设置
        </Button>
      </div>

      <Card>
        <CardHeader className="grid-cols-[1fr_auto] items-center">
          <CardTitle className="text-sm">抓取出口（服务端）</CardTitle>
          <Button size="sm" variant="ghost" disabled={netBusy} onClick={() => void loadNet()} className="gap-1">
            {netBusy ? <Loader2 size={12} className="animate-spin" /> : null} 重新检测
          </Button>
        </CardHeader>
        <CardContent className="space-y-2 text-xs">
          <div className="flex flex-wrap gap-1.5">
            <Badge variant={net?.proxy ? 'default' : 'secondary'}>
              出口 {net?.proxy ? '代理' : (net?.doh ? 'DoH 直连' : '未配置')}
            </Badge>
            {!net?.proxy && (
              <>
                <Badge variant={net?.doh ? 'default' : 'secondary'}>DoH {net?.doh ? '已启用' : '未启用'}</Badge>
              </>
            )}
            <Badge variant={net?.upstream_ok ? 'default' : 'destructive'}>
              上游 {net?.upstream_ok ? '可达' : '不可达'}
            </Badge>
          </div>
          <p className="text-muted-foreground">
            解析地址：{net?.diagnostics?.addresses?.join('，') || '—'}
          </p>
          <p className="break-all text-muted-foreground">
            DoH 入口：{net?.diagnostics?.endpoints?.join('，') || '—'}
          </p>
          {net?.upstream_error && <p className="break-all text-destructive">上游错误：{net.upstream_error}</p>}
          {net?.diagnostics?.error && <p className="text-destructive">诊断失败：{net.diagnostics.error}</p>}
          <p className="leading-relaxed text-muted-foreground">
            该通道只用于服务端抓取，不中转任何访客请求。可用 <code>ANIPICK_EGRESS_PROXY</code>
            配置抓取代理（代理负责远端 DNS/TLS）；否则走 DoH 解析。
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-sm">管理员密码</CardTitle></CardHeader>
        <CardContent className="flex flex-wrap items-end gap-2">
          <div className="space-y-1">
            <Label className="text-[11px] text-muted-foreground">当前密码</Label>
            <Input type="password" value={currentPw} onChange={e => setCurrentPw(e.target.value)} className="h-8 w-40 text-xs" />
          </div>
          <div className="space-y-1">
            <Label className="text-[11px] text-muted-foreground">新密码（≥ 6 位）</Label>
            <Input type="password" value={nextPw} onChange={e => setNextPw(e.target.value)} className="h-8 w-40 text-xs" />
          </div>
          <Button size="sm" variant="outline" disabled={!currentPw || nextPw.length < 6} onClick={() => void changePassword()}>
            修改密码
          </Button>
          {pwMsg && <span className="text-[11px] text-muted-foreground">{pwMsg}</span>}
        </CardContent>
      </Card>
    </div>
  )
}
