import * as React from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { LogOut } from 'lucide-react'
import {
  ApiError, adminCancelCrawl, adminGetSettings, adminLogout, adminMetrics, adminMetricsReset,
  adminOverview, adminSession,
} from '../api/client'
import type { AdminOverview, AdminSettings, MetricsSnapshot } from '../api/types'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { Notice, PageHead, RouteLoading } from '@/components/ui-kit'
import { OverviewPanel } from './admin/OverviewPanel'
import { CrawlPanel } from './admin/CrawlPanel'
import { DataPanel } from './admin/DataPanel'
import { SettingsPanel } from './admin/SettingsPanel'
import { PerfPanel } from './admin/PerfPanel'

const TABS = ['overview', 'crawl', 'data', 'settings', 'perf'] as const
type Tab = (typeof TABS)[number]

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
  const [settings, setSettings] = React.useState<AdminSettings | null>(null)
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
      if (!document.hidden) void loadOverview()
    }, 4000)
    return () => window.clearInterval(t)
  }, [ready, loadOverview])

  React.useEffect(() => {
    if (!ready || tab !== 'perf') return
    void loadMetrics()
    const t = window.setInterval(() => {
      if (!document.hidden) void loadMetrics()
    }, 4000)
    return () => window.clearInterval(t)
  }, [ready, tab, loadMetrics])

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

  return (
    <div>
      <PageHead
        title="控制台"
        description="抓取调度、索引数据、抓取凭据与性能监测。"
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

      <Tabs value={tab} onValueChange={setTab}>
        {/* 横向可滚（窄屏），纵向裁切——原生 Tabs 的 after 下划线位会向下溢出 5px，不裁会顶出竖向滚动条 */}
        <TabsList className="scrollbar-none w-full justify-start overflow-x-auto overflow-y-clip">
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="crawl">抓取</TabsTrigger>
          <TabsTrigger value="data">数据</TabsTrigger>
          <TabsTrigger value="settings">设置</TabsTrigger>
          <TabsTrigger value="perf">性能</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="mt-4">
          <OverviewPanel overview={overview} onCancelCrawl={() => void cancelCrawl()} />
        </TabsContent>

        <TabsContent value="crawl" className="mt-4">
          <CrawlPanel
            overview={overview}
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
            <SettingsPanel
              settings={settings}
              onSaved={async msg => {
                setError('')
                setNotice(msg)
                setSettings(await adminGetSettings())
                await loadOverview()
              }}
              onError={msg => {
                setNotice('')
                setError(msg)
              }}
            />
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
