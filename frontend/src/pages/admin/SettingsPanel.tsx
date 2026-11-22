import * as React from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Panel } from '@/components/panel'
import { adminChangePassword } from '../../api/client'
import type { AdminSettings, CacheSettings, SecuritySettings, ServerSettings,
} from '../../api/types'
import { NumField } from './parts'

// 全局运行参数：查询缓存、限流与安全、服务器与面板、管理员密码。
// 本页改动自动保存（AdminPage 统一防抖）；密码是凭据动作走显式表单。
// 来源相关配置（启用/顺序/评分权重/凭据/抓取参数/出站网络）在「抓取设置」tab。
export function SettingsPanel({
  settings,
  onChangeDraft,
}: {
  settings: AdminSettings
  /** 修改设置草稿（AdminPage 持有，自动保存） */
  onChangeDraft: (updater: (d: AdminSettings) => AdminSettings) => void
}) {
  const cache = settings.cache
  const security = settings.security
  const server = settings.server

  const [currentPw, setCurrentPw] = React.useState('')
  const [nextPw, setNextPw] = React.useState('')
  const [pwMsg, setPwMsg] = React.useState('')

  // 本面板没有保存按钮：修改即自动保存（AdminPage 统一防抖上送）。
  // 管理员密码是凭据动作，保持显式表单，不参与自动保存。
  function setCacheField<K extends keyof CacheSettings>(key: K, value: CacheSettings[K]) {
    onChangeDraft(d => ({ ...d, cache: { ...d.cache, [key]: value } }))
  }

  function setSecField<K extends keyof SecuritySettings>(key: K, value: SecuritySettings[K]) {
    onChangeDraft(d => ({ ...d, security: { ...d.security, [key]: value } }))
  }

  function setSrvField<K extends keyof ServerSettings>(key: K, value: ServerSettings[K]) {
    onChangeDraft(d => ({ ...d, server: { ...d.server, [key]: value } }))
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
          {/* 真实 IP 识别（原环境变量迁入）：保存时后端校验 CIDR 合法性，非法拒绝 */}
          <div className="grid gap-3 border-t pt-3 sm:grid-cols-2">
            <div className="space-y-1">
              <Label htmlFor="sec-ip-header" className="text-xs text-muted-foreground">
                客户端真实 IP 请求头
                <span className="text-muted-foreground">（选填）</span>
              </Label>
              <Input
                id="sec-ip-header"
                value={security.client_ip_header}
                onChange={e => setSecField('client_ip_header', e.target.value)}
                placeholder="如 CF-Connecting-IP；留空 = 用 TCP 对端"
                className="h-9 font-mono text-xs"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="sec-trusted-cidrs" className="text-xs text-muted-foreground">
                可信反代网段
                <span className="text-muted-foreground">（逗号分隔 CIDR；选填）</span>
              </Label>
              <Input
                id="sec-trusted-cidrs"
                value={security.trusted_proxy_cidrs}
                onChange={e => setSecField('trusted_proxy_cidrs', e.target.value)}
                placeholder="173.245.48.0/20,103.21.244.0/22"
                className="h-9 font-mono text-xs"
              />
              <p className="text-[11px] leading-relaxed text-muted-foreground">
                配了头就必须配网段：只有对端在网段内才采信头，防伪造绕过限流。Cloudflare 网段见 cloudflare.com/ips。
              </p>
            </div>
          </div>
      </Panel>

      {/* 服务器与面板：原为代码常量的运行参数；默认即推荐值，改动主要影响管理面与进程行为 */}
      <Panel title="服务器与面板" contentClassName="space-y-3">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <NumField label="会话校验缓存 (s)" value={server.session_cache_ttl_sec} onChange={n => setSrvField('session_cache_ttl_sec', n)} hint="默认 60 · 范围 5–600；命中时零数据库开销" />
          <NumField label="会话落库节流 (s)" value={server.session_seen_gap_sec} onChange={n => setSrvField('session_seen_gap_sec', n)} hint="默认 600 · last_seen 至多隔这么久写一次库" />
          <NumField label="会话缓存上限 (条)" value={server.session_cache_max} onChange={n => setSrvField('session_cache_max', n)} hint="默认 512 · 内存有界" />
          <NumField label="压缩阈值 (字节)" value={server.min_compress_bytes} onChange={n => setSrvField('min_compress_bytes', n)} hint="默认 860 · 更小的响应压缩反而变本加厉，不压" />
          <NumField label="设置快照刷新 (s)" value={server.snapshot_ttl_sec} onChange={n => setSrvField('snapshot_ttl_sec', n)} hint="默认 60 · 兜底刷新；保存后立即生效不走此周期" />
          <NumField label="每路由样本上限" value={server.perf_sample_cap} onChange={n => setSrvField('perf_sample_cap', n)} hint="默认 256 · 范围 32–4096；算 p95 用" />
          <NumField label="近期请求上限" value={server.recent_req_cap} onChange={n => setSrvField('recent_req_cap', n)} hint="默认 256 · 范围 32–4096；性能页环形缓冲" />
          <NumField label="总览任务条数" value={server.overview_runs} onChange={n => setSrvField('overview_runs', n)} hint="默认 24 · 范围 5–100；概览页最近任务显示数" />
          <NumField label="优雅停机等待 (s)" value={server.shutdown_wait_sec} onChange={n => setSrvField('shutdown_wait_sec', n)} hint="默认 10 · 范围 1–120；关闭进程时等在途请求收尾" />
          <NumField label="面板轮询间隔 (s)" value={server.admin_poll_sec} onChange={n => setSrvField('admin_poll_sec', n)} hint="默认 4 · 范围 2–60；页面隐藏时自动暂停" />
          <NumField label="返回缓存页数" value={server.page_cache_max} onChange={n => setSrvField('page_cache_max', n)} hint="默认 12 · 0 = 关闭返回不还原滚动" />
          <NumField label="元标签缓存键上限" value={cache.meta_tags_max_keys} onChange={n => setCacheField('meta_tags_max_keys', n)} hint="默认 64 · 内存有界" />
        </div>
      </Panel>
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

      <p className="text-right text-[11px] text-muted-foreground">
        本页修改即自动保存（右上角有保存状态）；管理员密码单独提交。
      </p>
    </div>
  )
}
