import * as React from 'react'
import { Activity, Loader2, RefreshCw, Save } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Panel } from '@/components/panel'
import {
  adminChangePassword, adminNetwork, adminSaveSettings, adminTestCredential,
} from '../../api/client'
import type { AdminSettings, CacheSettings, CrawlSettings, NetworkStatus, SecuritySettings } from '../../api/types'
import { TYPE_LABEL, TYPE_ORDER } from '../../api/types'
import { NumField, Toggle } from './parts'

export function SettingsPanel({
  settings,
  onSaved,
  onError,
}: {
  settings: AdminSettings
  onSaved: (msg: string) => Promise<void>
  onError: (msg: string) => void
}) {
  const [crawl, setCrawl] = React.useState<CrawlSettings>(settings.crawl)
  const [cache, setCache] = React.useState<CacheSettings>(settings.cache)
  const [security, setSecurity] = React.useState<SecuritySettings>(settings.security)
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
    setCache(settings.cache)
    setSecurity(settings.security)
  }, [settings])

  function set<K extends keyof CrawlSettings>(key: K, value: CrawlSettings[K]) {
    setCrawl(c => ({ ...c, [key]: value }))
  }

  function setCacheField<K extends keyof CacheSettings>(key: K, value: CacheSettings[K]) {
    setCache(c => ({ ...c, [key]: value }))
  }

  function setSecField<K extends keyof SecuritySettings>(key: K, value: SecuritySettings[K]) {
    setSecurity(s => ({ ...s, [key]: value }))
  }

  async function save() {
    setBusy(true)
    try {
      await adminSaveSettings({
        crawl,
        cache,
        security,
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
      <Panel title="抓取设置" contentClassName="space-y-4">
          <div className="space-y-2">
            <p className="text-xs text-muted-foreground">抓取类型</p>
            <div className="flex flex-wrap gap-x-5 gap-y-2">
              {TYPE_ORDER.map(t => (
                <label key={t} className="flex items-center gap-1.5 text-sm">
                  <Checkbox
                    checked={crawl.types.includes(t)}
                    onCheckedChange={() =>
                      set(
                        'types',
                        crawl.types.includes(t)
                          ? crawl.types.filter(x => x !== t)
                          : [...crawl.types, t],
                      )
                    }
                  />
                  {TYPE_LABEL[t]}
                </label>
              ))}
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <NumField label="每页条数" value={crawl.page_size} onChange={v => set('page_size', v)} hint="10–100" />
            <NumField label="请求间隔 (ms)" value={crawl.delay_ms} onChange={v => set('delay_ms', v)} hint="越大越礼貌" />
            <NumField label="请求超时 (s)" value={crawl.request_timeout_sec} onChange={v => set('request_timeout_sec', v)} />
            <NumField label="重爬 TTL (天)" value={crawl.refresh_ttl_days} onChange={v => set('refresh_ttl_days', v)} />
            <NumField label="重爬批量" value={crawl.refresh_batch} onChange={v => set('refresh_batch', v)} hint="0 = 不自动重爬" />
            <NumField label="增量页数 / 类型" value={crawl.incremental_pages} onChange={v => set('incremental_pages', v)} />
            <NumField label="增量间隔 (分钟)" value={crawl.incremental_interval_min} onChange={v => set('incremental_interval_min', v)} />
          </div>

          <div className="flex flex-wrap gap-x-6 gap-y-2 border-t pt-3">
            <Toggle label="自动增量" checked={crawl.auto_incremental} onChange={v => set('auto_incremental', v)} />
            <Toggle label="自动重爬过期" checked={crawl.auto_refresh} onChange={v => set('auto_refresh', v)} />
            <Toggle label="全量后清理旧数据" checked={crawl.prune_on_full} onChange={v => set('prune_on_full', v)} />
          </div>
      </Panel>

      <Panel title="查询缓存" contentClassName="space-y-3">
          <p className="text-xs leading-relaxed text-muted-foreground">
            搜索页 / 搜索总数 / 标签候选 / 条目详情共用进程内结果缓存，抓取收尾与管理端删改条目时会主动失效，
            这里的 TTL 只是兜底，不设上限。填 0 = 关闭该层缓存（每次都实查，排查问题时用）。
            注意「浏览器缓存」是浏览器与 CF 边缘按 max-age 自然过期，不会随抓取主动失效——设得很大意味着访问者会长期看到旧列表。
          </p>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <NumField
              label="查询结果缓存 (s)"
              value={cache.query_ttl}
              onChange={v => setCacheField('query_ttl', v)}
              hint="0 = 关闭；默认 60"
            />
            <NumField
              label="浏览器缓存 max-age (s)"
              value={cache.public_ttl}
              onChange={v => setCacheField('public_ttl', v)}
              hint="0 = 关闭（no-store）；默认 30"
            />
            <NumField
              label="首页元信息缓存 (s)"
              value={cache.meta_ttl}
              onChange={v => setCacheField('meta_ttl', v)}
              hint="0 = 关闭；默认 30"
            />
            <NumField
              label="元标签候选缓存 (s)"
              value={cache.meta_tags_ttl}
              onChange={v => setCacheField('meta_tags_ttl', v)}
              hint="0 = 关闭；默认 600"
            />
            <NumField
              label="后台总览缓存 (s)"
              value={cache.overview_ttl}
              onChange={v => setCacheField('overview_ttl', v)}
              hint="0 = 关闭；默认 3；后台 4s 轮询"
            />
            <NumField
              label="缓存键上限"
              value={cache.max_keys}
              onChange={v => setCacheField('max_keys', v)}
              hint="16–10000，默认 256；搜索/标签/详情/总数共用"
            />
          </div>
      </Panel>

      <Panel title="限流与安全" contentClassName="space-y-3">
          <p className="text-xs leading-relaxed text-muted-foreground">
            限流按 IP 计数（经 Cloudflare/可信反代时取真实 IP 头），令牌桶算法：超出突发容量后按速率匀速补充。
            调低防爬防刷，调高应对大流量；0 = 恢复默认值。改动保存后立即生效，无需重启。
          </p>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <NumField
              label="请求速率 (次/秒/IP)"
              value={security.rate_limit_rps}
              onChange={v => setSecField('rate_limit_rps', v)}
              hint="1–1000，默认 30"
            />
            <NumField
              label="请求突发容量"
              value={security.rate_limit_burst}
              onChange={v => setSecField('rate_limit_burst', v)}
              hint="10–10000，默认 120"
            />
            <NumField
              label="登录速率 (次/分/IP)"
              value={security.login_rpm}
              onChange={v => setSecField('login_rpm', v)}
              hint="1–60，默认 5；防密码爆破"
            />
            <NumField
              label="登录突发容量"
              value={security.login_burst}
              onChange={v => setSecField('login_burst', v)}
              hint="1–100，默认 6"
            />
            <NumField
              label="错误密码延迟 (ms)"
              value={security.login_delay_ms}
              onChange={v => setSecField('login_delay_ms', v)}
              hint="0–2000，默认 300；拉高爆破成本"
            />
            <NumField
              label="会话有效期 (天)"
              value={security.session_ttl_days}
              onChange={v => setSecField('session_ttl_days', v)}
              hint="1–90，默认 7；对新会话生效"
            />
            <NumField
              label="限流表 IP 上限"
              value={security.limiter_max_ips}
              onChange={v => setSecField('limiter_max_ips', v)}
              hint="256–65536，默认 4096；内存有界"
            />
            <NumField
              label="公开搜索最大页码"
              value={security.max_page}
              onChange={v => setSecField('max_page', v)}
              hint="100–100000，默认 5000；深分页成本上限"
            />
          </div>
      </Panel>

      <div className="grid gap-4 xl:grid-cols-2">
        <Panel title="站长 Bangumi 凭据" contentClassName="space-y-3">
            <p className="text-xs leading-relaxed text-muted-foreground">
              仅用于后台抓取。配置后可以抓取需要登录的条目；凭据保存在数据库（配置
              <code className="mx-1 rounded bg-muted px-1 py-0.5">ANIPICK_SECRET</code> 时加密）。
            </p>

            <div className="space-y-1.5">
              <Label htmlFor="admin-token" className="text-xs">
                Access Token{' '}
                <span className="text-muted-foreground">
                  {settings.bgm.token_configured ? `（已配置 ${settings.bgm.token}）` : '（未配置）'}
                </span>
              </Label>
              <Input
                id="admin-token"
                value={token}
                onChange={e => setToken(e.target.value)}
                placeholder={settings.bgm.token_configured ? '留空表示不修改' : '粘贴 Access Token'}
                className="h-9 font-mono text-xs"
              />
              {settings.bgm.token_configured && (
                <Toggle label="清除 Token" checked={clearToken} onChange={setClearToken} />
              )}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="admin-cookie" className="text-xs">
                Cookie{' '}
                <span className="text-muted-foreground">
                  {settings.bgm.cookie_configured ? `（已配置 ${settings.bgm.cookie}）` : '（未配置）'}
                </span>
              </Label>
              <Input
                id="admin-cookie"
                value={cookie}
                onChange={e => setCookie(e.target.value)}
                placeholder={settings.bgm.cookie_configured ? '留空表示不修改' : 'chii_sid=…; chii_auth=…'}
                className="h-9 font-mono text-xs"
              />
              {settings.bgm.cookie_configured && (
                <Toggle label="清除 Cookie" checked={clearCookie} onChange={setClearCookie} />
              )}
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => void test()} className="gap-1.5">
                <Activity size={14} aria-hidden /> 校验凭据
              </Button>
              {testMsg && (
                <span className="max-w-full break-all text-[11px] text-muted-foreground">{testMsg}</span>
              )}
            </div>
        </Panel>

        <Panel
          title="抓取出口"
          action={
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={netBusy}
              onClick={() => void loadNet()}
              className="gap-1.5 text-muted-foreground"
            >
              {netBusy ? (
                <Loader2 size={13} className="animate-spin" aria-hidden />
              ) : (
                <RefreshCw size={13} aria-hidden />
              )}
              重新检测
            </Button>
          }
          contentClassName="space-y-3"
        >
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={net?.proxy ? 'default' : 'secondary'} className="font-normal">
                出口 {net?.proxy ? '代理' : net?.doh ? 'DoH 直连' : '未配置'}
              </Badge>
              {!net?.proxy && (
                <Badge variant={net?.doh ? 'default' : 'secondary'} className="font-normal">
                  DoH {net?.doh ? '已启用' : '未启用'}
                </Badge>
              )}
              <Badge variant={net?.upstream_ok ? 'secondary' : 'destructive'} className="font-normal">
                上游 {net?.upstream_ok ? '可达' : '不可达'}
              </Badge>
            </div>
            <dl className="space-y-1.5 text-xs leading-relaxed text-muted-foreground">
              <div className="flex gap-2">
                <dt className="shrink-0">解析地址</dt>
                <dd className="break-all font-mono text-[11px]">
                  {net?.diagnostics?.addresses?.join('，') || '—'}
                </dd>
              </div>
              <div className="flex gap-2">
                <dt className="shrink-0">DoH 入口</dt>
                <dd className="break-all font-mono text-[11px]">
                  {net?.diagnostics?.endpoints?.join('，') || '—'}
                </dd>
              </div>
              {net?.upstream_error && (
                <div className="flex gap-2 text-destructive">
                  <dt className="shrink-0">上游错误</dt>
                  <dd className="break-all">{net.upstream_error}</dd>
                </div>
              )}
              {net?.diagnostics?.error && (
                <div className="flex gap-2 text-destructive">
                  <dt className="shrink-0">诊断失败</dt>
                  <dd className="break-all">{net.diagnostics.error}</dd>
                </div>
              )}
            </dl>
            <p className="text-[11px] leading-relaxed text-muted-foreground">
              该通道只用于服务端抓取，不中转访客请求。可用
              <code className="mx-1 rounded bg-muted px-1 py-0.5">ANIPICK_EGRESS_PROXY</code>
              配置抓取代理；否则走 DoH 解析。
            </p>
        </Panel>
      </div>

      <Panel title="管理员密码" contentClassName="flex flex-wrap items-end gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="admin-current-pw" className="text-xs text-muted-foreground">
              当前密码
            </Label>
            <Input
              id="admin-current-pw"
              type="password"
              value={currentPw}
              onChange={e => setCurrentPw(e.target.value)}
              className="h-9 w-48"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="admin-next-pw" className="text-xs text-muted-foreground">
              新密码（至少 6 位）
            </Label>
            <Input
              id="admin-next-pw"
              type="password"
              value={nextPw}
              onChange={e => setNextPw(e.target.value)}
              className="h-9 w-48"
            />
          </div>
          <Button
            size="sm"
            variant="outline"
            disabled={!currentPw || nextPw.length < 6}
            onClick={() => void changePassword()}
          >
            修改密码
          </Button>
          {pwMsg && <span className="text-xs text-muted-foreground">{pwMsg}</span>}
      </Panel>

      <div className="flex flex-wrap items-center justify-end gap-3">
        <span className="mr-auto text-[11px] text-muted-foreground">
          更新于 {settings.updated_at ? settings.updated_at.slice(0, 16).replace('T', ' ') : '—'}
        </span>
        <Button onClick={() => void save()} disabled={busy} className="gap-1.5">
          {busy ? <Loader2 size={14} className="animate-spin" aria-hidden /> : <Save size={14} aria-hidden />}
          保存设置
        </Button>
      </div>
    </div>
  )
}
