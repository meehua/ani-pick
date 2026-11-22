import * as React from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { LogOut } from 'lucide-react'
import {
  ApiError, adminCancelCrawl, adminGetSettings, adminLogout, adminMetrics, adminMetricsReset,
  adminOverview, adminSaveSettings, adminSession,
} from '../api/client'
import { setPageCacheMax } from '../lib/page-cache'
import type { AdminOverview, AdminSettings, MetricsSnapshot } from '../api/types'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { Notice, PageHead, RouteLoading } from '@/components/ui-kit'
import { resetMetaCache } from '../lib/use-meta'
import { OverviewPanel } from './admin/OverviewPanel'
import { CrawlPanel } from './admin/CrawlPanel'
import { DataPanel } from './admin/DataPanel'
import { SettingsPanel } from './admin/SettingsPanel'
import { PerfPanel } from './admin/PerfPanel'

// tab 顺序：概览 / 数据 / 抓取设置 / 性能 / 设置（数据第二、设置最后）
const TABS = ['overview', 'data', 'crawl', 'perf', 'settings'] as const
type Tab = (typeof TABS)[number]

// 自动保存防抖：停止修改 900ms 后整体保存（凭据除外——凭据保持显式保存）
const AUTOSAVE_DEBOUNCE_MS = 900

export default function AdminPage() {
  const navigate = useNavigate()
  const [sp, setSp] = useSearchParams()
  const tabParam = sp.get('tab') as Tab | null
  const tab: Tab = tabParam && TABS.includes(tabParam) ? tabParam : 'overview'
  const setTab = (t: string) => {
    const p = new URLSearchParams(sp)
    if (t === 'overview') p.delete('tab')
    else p.set('tab', t)
    setSp(p, { replace: true })
  }

  const [ready, setReady] = React.useState(false)
  const [overview, setOverview] = React.useState<AdminOverview | null>(null)
  // settings 是唯一可编辑草稿（单一事实源）：面板经 updateSettings 修改，
  // 自动保存把整份草稿发回去——没有跨面板覆盖，也没有切 tab 丢编辑。
  const [settings, setSettings] = React.useState<AdminSettings | null>(null)
  const [saveVersion, setSaveVersion] = React.useState(0)
  const [saveStatus, setSaveStatus] = React.useState<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const [savedAt, setSavedAt] = React.useState('')

  const settingsRef = React.useRef<AdminSettings | null>(null)
  settingsRef.current = settings

  // updateSettings：面板唯一的编辑入口（不可变更新 + 触发自动保存）
  const updateSettings = React.useCallback((updater: (d: AdminSettings) => AdminSettings) => {
    setSettings(prev => (prev ? updater(prev) : prev))
    setSaveVersion(v => v + 1)
  }, [])

  React.useEffect(() => {
    setPageCacheMax(settings?.server?.page_cache_max ?? 12)
  }, [settings?.server?.page_cache_max])
  const [notice, setNotice] = React.useState('')
  const [error, setError] = React.useState('')
  const [metrics, setMetrics] = React.useState<MetricsSnapshot | null>(null)

  const loadOverview = React.useCallback(async () => {
    try {
      setOverview(await adminOverview())
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) navigate('/admin/login', { replace: true })
    }
  }, [navigate])

  const loadMetrics = React.useCallback(async () => {
    try {
      setMetrics(await adminMetrics())
    } catch {
      // 401 由 overview 轮询统一处理跳登录
    }
  }, [])

  // reloadSettings：凭据保存等需要回读服务端状态时用（覆盖本地草稿）
  const reloadSettings = React.useCallback(async () => {
    setSettings(await adminGetSettings())
  }, [])

  React.useEffect(() => {
    adminSession()
      .then(async () => {
        setReady(true)
        await loadOverview()
        setSettings(await adminGetSettings())
      })
      .catch(() => navigate('/admin/login', { replace: true }))
  }, [loadOverview, navigate])

  // 自动保存：设置变更后防抖整体保存（app-screens §8.2「每个控件自存 + 安静戳」；
  // 凭据走各自的显式保存，绝不自动上送半输入的密钥）
  React.useEffect(() => {
    if (!saveVersion || !settingsRef.current) return
    setSaveStatus('saving')
    const t = window.setTimeout(async () => {
      const d = settingsRef.current
      if (!d) return
      try {
        await adminSaveSettings({
          crawl: d.crawl,
          cache: d.cache,
          security: d.security,
          server: d.server,
          network: d.network,
          sources: d.sources,
          bgm: {},
        })
        setSaveStatus('saved')
        setSavedAt(new Date().toLocaleTimeString('zh-CN', { hour12: false }))
        // 来源启停可能变化：丢 meta 缓存让 manifest 随下次拉取重放
        resetMetaCache()
      } catch {
        setSaveStatus('error')
      }
    }, AUTOSAVE_DEBOUNCE_MS)
    return () => window.clearTimeout(t)
  }, [saveVersion])

  // 轮询间隔是管理设置（admin_poll_sec，默认 4s）；页面隐藏时暂停（性能红线）
  const pollMs = (settings?.server?.admin_poll_sec ?? 4) * 1000

  React.useEffect(() => {
    if (!ready) return
    const t = window.setInterval(() => {
      if (!document.hidden) void loadOverview()
    }, pollMs)
    return () => window.clearInterval(t)
  }, [ready, loadOverview, pollMs])

  React.useEffect(() => {
    if (!ready || tab !== 'perf') return
    void loadMetrics()
    const t = window.setInterval(() => {
      if (!document.hidden) void loadMetrics()
    }, pollMs)
    return () => window.clearInterval(t)
  }, [ready, tab, loadMetrics, pollMs])

  async function cancelCrawl() {
    try {
      await adminCancelCrawl()
      setNotice('已请求停止任务')
      setError('')
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
    return <RouteLoading />
  }

  // 自动保存状态戳（安静提示：保存中/已保存/失败；失败才需要人管）
  const saveStamp =
    saveStatus === 'saving'
      ? '保存中…'
      : saveStatus === 'saved'
        ? `已保存 ${savedAt}`
        : saveStatus === 'error'
          ? '保存失败 · 修改任意设置后会自动重试'
          : ''

  return (
    <div>
      <PageHead
        title="控制台"
        description="抓取设置、索引数据与性能监测。"
        aside={
          <Button variant="outline" size="sm" onClick={() => void logout()} className="gap-1.5">
            <LogOut size={14} aria-hidden /> 退出登录
          </Button>
        }
      />

      {notice && (
        <Notice tone="success" className="mb-3">
          {notice}
        </Notice>
      )}
      {error && (
        <Notice tone="error" className="mb-3">
          {error}
        </Notice>
      )}

      {/* 自动保存状态戳：aria-live 让读屏器知道设置已落库 */}
      <div className="mb-1 text-right text-xs text-muted-foreground" aria-live="polite" role="status">
        {saveStatus === 'error' ? (
          <span className="text-destructive">{saveStamp}</span>
        ) : (
          saveStamp
        )}
      </div>

      <Tabs value={tab} onValueChange={setTab}>
        {/* 横向可滚（窄屏），纵向裁切——原生 Tabs 的 after 下划线位会向下溢出 5px，不裁会顶出竖向滚动条 */}
        <TabsList className="scrollbar-none w-full justify-start overflow-x-auto overflow-y-clip">
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="data">数据</TabsTrigger>
          <TabsTrigger value="crawl">抓取设置</TabsTrigger>
          <TabsTrigger value="perf">性能</TabsTrigger>
          <TabsTrigger value="settings">设置</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="mt-4">
          <OverviewPanel overview={overview} onCancelCrawl={() => void cancelCrawl()} />
        </TabsContent>

        <TabsContent value="crawl" className="mt-4">
          <CrawlPanel
            overview={overview}
            settings={settings}
            onChangeDraft={updateSettings}
            reloadSettings={reloadSettings}
            onRefresh={loadOverview}
            onNotice={msg => {
              setError('')
              setNotice(msg)
            }}
            onError={msg => {
              setNotice('')
              setError(msg)
            }}
          />
        </TabsContent>

        <TabsContent value="data" className="mt-4">
          <DataPanel onChange={() => void loadOverview()} />
        </TabsContent>

        <TabsContent value="settings" className="mt-4">
          {settings && (
            <SettingsPanel settings={settings} onChangeDraft={updateSettings} />
          )}
        </TabsContent>

        <TabsContent value="perf" className="mt-4">
          <PerfPanel
            metrics={metrics}
            onRefresh={() => void loadMetrics()}
            onReset={() => void adminMetricsReset().then(() => void loadMetrics())}
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}
