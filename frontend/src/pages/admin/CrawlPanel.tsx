import * as React from 'react'
import { Activity, ChevronDown, ChevronUp, GripVertical, Loader2 } from 'lucide-react'
import { Panel } from '@/components/panel'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { AdminSettings, AdminOverview, AutoSchedule, Run, SourceSetting, TypeRef } from '../../api/types'
import type { CrawlMode } from '../../api/client'
import { modeLabel, nativeSource, sourceLabel, sourceMeta, sourceModes, typeLabel } from '../../api/vocab'
import { fmtDateTime, STATUS_LABEL } from '../../lib/format'
import { humanizeCrawlError } from '../../lib/crawl-errors'
import {
  ApiError, adminCancelCrawl, adminRematchWorks, adminSaveSettings, adminStartCrawl, adminTestCredential,
} from '../../api/client'
import { CrawlStatusCard, NumField, RunsTable, Toggle } from './parts'

// 抓取设置：来源相关配置与操作的唯一去处。页面结构自上而下：
// 启动抓取（全源入口 + 全局新鲜度默认）→ 运行状态 → 网络 → 各数据源 → 任务表。
// 每个源一张紧凑卡：状态行（徽标/类型/新鲜度/最近运行）+ 控制行（启用/顺序/
// 权重/间隔/保留期 + 该源自己的抓取按钮组），按钮组由登记表经 manifest 声明，
// 照单渲染；抓取类型与更多参数、凭据折叠在卡内展开器里，默认收起。

// 每个数据源的最近一次运行（按 runs 列表倒序取首个匹配）。
function latestRunFor(runs: Run[], source: string): Run | undefined {
  return runs.find(r => r.source === source)
}

function runNum(run: Run, key: 'inserted' | 'updated' | 'errors'): number {
  const s = run.stats as Record<string, unknown>
  const v = s?.[key]
  return typeof v === 'number' ? v : 0
}

// 卡内展开器（默认收起）：原生源的抓取类型/参数/凭据与扩展源凭据收在这里，
// 主行只留状态与控制两行。
function CardDetails({ hint, children }: { hint: string; children: React.ReactNode }) {
  return (
    <details className="mt-2 border-t pt-2">
      <summary className="cursor-pointer select-none text-xs text-muted-foreground transition-colors hover:text-foreground">
        {hint}
      </summary>
      <div className="mt-3 space-y-3">{children}</div>
    </details>
  )
}

// 分钟数 ↔ HH:MM（time 输入框的值与调度存储互转）
function minuteToHHMM(m: number): string {
  const mm = Math.min(Math.max(Math.round(m), 0), 1439)
  return `${String(Math.floor(mm / 60)).padStart(2, '0')}:${String(mm % 60).padStart(2, '0')}`
}
function hhmmToMinute(v: string): number {
  const [h, m] = v.split(':').map(Number)
  return (h || 0) * 60 + (m || 0)
}

// 自动任务调度行：开关 + 调度形式（按间隔 / 每日定时 / 每隔几天定时）+ 随形式
// 变化的字段 + 下次触发估算。调度数据随面板底部保存一起提交。
function ScheduleRow({
  title, hint, checked, onToggle, schedule, onChange, next,
}: {
  title: string
  hint: string
  checked: boolean
  onToggle: (on: boolean) => void
  schedule: AutoSchedule | null
  onChange: (s: AutoSchedule) => void
  next: string | null
}) {
  if (!schedule) return null
  const set = (patch: Partial<AutoSchedule>) => onChange({ ...schedule, ...patch })
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
      <Toggle label={title} checked={checked} onChange={onToggle} />
      <MiniField label="调度方式">
        <Select
          value={schedule.kind}
          onValueChange={k => {
            // 切形式时给未设过的字段补个合理起点（at_minute=0 即 00:00，是合法值不补）
            const seed: Partial<AutoSchedule> = {}
            if (k === 'interval' && !schedule.every_min) seed.every_min = 10
            if (k === 'every_days' && !schedule.every_days) seed.every_days = 2
            set({ ...seed, kind: k as AutoSchedule['kind'] })
          }}
        >
          <SelectTrigger className="h-8 w-[120px] text-xs" aria-label={`${title}调度方式`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="interval">按间隔</SelectItem>
            <SelectItem value="daily">每日定时</SelectItem>
            <SelectItem value="every_days">每隔几天定时</SelectItem>
          </SelectContent>
        </Select>
      </MiniField>
      {schedule.kind === 'interval' ? (
        <MiniField label="间隔">
          <Input
            type="number"
            min={1}
            value={schedule.every_min}
            onChange={e => set({ every_min: Number(e.target.value) || 0 })}
            className="h-8 w-16 text-xs"
            aria-label={`${title}间隔分钟`}
          />
          <span className="text-[11px] text-muted-foreground">分钟</span>
        </MiniField>
      ) : (
        <>
          {schedule.kind === 'every_days' && (
            <MiniField label="每隔">
              <Input
                type="number"
                min={1}
                value={schedule.every_days}
                onChange={e => set({ every_days: Number(e.target.value) || 1 })}
                className="h-8 w-14 text-xs"
                aria-label={`${title}间隔天数`}
              />
              <span className="text-[11px] text-muted-foreground">天</span>
            </MiniField>
          )}
          <MiniField label="开始时间">
            <Input
              type="time"
              value={minuteToHHMM(schedule.at_minute)}
              onChange={e => set({ at_minute: hhmmToMinute(e.target.value) })}
              className="h-8 w-[110px] text-xs"
              aria-label={`${title}开始时间`}
            />
            <span className="text-[11px] text-muted-foreground">服务器本地时间</span>
          </MiniField>
        </>
      )}
      <span className="text-[11px] text-muted-foreground">
        {next === null
          ? (checked ? '下次 ≈ 暂无依据（等最近一次运行）' : '已停用')
          : next === 'now'
            ? '已到期，即将触发'
            : `下次 ≈ ${fmtDateTime(next)}`}
      </span>
      <p className="w-full text-[11px] leading-relaxed text-muted-foreground">{hint}</p>
    </div>
  )
}

// 控制行的内联字段：标签 + 控件同排，全部卡同一结构保证跨卡列对齐。
function MiniField({ label, children, title }: { label: string; children: React.ReactNode; title?: string }) {
  return (
    <label className="flex items-center gap-1.5" title={title}>
      <span className="shrink-0 text-[11px] text-muted-foreground">{label}</span>
      {children}
    </label>
  )
}

export function CrawlPanel({
  overview,
  settings,
  onChangeDraft,
  reloadSettings,
  onRefresh,
  onNotice,
  onError,
}: {
  overview: AdminOverview | null
  /** 当前管理设置草稿（AdminPage 持有的单一事实源；编辑经 onChangeDraft） */
  settings: AdminSettings | null
  /** 修改设置草稿（触发 AdminPage 的自动保存） */
  onChangeDraft: (updater: (d: AdminSettings) => AdminSettings) => void
  /** 回读服务端设置（凭据保存后刷新掩码回显） */
  reloadSettings: () => Promise<void>
  onRefresh: () => Promise<void>
  onNotice: (msg: string) => void
  onError: (msg: string) => void
}) {
  const crawler = overview?.crawler ?? null

  // 抓取设置没有本地副本：全部直接读写 AdminPage 的草稿（自动保存唯一入口）；
  // 只有凭据（token/cookie/各源 key）保留本地草稿——半输入的密钥绝不自动上送
  const crawl = settings?.crawl
  const network = settings?.network
  const sources = settings?.sources ?? {}
  // DoH 入口清单的输入缓冲：允许「a, 」这类中间态（逗号结尾不被吞），草稿里存解析后的数组
  const [epText, setEpText] = React.useState((settings?.network?.doh_endpoints ?? []).join(','))
  const [credDrafts, setCredDrafts] = React.useState<Record<string, string>>({})
  const [token, setToken] = React.useState('')
  const [cookie, setCookie] = React.useState('')
  const [clearToken, setClearToken] = React.useState(false)
  const [clearCookie, setClearCookie] = React.useState(false)
  const [busy, setBusy] = React.useState(false)
  // 每源一条校验消息（键=来源标识；bgm 与扩展源各自展开器里展示）
  const [testMsgs, setTestMsgs] = React.useState<Record<string, string>>({})

  // 通用草稿编辑：crawl/network 块的具名字段
  function setCrawlField<K extends keyof NonNullable<typeof crawl>>(key: K, value: NonNullable<typeof crawl>[K]) {
    onChangeDraft(d => ({ ...d, crawl: { ...d.crawl, [key]: value } }))
  }
  function setNetField<K extends keyof NonNullable<typeof network>>(key: K, value: NonNullable<typeof network>[K]) {
    onChangeDraft(d => ({ ...d, network: { ...d.network, [key]: value } }))
  }
  function setSource(id: string, patch: Partial<SourceSetting>) {
    onChangeDraft(d => ({ ...d, sources: { ...d.sources, [id]: { ...d.sources[id], ...patch } } }))
  }

  const orderedSources = React.useMemo(
    () => Object.entries(sources).sort((a, b) => a[1].priority - b[1].priority),
    [sources],
  )

  // 每源过期条目数（overview 按各源有效保留期判定）
  const staleBySource = React.useMemo(() => {
    const m = new Map<string, number>()
    for (const h of overview?.harvesters ?? []) m.set(h.source, h.stale ?? 0)
    return m
  }, [overview])

  // 每源多源匹配度（行数与已并入多源作品组的行数，匹配率 = merged/rows）
  const matchBySource = React.useMemo(() => {
    const m = new Map<string, { rows: number; merged: number }>()
    for (const h of overview?.harvesters ?? []) m.set(h.source, { rows: h.match_rows ?? 0, merged: h.match_merged ?? 0 })
    return m
  }, [overview])

  const bgmID = nativeSource()?.id
  const globalTTL = crawl?.refresh_ttl_days ?? 0
  // 源有效保留期 = 源覆盖 ?? 全局默认（与后端同一套取值口径）
  function effectiveTTL(id: string): number {
    const o = sources[id]?.refresh_ttl_days ?? 0
    return o > 0 ? o : globalTTL
  }

  // 自动任务下次触发估算（显示用近似，判定以后端为准）：
  // interval = 最近一次完成运行 + 间隔；daily/every_days = 上次自动触发推下一时隙。
  function nextAutoDue(mode: 'incremental' | 'refresh', sch: AutoSchedule | null, enabled: boolean): string | null {
    if (!enabled || !sch || !overview) return null
    const runs = overview.runs ?? []
    if (sch.kind === 'interval') {
      const done = runs
        .filter(r => r.status === 'done' && r.finished_at && (mode === 'incremental' ? r.mode === 'incremental' : true))
        .sort((a, b) => new Date(b.finished_at!).getTime() - new Date(a.finished_at!).getTime())
      if (!done.length) return null
      return new Date(new Date(done[0].finished_at!).getTime() + sch.every_min * 60000).toISOString()
    }
    const lastAuto = runs
      .filter(r => r.mode === mode && r.trigger === 'auto')
      .sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime())[0]
    const cand = new Date()
    cand.setHours(Math.floor(sch.at_minute / 60), sch.at_minute % 60, 0, 0)
    const step = sch.kind === 'every_days' ? Math.max(1, sch.every_days) : 1
    const dayDiff = (a: Date, b: Date) =>
      Math.round((new Date(a.getFullYear(), a.getMonth(), a.getDate()).getTime()
        - new Date(b.getFullYear(), b.getMonth(), b.getDate()).getTime()) / 86400000)
    if (lastAuto) {
      const fire = new Date(lastAuto.started_at)
      // 已到期（时隙已过且天数已满）→ 即将触发
      if (cand.getTime() <= Date.now() && dayDiff(cand, fire) >= step) return 'now'
      for (let i = 0; i < step + 2; i++) {
        if (cand.getTime() > Date.now() && dayDiff(cand, fire) >= step) return cand.toISOString()
        cand.setDate(cand.getDate() + 1)
      }
      return null
    }
    if (cand.getTime() <= Date.now()) cand.setDate(cand.getDate() + 1)
    return cand.toISOString()
  }

  // 上移/下移：与相邻源互换优先级（拖拽的键盘可达替代——WCAG 2.2 拖拽准则）
  function moveSource(idx: number, dir: -1 | 1) {
    const other = idx + dir
    if (other < 0 || other >= orderedSources.length) return
    const [a, b] = [orderedSources[idx], orderedSources[other]]
    onChangeDraft(d => ({
      ...d,
      sources: {
        ...d.sources,
        [a[0]]: { ...d.sources[a[0]], priority: d.sources[b[0]].priority },
        [b[0]]: { ...d.sources[b[0]], priority: d.sources[a[0]].priority },
      },
    }))
  }

  // 源卡拖动排序：把手触发 HTML5 拖拽，落点重排后按新顺序重编优先级（1..N）
  const [dragId, setDragId] = React.useState<string | null>(null)
  const [overId, setOverId] = React.useState<string | null>(null)

  function reorderTo(fromId: string, toId: string) {
    if (fromId === toId) return
    const ids = orderedSources.map(([k]) => k)
    const from = ids.indexOf(fromId)
    const to = ids.indexOf(toId)
    if (from < 0 || to < 0) return
    ids.splice(to, 0, ids.splice(from, 1)[0])
    onChangeDraft(d => {
      const next = { ...d.sources }
      ids.forEach((k, i) => {
        next[k] = { ...next[k], priority: i + 1 }
      })
      return { ...d, sources: next }
    })
  }

  // ---------- 匹配组（多对多类型匹配）编辑 ----------
  const [mgSrc, setMgSrc] = React.useState('')
  const [mgType, setMgType] = React.useState(0)

  function setMatchGroups(g: TypeRef[][]) {
    setCrawlField('match_groups', g)
  }
  function removeMember(gi: number, mi: number) {
    const g = (crawl?.match_groups ?? []).map(x => [...x])
    if (!g[gi]) return
    g[gi].splice(mi, 1)
    setMatchGroups(g)
  }
  function addMember(gi: number) {
    if (!mgSrc || !mgType) return
    const g = (crawl?.match_groups ?? []).map(x => [...x])
    if (!g[gi]) return
    g[gi].push({ source: mgSrc, type: mgType })
    setMatchGroups(g)
  }
  function removeGroup(gi: number) {
    setMatchGroups((crawl?.match_groups ?? []).filter((_, i) => i !== gi))
  }
  function addGroup() {
    setMatchGroups([...(crawl?.match_groups ?? []), []])
  }
  // 组顺序只影响编辑器阅读（匹配按成员集合算，与组顺序无关）：箭头换位 / 拖拽落点重排
  const [mgDrag, setMgDrag] = React.useState<number | null>(null)
  const [mgOver, setMgOver] = React.useState<number | null>(null)

  function moveGroup(gi: number, dir: -1 | 1) {
    const g = [...(crawl?.match_groups ?? [])]
    const to = gi + dir
    if (to < 0 || to >= g.length) return
    ;[g[gi], g[to]] = [g[to], g[gi]]
    setMatchGroups(g)
  }

  function reorderGroupTo(from: number, to: number) {
    if (from === to) return
    const g = [...(crawl?.match_groups ?? [])]
    if (from < 0 || to < 0 || from >= g.length || to >= g.length) return
    g.splice(to, 0, g.splice(from, 1)[0])
    setMatchGroups(g)
  }
  // 推荐值从 manifest 推导：可跨源配对的 kind（动画/电影/剧集）各成一组
  function defaultMatchGroups(): TypeRef[][] {
    const byKind: Record<string, TypeRef[]> = {}
    for (const [id] of orderedSources) {
      for (const t of sourceMeta(id)?.types ?? []) {
        if (t.kind !== 'anime' && t.kind !== 'movie' && t.kind !== 'tv') continue
        ;(byKind[t.kind] ??= []).push({ source: id, type: t.code })
      }
    }
    return Object.values(byKind).filter(g => g.length >= 2)
  }

  // 全源入口：一次合并运行覆盖全部启用中的来源（各源按自己声明的模式跑一轮）
  async function startAll(mode: CrawlMode) {
    if (mode === 'full' && !window.confirm('全量抓取会遍历所有启用来源的发现列表，可能持续数十分钟到数小时；原生索引结束后会清理本轮未覆盖的旧条目。确定开始？')) {
      return
    }
    try {
      const res = await adminStartCrawl(mode)
      onNotice(`已开始全源${modeLabel(mode)}（任务 #${res.run_id}）`)
      await onRefresh()
    } catch (e) {
      onError(e instanceof Error ? e.message : '启动失败')
    }
  }

  // 启动某个源的一种抓取（模式必须是该源声明的能力；按钮组同源照单渲染）
  async function startOne(mode: CrawlMode, id: string) {
    if (mode === 'full' && !window.confirm('全量抓取会遍历该来源的发现列表，可能持续数十分钟到数小时；结束后会清理本轮未覆盖的旧条目。确定开始？')) {
      return
    }
    try {
      const res = await adminStartCrawl(mode, id)
      onNotice(`已开始${modeLabel(mode, id)}（任务 #${res.run_id}）`)
      await onRefresh()
    } catch (e) {
      onError(e instanceof Error ? e.message : '启动失败')
    }
  }

  async function cancelCrawl() {
    try {
      await adminCancelCrawl()
      onNotice('已请求停止任务')
      await onRefresh()
    } catch (e) {
      onError(e instanceof Error ? e.message : '取消失败')
    }
  }

  async function rematch() {
    try {
      onNotice('正在重新配对作品…（先补齐缺失的条目信息，耗时视存量而定）')
      const r = await adminRematchWorks()
      onNotice(
        `重新配对完成：新并入已有作品 ${r.merged} 个，保持独立 ${r.independent} 个，` +
          `同名存疑未合并 ${r.ambiguous} 个，其余 ${r.already_in} 个原本就在同一作品下`,
      )
      await onRefresh()
    } catch (e) {
      onError(e instanceof Error ? e.message : '重新配对作品失败')
    }
  }

  // 按源校验凭据：扩展源用展开器里的草稿值（还没保存也能试），
  // bgm 校验站长凭据（已存的 Token/Cookie）。哪些源有校验器由登记表声明。
  async function testCreds(id: string) {
    setTestMsgs(m => ({ ...m, [id]: '校验中…' }))
    try {
      const isBgm = id === bgmID
      const drafts: Record<string, string> = {}
      if (!isBgm) {
        for (const c of sourceMeta(id)?.credentials ?? []) {
          const val = (credDrafts[`${id}.${c.key}`] ?? '').trim()
          if (val) drafts[c.key] = val
        }
      }
      const res = await adminTestCredential(
        isBgm ? undefined : id,
        !isBgm && Object.keys(drafts).length > 0 ? drafts : undefined,
      )
      const how =
        res.mode === 'read_token' ? '读访问令牌有效（Bearer）'
        : res.mode === 'api_key' ? 'API Key 有效'
        : res.mode === 'token' ? '站长 Token 有效'
        : '未配置凭据，匿名通道可达'
      setTestMsgs(m => ({ ...m, [id]: `校验通过：${how}` }))
    } catch (e) {
      // 带上后端给的修复线索（如上游 401 的原文），不只一句「失败」
      const msg =
        e instanceof ApiError ? (e.hint ? `${e.message}：${e.hint}` : e.message)
        : e instanceof Error ? e.message
        : '校验失败'
      setTestMsgs(m => ({ ...m, [id]: msg }))
    }
  }

  // 凭据显式保存：token/cookie/各源 key 不参与自动保存——半输入的密钥
  // 绝不自动上送，这里用按钮一次性提交并回读掩码
  async function saveCredentials() {
    if (!settings) return
    setBusy(true)
    try {
      const credentials: AdminSettings['credentials'] = {}
      for (const [key, val] of Object.entries(credDrafts)) {
        const v = val.trim()
        if (!v) continue
        const dot = key.indexOf('.')
        const src = key.slice(0, dot)
        const field = key.slice(dot + 1)
        ;(credentials[src] ??= {})[field] = v
      }
      if (token.trim() || clearToken || cookie.trim() || clearCookie) {
        credentials.bgm = {
          ...(token.trim() ? { token: token.trim() } : {}),
          ...(cookie.trim() ? { cookie: cookie.trim() } : {}),
        }
      }
      await adminSaveSettings({
        credentials: Object.keys(credentials).length > 0 ? credentials : undefined,
        bgm: {
          token: token.trim() || undefined,
          cookie: cookie.trim() || undefined,
          clear_token: clearToken,
          clear_cookie: clearCookie,
        },
      })
      setCredDrafts({})
      setToken('')
      setCookie('')
      setClearToken(false)
      setClearCookie(false)
      await reloadSettings()
      onNotice('凭据已保存')
    } catch (e) {
      onError(e instanceof Error ? e.message : '凭据保存失败')
    } finally {
      setBusy(false)
    }
  }

  // 原生源模式的按钮提示动态显示当前生效值（判定参数都能在页面上找到，
  // 保留期取该源的有效值并注明是否覆盖）
  function bgmModeHint(mode: string): string {
    if (!crawl) return ''
    switch (mode) {
      case 'full':
        return `逐类型遍历全部条目（每页 ${crawl.page_size} 条、共 ${crawl.types.length} 类），结束后清理本轮未覆盖的旧条目`
      case 'incremental':
        return `抓最新 ${crawl.incremental_pages} 页/类型并重爬过期条目；自动调度每 ${crawl.incremental_interval_min} 分钟一次`
      case 'refresh': {
        const override = (sources[bgmID ?? '']?.refresh_ttl_days ?? 0) > 0
        return `只处理超过保留期（当前 ${effectiveTTL(bgmID ?? '')} 天${override ? '，本源覆盖' : '，全局默认'}）的条目，按排名优先，单批 ${crawl.refresh_batch} 条`
      }
      default:
        return ''
    }
  }

  return (
    <div className="space-y-3">
      {/* 全源入口：一键启动全部启用来源 + 全局新鲜度默认（显眼位置，不藏在任何源卡里） */}
      <Panel title="启动抓取" contentClassName="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            size="sm"
            className="h-9"
            disabled={crawler?.running}
            onClick={() => void startAll('full')}
            title="全部启用来源各跑一轮全量：原生索引遍历发现列表，其余来源按各自的全量预算抓取"
          >
            全量抓取
          </Button>
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-9"
            disabled={crawler?.running}
            onClick={() => void startAll('incremental')}
            title="全部启用来源各跑一轮增量：原生索引抓最新页并重爬过期条目，其余来源抓各自的增量清单"
          >
            增量抓取
          </Button>
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-9"
            disabled={crawler?.running}
            onClick={() => void startAll('refresh')}
            title="只处理过期数据：原生索引逐条刷新超过保留期的条目；有过期条目的来源整源重抓（保留期按各源覆盖或全局默认判定）"
          >
            过期重爬
          </Button>
        </div>
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-t pt-3">
          <span className="text-xs text-muted-foreground">
            上次全源抓取{' '}
            <span className="text-foreground">
              {overview?.last_all_sources ? fmtDateTime(overview.last_all_sources) : '—'}
            </span>
          </span>
          <MiniField label="全局默认保留期">
            <Input
              type="number"
              min={1}
              value={crawl?.refresh_ttl_days ?? 0}
              onChange={e => setCrawlField('refresh_ttl_days', Number(e.target.value) || 0)}
              className="h-9 w-16 text-xs"
              aria-label="全局默认保留期（天）"
            />
            <span className="text-[11px] text-muted-foreground">天</span>
          </MiniField>
          <p className="w-full text-[11px] leading-relaxed text-muted-foreground">
            条目超过保留期即算过期：过期重爬只处理过期条目（原生索引逐条刷新，有过期条目的来源整源重抓）；
            每个源可在自己的卡里覆盖保留期（0 = 用这里的全局默认）。
          </p>
          {/* 自动任务调度：触发节奏是站点全局行为（不是某个源的参数），三种形式照单可切。
              判定以后端调度器为准，下面的「下次 ≈」只是按最近运行做的显示估算 */}
          <div className="space-y-3 border-t pt-3">
            <ScheduleRow
              title="自动增量"
              hint="到点自动跑一轮增量：原生索引抓最新页并重爬过期条目，其余来源抓各自的增量清单"
              checked={crawl?.auto_incremental ?? false}
              onToggle={n => setCrawlField('auto_incremental', n)}
              schedule={crawl?.incremental_schedule ?? null}
              onChange={s => setCrawlField('incremental_schedule', s)}
              next={nextAutoDue('incremental', crawl?.incremental_schedule ?? null, crawl?.auto_incremental ?? false)}
            />
            <ScheduleRow
              title="自动过期重爬"
              hint="到点自动处理过期条目：原生索引逐条刷新，有过期条目的来源整源重抓"
              checked={crawl?.auto_refresh ?? false}
              onToggle={n => setCrawlField('auto_refresh', n)}
              schedule={crawl?.refresh_schedule ?? null}
              onChange={s => setCrawlField('refresh_schedule', s)}
              next={nextAutoDue('refresh', crawl?.refresh_schedule ?? null, crawl?.auto_refresh ?? false)}
            />
          </div>
          {/* 任务与请求参数（原为代码常量，全部后台可调；默认即推荐值） */}
          {crawl && (
            <details className="border-t pt-2">
              <summary className="cursor-pointer select-none text-xs text-muted-foreground transition-colors hover:text-foreground">
                任务与请求参数（调度精度 · 重试；默认即推荐值）
              </summary>
              <div className="mt-3 space-y-3">
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                  <NumField label="调度检查周期 (s)" value={crawl.scheduler_tick_sec} onChange={n => setCrawlField('scheduler_tick_sec', n)} hint="默认 120 · 范围 30–600；决定定时触发的精度" />
                  <NumField label="调度回看条数" value={crawl.scheduler_lookback} onChange={n => setCrawlField('scheduler_lookback', n)} hint="默认 20 · 范围 5–200" />
                  <NumField label="进度播报节奏 (条)" value={crawl.refresh_progress_every} onChange={n => setCrawlField('refresh_progress_every', n)} hint="默认 20 · 每处理这么多条播报一次" />
                  <NumField label="页失败重试" value={crawl.page_fail_retries} onChange={n => setCrawlField('page_fail_retries', n)} hint="默认 3 · 范围 0–10；超过后整页跳过" />
                  <NumField label="页重试间隔 (s)" value={crawl.page_retry_wait_sec} onChange={n => setCrawlField('page_retry_wait_sec', n)} hint="默认 5 · 范围 1–60" />
                  <NumField label="过期单条跳过阈值" value={crawl.stale_retry_cap} onChange={n => setCrawlField('stale_retry_cap', n)} hint="默认 5 · 连续失败超过即本轮跳过该条" />
                  <NumField label="源间间隔 (ms)" value={crawl.source_gap_ms} onChange={n => setCrawlField('source_gap_ms', n)} hint="默认 2000 · 合并运行里相邻来源的缓冲" />
                  <NumField label="扩展源标签上限" value={crawl.ext_tag_max} onChange={n => setCrawlField('ext_tag_max', n)} hint="默认 8 · 每条目最多入库的标签数" />
                  <NumField label="请求重试次数" value={crawl.request_attempts} onChange={n => setCrawlField('request_attempts', n)} hint="默认 3 · 范围 1–10（429/5xx/网络错）" />
                  <NumField label="重试退避基数 (ms)" value={crawl.request_backoff_ms} onChange={n => setCrawlField('request_backoff_ms', n)} hint="默认 2000 · 按平方递增" />
                  <NumField label="429 等待封顶 (s)" value={crawl.retry_after_cap_sec} onChange={n => setCrawlField('retry_after_cap_sec', n)} hint="默认 30 · 上游要求更长也截断" />
                  <NumField label="响应体积上限 (MB)" value={crawl.response_limit_mb} onChange={n => setCrawlField('response_limit_mb', n)} hint="默认 8 · 防异常响应撑爆内存" />
                  <NumField label="扩展源请求超时 (s)" value={crawl.outbound_timeout_sec} onChange={n => setCrawlField('outbound_timeout_sec', n)} hint="默认 30 · bgm 用上方「请求超时」" />
                </div>
              </div>
            </details>
          )}
        </div>
      </Panel>

      <CrawlStatusCard status={crawler} onCancel={() => void cancelCrawl()} />

      {/* 网络（出站代理 + DoH/ECH）：保存后立即重建出站通道，无需重启 */}
      <Panel title="网络" contentClassName="space-y-3">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <span className="text-sm font-medium">出站代理</span>
          <Badge
            variant={network?.egress_proxy ? 'default' : 'secondary'}
            className="font-normal"
          >
            {network?.egress_proxy ? '代理' : network?.doh ? 'DoH' : '直连'}
          </Badge>
          <Input
            id="egress-proxy"
            value={network?.egress_proxy ?? ''}
            onChange={e => setNetField('egress_proxy', e.target.value)}
            placeholder="http:// 或 socks5:// 主机:端口；留空 = 走 DoH 或直连"
            className="h-9 min-w-56 flex-1 font-mono text-xs sm:max-w-md"
          />
          <p className="w-full text-[11px] leading-relaxed text-muted-foreground">
            某来源连不上（网络不可达或被墙）时，在这里填抓取代理并保存，所有来源的抓取随即走新通道；只影响服务端抓取，不中转访客请求。填错地址会保留当前通道并提示。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-x-6 gap-y-2 border-t pt-3">
          <Toggle label="DoH 解析（出站 DNS 走加密查询）" checked={network?.doh ?? true} onChange={n => setNetField('doh', n)} />
          <Toggle label="ECH（DoH 启用时生效）" checked={network?.ech ?? true} onChange={n => setNetField('ech', n)} />
        </div>
        <div className="space-y-1">
          <Label htmlFor="doh-endpoints" className="text-xs text-muted-foreground">
            DoH 入口清单{' '}
            <span className="text-muted-foreground">（逗号分隔；留空 = 内置默认）</span>
          </Label>
          <Input
            id="doh-endpoints"
            value={epText}
            onChange={e => {
              setEpText(e.target.value)
              setNetField('doh_endpoints', e.target.value.split(',').map(x => x.trim()).filter(Boolean))
            }}
            placeholder="https://dns.alidns.com/resolve,https://1.1.1.1/dns-query"
            className="h-9 font-mono text-xs"
          />
          <p className="text-[11px] leading-relaxed text-muted-foreground">
            代理 / DoH / ECH 任一变化，保存后立即重建出站通道（无需重启），所有来源的抓取同步换新。
          </p>
        </div>
        <div className="grid gap-3 border-t pt-3 sm:grid-cols-2 lg:grid-cols-4">
          <NumField
            label="DoH 解析超时 (s)"
            value={network?.doh_timeout_sec ?? 0}
            onChange={n => setNetField('doh_timeout_sec', n)}
            hint="默认 6 · 范围 1–30"
          />
          <NumField
            label="DNS 缓存最短 (s)"
            value={network?.dns_cache_min_sec ?? 0}
            onChange={n => setNetField('dns_cache_min_sec', n)}
            hint="默认 60 · 范围 10–3600"
          />
          <NumField
            label="DNS 缓存默认 (s)"
            value={network?.dns_cache_ttl_sec ?? 0}
            onChange={n => setNetField('dns_cache_ttl_sec', n)}
            hint="默认 300 · 范围 30–86400"
          />
          <div className="space-y-1 sm:col-span-2 lg:col-span-1">
            <Label htmlFor="outbound-ua" className="text-xs text-muted-foreground">扩展源 User-Agent</Label>
            <Input
              id="outbound-ua"
              value={network?.outbound_ua ?? ''}
              onChange={e => setNetField('outbound_ua', e.target.value)}
              placeholder="留空 = 内置默认"
              className="h-9 font-mono text-xs"
            />
            <p className="text-[11px] text-muted-foreground">扩展源出站请求的身份标识</p>
          </div>
        </div>
      </Panel>

      {/* 各数据源：一源一卡，同构——状态行 + 控制行 + 展开器 */}
      <Panel title="各数据源" contentClassName="space-y-2">
        {orderedSources.map(([id, v], idx) => {
          const meta = sourceMeta(id)
          const isBgm = id === bgmID
          const creds = meta?.credentials ?? []
          const modes = sourceModes(id)
          const run = overview ? latestRunFor(overview.runs, id) : undefined
          const stale = staleBySource.get(id) ?? 0
          const match = matchBySource.get(id)
          // 抓取按钮禁用条件：有任务在跑；扩展源还需凭据已配置且设置启用中
          const crawlBlocked =
            !!crawler?.running || (!isBgm && (!meta?.configured || !v.enabled))
          const blockReason = crawler?.running
            ? '已有抓取任务在运行'
            : !isBgm && !meta?.configured
              ? `${sourceLabel(id)} 未配置凭据，先在展开器里填写`
              : !isBgm && !v.enabled
                ? '该来源已停用，先打开启用开关'
                : ''
          const showDetails = isBgm || creds.length > 0

          return (
            <div
              key={id}
              draggable={dragId === id}
              onDragOver={e => {
                if (dragId && dragId !== id) {
                  e.preventDefault()
                  setOverId(id)
                }
              }}
              onDragLeave={() => setOverId(o => (o === id ? null : o))}
              onDrop={e => {
                e.preventDefault()
                if (dragId) reorderTo(dragId, id)
                setDragId(null)
                setOverId(null)
              }}
              className={`group/row relative rounded-lg border py-2.5 pl-9 pr-3 transition-opacity ${
                dragId === id ? 'opacity-50' : ''
              } ${overId === id && dragId !== id ? 'border-primary bg-primary/5' : ''}`}
            >
              {/* 状态行：身份 + 新鲜度 + 最近运行（错误人话 + 详情折叠，不靠展开） */}
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="text-sm font-medium">{sourceLabel(id)}</span>
                  {meta && !meta.configured && (
                    <Badge variant="outline" className="text-[10px] text-amber-600">未配置</Badge>
                  )}
                  {!v.enabled && (
                    <Badge variant="outline" className="text-[10px] text-muted-foreground">已停用</Badge>
                  )}
                  {stale > 0 && (
                    <Badge
                      variant="outline"
                      className="text-[10px] text-amber-600"
                      title={`超过保留期（${effectiveTTL(id)} 天${(v.refresh_ttl_days ?? 0) > 0 ? '，本源覆盖' : '，全局默认'}）的条目数`}
                    >
                      过期 {stale}
                    </Badge>
                  )}
                  {match && match.rows > 0 && (
                    <Badge
                      variant="outline"
                      className="text-[10px] text-muted-foreground"
                      title="该源行数与已并入多源作品组的行数（匹配率 = merged/rows）"
                    >
                      多源匹配率 {Math.round((match.merged / match.rows) * 100)}%（{match.merged}/{match.rows}）
                    </Badge>
                  )}
                </div>
                {/* 原生类型：该源自己产出的类型与原生叫法（站点不改名） */}
                <div className="flex flex-wrap items-center gap-1">
                  {meta?.types.map(t => (
                    <Badge
                      key={t.code}
                      variant="secondary"
                      className="px-1.5 py-0 text-[10px] font-normal"
                      title={`类型 ${t.code}`}
                    >
                      {t.label}
                    </Badge>
                  ))}
                </div>
                {/* 最近运行摘要 + 下次自动计划（如有），失败展开看原文 */}
                <span className="ml-auto text-right text-[11px] leading-relaxed text-muted-foreground">
                  {run ? (
                    <>
                      最近 {fmtDateTime(run.started_at)} · 新增 {runNum(run, 'inserted')} / 更新{' '}
                      {runNum(run, 'updated')} ·{' '}
                      <Badge
                        variant={
                          run.status === 'done' ? 'secondary' : run.status === 'failed' ? 'destructive' : 'outline'
                        }
                        className="px-1 py-0 text-[10px] font-normal"
                      >
                        {STATUS_LABEL[run.status] ?? run.status}
                      </Badge>
                      {run.status === 'skipped' && run.error && (
                        <span className="ml-1">{humanizeCrawlError(sourceLabel(id), run.error)}</span>
                      )}
                      {run.status === 'failed' && run.error && (
                        <details className="ml-1 inline">
                          <summary className="cursor-pointer text-destructive">
                            {humanizeCrawlError(sourceLabel(id), run.error)}
                          </summary>
                          <p className="mt-1 whitespace-pre-wrap break-all text-left">{run.error}</p>
                        </details>
                      )}
                    </>
                  ) : (
                    '尚未运行'
                  )}
                  {/* 自动任务的下一次触发估算在上方「启动抓取」卡的自动任务区 */}
                </span>
              </div>

              {/* 控制行：全部卡同一组字段、同一顺序、同一控件高度——跨卡列对齐 */}
              <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2 border-t pt-2">
                <Toggle
                  label="启用"
                  checked={v.enabled}
                  onChange={on => setSource(id, { enabled: on })}
                />
                {/* 排序在卡片左缘竖排（↑ ⋮ ↓，见下方排序轨道） */}
                <MiniField label="权重" title="评分权重 0-100；0 = 不参与合成评分">
                  <Input
                    type="number"
                    min={0}
                    max={100}
                    value={v.weight}
                    onChange={e => setSource(id, { weight: Number(e.target.value) || 0 })}
                    className="h-9 w-16 text-xs"
                    aria-label={`${sourceLabel(id)} 评分权重`}
                  />
                </MiniField>
                <MiniField label="间隔 (ms)" title={`0 = 用默认 ${meta?.interval_default ?? '—'}ms`}>
                  <Input
                    type="number"
                    min={0}
                    value={v.interval_ms === 0 ? '' : v.interval_ms}
                    onChange={e => setSource(id, { interval_ms: Number(e.target.value) || 0 })}
                    placeholder={String(meta?.interval_default ?? '')}
                    className="h-9 w-20 text-xs"
                    aria-label={`${sourceLabel(id)} 请求间隔（毫秒）`}
                  />
                </MiniField>
                <MiniField
                  label="保留期 (天)"
                  title={`超过保留期的条目算过期；0 = 用全局默认 ${globalTTL} 天`}
                >
                  <Input
                    type="number"
                    min={0}
                    value={v.refresh_ttl_days === 0 ? '' : v.refresh_ttl_days}
                    onChange={e => setSource(id, { refresh_ttl_days: Number(e.target.value) || 0 })}
                    placeholder={String(globalTTL)}
                    className="h-9 w-16 text-xs"
                    aria-label={`${sourceLabel(id)} 保留期覆盖（天）`}
                  />
                </MiniField>
                {/* 抓取按钮组：该源在登记表里声明的模式照单渲染 */}
                {modes.length > 0 && (
                  <div className="ml-auto flex flex-wrap items-center gap-2">
                    {modes.map(m => (
                      <Button
                        key={m.mode}
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-9"
                        disabled={crawlBlocked}
                        title={blockReason || `${m.label}：${isBgm && crawl ? bgmModeHint(m.mode) : (m.hint ?? '')}`}
                        onClick={() => void startOne(m.mode as CrawlMode, id)}
                      >
                        {m.label}
                      </Button>
                    ))}
                  </div>
                )}
              </div>

              {/* 排序轨道：左缘竖排 ↑ ⋮ ↓——拖拽把手悬停显形，箭头常驻
                  （WCAG 2.2 拖拽准则要求非拖拽替代）。序位 = 资料以谁为准 + 作品归类依据 */}
              <div className="absolute left-1.5 top-1/2 flex -translate-y-1/2 flex-col items-center gap-0.5">
                <Button type="button" size="xs" variant="ghost" className="h-6 w-6 px-0"
                  disabled={idx === 0}
                  aria-label={`${sourceLabel(id)} 上移`}
                  onClick={() => moveSource(idx, -1)}>
                  <ChevronUp className="size-3.5" />
                </Button>
                <span
                  draggable
                  onDragStart={e => {
                    setDragId(id)
                    e.dataTransfer.effectAllowed = 'move'
                    e.dataTransfer.setData('text/plain', id)
                  }}
                  onDragEnd={() => {
                    setDragId(null)
                    setOverId(null)
                  }}
                  className="cursor-grab rounded p-0.5 text-muted-foreground/40 opacity-0 transition-opacity group-hover/row:opacity-100 focus-visible:opacity-100 hover:text-foreground active:cursor-grabbing"
                  aria-label={`${sourceLabel(id)} 拖动排序`}
                >
                  <GripVertical className="size-4" />
                </span>
                <Button type="button" size="xs" variant="ghost" className="h-6 w-6 px-0"
                  disabled={idx === orderedSources.length - 1}
                  aria-label={`${sourceLabel(id)} 下移`}
                  onClick={() => moveSource(idx, 1)}>
                  <ChevronDown className="size-3.5" />
                </Button>
              </div>

              {/* 展开器（默认收起）：bgm 是抓取类型/参数/凭据，扩展源是凭据与校验。
                  请求间隔与保留期已上移到控制行，参数堆里只剩原生专属参数。 */}
              {showDetails && (
                <CardDetails
                  hint={
                    isBgm
                      ? '抓取配置（类型 · 参数 · 凭据）'
                      : `凭据（${creds.map(c => c.label).join(' · ')}）`
                  }
                >
                  {isBgm && crawl && (
                    <>
                      <div className="space-y-1.5">
                        <p className="text-xs text-muted-foreground">抓取类型</p>
                        <div className="flex flex-wrap gap-x-5 gap-y-2">
                          {nativeSource()?.types.map(t => (
                            <label key={t.code} className="flex items-center gap-1.5 text-sm">
                              <Checkbox
                                checked={crawl.types.includes(t.code)}
                                onCheckedChange={() =>
                                  setCrawlField(
                                    'types',
                                    crawl.types.includes(t.code)
                                      ? crawl.types.filter(x => x !== t.code)
                                      : [...crawl.types, t.code],
                                  )
                                }
                              />
                              {typeLabel(t.code, bgmID)}
                            </label>
                          ))}
                        </div>
                      </div>
                      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                        <NumField label="每页条数" value={crawl.page_size} onChange={n => setCrawlField('page_size', n)} hint="10–100" />
                        <NumField label="请求超时 (s)" value={crawl.request_timeout_sec} onChange={n => setCrawlField('request_timeout_sec', n)} />
                        <NumField label="重爬批量" value={crawl.refresh_batch} onChange={n => setCrawlField('refresh_batch', n)} hint="0 = 不自动重爬" />
                        <NumField label="增量页数 / 类型" value={crawl.incremental_pages} onChange={n => setCrawlField('incremental_pages', n)} />
                      </div>
                      {/* 自动增量/自动过期重爬的调度是站点全局行为，在上方「启动抓取」卡配置；
                          这里只留原生源自己的抓取参数与全量清理开关 */}
                      <div className="flex flex-wrap gap-x-6 gap-y-2">
                        <Toggle label="全量后清理旧数据" checked={crawl.prune_on_full} onChange={n => setCrawlField('prune_on_full', n)} />
                      </div>
                      {/* 站长凭据：两列对齐的表单栅格（Token 行 / Cookie 行，
                          每行 = 标签 + 输入框 + 清除开关内联），校验按钮固定右下 */}
                      <div className="grid gap-x-4 gap-y-3 border-t pt-3 sm:grid-cols-2">
                        <div className="space-y-1">
                          <Label htmlFor="bgm-token" className="text-xs text-muted-foreground">
                            Access Token{' '}
                            <span className="text-muted-foreground">
                              {settings?.bgm.token_configured ? `（已配置 ${settings.bgm.token}）` : '（未配置，选填）'}
                            </span>
                          </Label>
                          <div className="flex items-center gap-2">
                            <Input
                              id="bgm-token"
                              value={token}
                              onChange={e => setToken(e.target.value)}
                              placeholder={settings?.bgm.token_configured ? '留空表示不修改' : '粘贴 Access Token'}
                              className="h-9 flex-1 font-mono text-xs"
                            />
                            {settings?.bgm.token_configured && (
                              <div className="shrink-0">
                                <Toggle label="清除" checked={clearToken} onChange={setClearToken} />
                              </div>
                            )}
                          </div>
                        </div>
                        <div className="space-y-1">
                          <Label htmlFor="bgm-cookie" className="text-xs text-muted-foreground">
                            Cookie{' '}
                            <span className="text-muted-foreground">
                              {settings?.bgm.cookie_configured ? `（已配置 ${settings.bgm.cookie}）` : '（未配置，选填）'}
                            </span>
                          </Label>
                          <div className="flex items-center gap-2">
                            <Input
                              id="bgm-cookie"
                              value={cookie}
                              onChange={e => setCookie(e.target.value)}
                              placeholder={settings?.bgm.cookie_configured ? '留空表示不修改' : 'chii_sid=…; chii_auth=…'}
                              className="h-9 flex-1 font-mono text-xs"
                            />
                            {settings?.bgm.cookie_configured && (
                              <div className="shrink-0">
                                <Toggle label="清除" checked={clearCookie} onChange={setClearCookie} />
                              </div>
                            )}
                          </div>
                        </div>
                      </div>
                      <div className="flex flex-wrap items-center justify-end gap-2">
                        {testMsgs[bgmID] && (
                          <span className="mr-auto min-w-0 flex-1 break-all text-[11px] text-muted-foreground">
                            {testMsgs[bgmID]}
                          </span>
                        )}
                        <Button type="button" variant="outline" size="sm" onClick={() => void testCreds(bgmID)} className="gap-1.5">
                          <Activity size={14} aria-hidden /> 校验凭据
                        </Button>
                        <Button type="button" size="sm" onClick={() => void saveCredentials()} disabled={busy}>
                          {busy ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
                          保存凭据
                        </Button>
                      </div>
                    </>
                  )}

                  {/* 扩展源：收获预算（照 manifest 声明渲染，0 = 登记表默认）+ 凭据 */}
                  {!isBgm && (
                    <>
                      {(meta?.page_size_max || meta?.full_pages || meta?.inc_pages || meta?.tag_min_rank || meta?.region) && (
                        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                          {meta?.page_size_max ? (
                            <NumField
                              label="单页条数"
                              value={v.page_size}
                              onChange={n => setSource(id, { page_size: n })}
                              placeholder={`0 = 默认 ${meta.page_size}`}
                              hint={`上限 ${meta.page_size_max}（上游 API 硬顶）`}
                            />
                          ) : null}
                          {meta?.full_pages ? (
                            <NumField
                              label="全量页数"
                              value={v.full_pages}
                              onChange={n => setSource(id, { full_pages: n })}
                              placeholder={`0 = 默认 ${meta.full_pages}`}
                              hint="全量抓取枚举的发现列表页数"
                            />
                          ) : null}
                          {meta?.inc_pages ? (
                            <NumField
                              label="增量页数"
                              value={v.inc_pages}
                              onChange={n => setSource(id, { inc_pages: n })}
                              placeholder={`0 = 默认 ${meta.inc_pages}`}
                              hint="增量抓取取发现列表头部的页数"
                            />
                          ) : null}
                          {meta?.tag_min_rank ? (
                            <NumField
                              label="标签门槛 (rank)"
                              value={v.tag_min_rank}
                              onChange={n => setSource(id, { tag_min_rank: n })}
                              placeholder={`0 = 默认 ${meta.tag_min_rank}`}
                              hint="相关度低于门槛的标签不入库（0–100）"
                            />
                          ) : null}
                          {meta?.region ? (
                            <div className="space-y-1">
                              <Label className="text-xs text-muted-foreground">商店地区</Label>
                              <Input
                                value={v.region}
                                onChange={e => setSource(id, { region: e.target.value.toLowerCase() })}
                                placeholder={`默认 ${meta.region}`}
                                className="h-9 w-24 font-mono text-xs"
                              />
                              <p className="text-[11px] text-muted-foreground">两位代码（如 tw / jp）；留空 = 默认</p>
                            </div>
                          ) : null}
                        </div>
                      )}
                      <div className="grid gap-x-4 gap-y-3 sm:grid-cols-2">
                        {creds.map(c => (
                          <div key={c.key} className="space-y-1">
                            <Label className="text-xs text-muted-foreground">{c.label}</Label>
                            <Input
                              type={c.secret ? 'password' : 'text'}
                              className="h-9 font-mono text-xs"
                              placeholder={settings?.credentials?.[id]?.[c.key] ? '已保存，输入新值可更换' : '未填写'}
                              value={credDrafts[`${id}.${c.key}`] ?? ''}
                              onChange={e => setCredDrafts(d => ({ ...d, [`${id}.${c.key}`]: e.target.value }))}
                            />
                          </div>
                        ))}
                      </div>
                      <div className="flex flex-wrap items-center justify-end gap-2">
                        {testMsgs[id] && (
                          <span className="mr-auto min-w-0 flex-1 break-all text-[11px] text-muted-foreground">
                            {testMsgs[id]}
                          </span>
                        )}
                        {meta?.validate && creds.length > 0 && (
                          <Button type="button" variant="outline" size="sm" onClick={() => void testCreds(id)} className="gap-1.5">
                            <Activity size={14} aria-hidden /> 校验凭据
                          </Button>
                        )}
                        {creds.length > 0 && (
                          <Button type="button" size="sm" onClick={() => void saveCredentials()} disabled={busy}>
                            {busy ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
                            保存凭据
                          </Button>
                        )}
                      </div>
                    </>
                  )}
                </CardDetails>
              )}
            </div>
          )
        })}
        <p className="pt-1 text-[11px] leading-relaxed text-muted-foreground">
          各源只抓自己：按钮组就是该源声明的能力；条目资料以谁为准看排序，评分按权重合成，保留期各源可覆盖。
          修改后自动保存（右上角有保存状态）；凭据在各源展开区单独保存。
        </p>
      </Panel>

      {/* 配对与匹配：作用于上方各数据源——匹配组决定谁和谁允许配对，
          重新配对作品把组规则应用到既有条目；合并结果与作品分类的总开关区 */}
      <Panel title="配对与匹配" contentClassName="space-y-3">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <Button type="button" size="sm" className="h-9" onClick={() => void rematch()}
            title="按各源条目信息重新把同一作品配到一起：同名同年同类才合并，拿不准就不合并">
            重新配对作品
          </Button>
          {crawl && (
            <>
              <NumField label="合并年份容差 (±年)" value={crawl.match_year_window} onChange={n => setCrawlField('match_year_window', n)} hint="默认 1 · 0=必须同年；改大易误并，慎动" />
              <NumField label="重新配对分批 (条)" value={crawl.rematch_batch} onChange={n => setCrawlField('rematch_batch', n)} hint="默认 100 · 范围 10–1000" />
              <NumField label="同级打平日期容差 (天)" value={crawl.match_date_days} onChange={n => setCrawlField('match_date_days', n)} hint="默认 14 · 范围 1–60；打平且恰有一个作品首播日期在此范围内即并入，调小更严" />
            </>
          )}
        </div>
        {crawl && (
          <details className="border-t pt-2">
            <summary className="cursor-pointer select-none text-xs text-muted-foreground transition-colors hover:text-foreground">
              匹配组（多对多类型匹配：决定哪些来源的哪些类型之间允许配对；冲突按源优先级归类）
            </summary>
            <div className="mt-3 space-y-2">
              {(crawl.match_groups ?? []).map((g, gi) => (
                <div
                  key={gi}
                  onDragOver={e => {
                    e.preventDefault()
                    setMgOver(gi)
                  }}
                  onDragLeave={() => setMgOver(o => (o === gi ? null : o))}
                  onDrop={e => {
                    e.preventDefault()
                    if (mgDrag !== null) reorderGroupTo(mgDrag, gi)
                    setMgDrag(null)
                    setMgOver(null)
                  }}
                  className={`group/mg flex flex-wrap items-center gap-x-2 gap-y-1.5 rounded-md border bg-muted/30 px-2.5 py-2 transition-opacity ${
                    mgDrag === gi ? 'opacity-50' : ''
                  } ${mgOver === gi && mgDrag !== null && mgDrag !== gi ? 'border-primary bg-primary/5' : ''}`}
                >
                  {/* 组排序：左缘横排 ↑ ⋮ ↓，与源卡同一套词汇（把手悬停显形，箭头常驻） */}
                  <div className="flex shrink-0 items-center gap-0.5">
                    <Button type="button" size="xs" variant="ghost" className="h-6 w-6 px-0"
                      disabled={gi === 0}
                      aria-label={`匹配组 ${gi + 1} 上移`}
                      onClick={() => moveGroup(gi, -1)}>
                      <ChevronUp className="size-3.5" />
                    </Button>
                    <span
                      draggable
                      onDragStart={e => {
                        setMgDrag(gi)
                        e.dataTransfer.effectAllowed = 'move'
                        e.dataTransfer.setData('text/plain', String(gi))
                      }}
                      onDragEnd={() => {
                        setMgDrag(null)
                        setMgOver(null)
                      }}
                      className="cursor-grab rounded p-0.5 text-muted-foreground/40 opacity-0 transition-opacity group-hover/mg:opacity-100 focus-visible:opacity-100 hover:text-foreground active:cursor-grabbing"
                      aria-label={`匹配组 ${gi + 1} 拖动排序`}
                    >
                      <GripVertical className="size-3.5" />
                    </span>
                    <Button type="button" size="xs" variant="ghost" className="h-6 w-6 px-0"
                      disabled={gi === (crawl.match_groups?.length ?? 0) - 1}
                      aria-label={`匹配组 ${gi + 1} 下移`}
                      onClick={() => moveGroup(gi, 1)}>
                      <ChevronDown className="size-3.5" />
                    </Button>
                  </div>
                  {g.map((m, mi) => (
                    <span
                      key={`${m.source}:${m.type}:${mi}`}
                      className="inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs"
                    >
                      {sourceLabel(m.source)}·{typeLabel(m.type, m.source)}
                      <button
                        type="button"
                        aria-label="移除该成员"
                        className="text-muted-foreground transition-colors hover:text-destructive"
                        onClick={() => removeMember(gi, mi)}
                      >
                        ×
                      </button>
                    </span>
                  ))}
                  <div className="ml-auto flex items-center gap-1.5">
                    <Select value={mgSrc} onValueChange={v => { setMgSrc(v); setMgType(0) }}>
                      <SelectTrigger className="h-7 w-[110px] text-xs" aria-label="选择来源">
                        <SelectValue placeholder="选来源" />
                      </SelectTrigger>
                      <SelectContent>
                        {orderedSources.map(([sid]) => (
                          <SelectItem key={sid} value={sid}>{sourceLabel(sid)}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Select value={mgType ? String(mgType) : ''} onValueChange={v => setMgType(Number(v))}>
                      <SelectTrigger className="h-7 w-[100px] text-xs" aria-label="选择类型">
                        <SelectValue placeholder="选类型" />
                      </SelectTrigger>
                      <SelectContent>
                        {(sourceMeta(mgSrc)?.types ?? []).map(t => (
                          <SelectItem key={t.code} value={String(t.code)}>{t.label}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Button type="button" size="sm" variant="outline" className="h-7 px-2 text-xs"
                      disabled={!mgSrc || !mgType}
                      onClick={() => addMember(gi)}>
                      添加
                    </Button>
                    <button type="button" className="text-[11px] text-muted-foreground transition-colors hover:text-destructive"
                      onClick={() => removeGroup(gi)}>
                      删除组
                    </button>
                  </div>
                </div>
              ))}
              <div className="flex flex-wrap items-center gap-2">
                <Button type="button" size="sm" variant="outline" className="h-7 px-2 text-xs"
                  onClick={() => addGroup()}>
                  新增空组
                </Button>
                <Button type="button" size="sm" variant="ghost" className="h-7 px-2 text-xs text-muted-foreground"
                  onClick={() => setMatchGroups(defaultMatchGroups())}>
                  恢复推荐值
                </Button>
                <span className="text-[11px] text-muted-foreground">
                  当前 {orderedSources.length} 个来源按设置顺序展示，序位即源优先级（左侧手柄拖动排序）
                </span>
              </div>
              <p className="text-[11px] leading-relaxed text-muted-foreground">
                匹配组是多对多的：同一来源类型可进多个组——把「TMDB·剧集」「TMDB·电影」也加进动画组后，
                TMDB 收录的动画剧与剧场版就能和 bgm/AniList/MAL 动画配对；TMDB 的真人剧集仍在剧集组里
                与 bgm 三次元配对。注意：加入后真人剧集也会获得动画组候选——同名同年的真人改编撞上动画
                本尊时，两边都是 bgm 会按同级打平拒绝（宁缺毋滥）。
                默认推荐值 = 动画（bgm/AniList/MAL）、电影（Apple TV/TMDB）、剧集（bgm 三次元/TMDB）各一组。
                组的排列只为读着顺眼（左侧可拖动/箭头排序），不影响匹配；改动自动保存后，
                点上方「重新配对作品」对既有条目生效。
              </p>
            </div>
          </details>
        )}
      </Panel>

      <RunsTable />
    </div>
  )
}
