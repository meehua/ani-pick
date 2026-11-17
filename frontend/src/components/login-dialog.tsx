import * as React from 'react'
import { ExternalLink, Eye, EyeOff, Loader2, LogOut, Plus, Server, ShieldCheck, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import { bgmMe, resetBgmCircuit } from '../api/client'
import { clearCredential, saveCredential, useCredential } from '../lib/credential'
import { fmtDateTime } from '../lib/format'
import {
  BUILTIN_MIRRORS, OFFICIAL_ENDPOINTS, addUserMirror, allEndpoints, getActiveId, removeUserMirror,
  setActiveId, userMirrors,
} from '../net/endpoints'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onLoggedIn?: () => void
}

function EndpointSettings() {
  const [, forceRender] = React.useState(0)
  const [api, setApi] = React.useState('')
  const [image, setImage] = React.useState('')
  const [err, setErr] = React.useState('')
  const active = getActiveId()
  const endpoints = allEndpoints()
  const user = userMirrors()

  const activeEndpoint = endpoints.find(e => e.id === active)
  const activeIsMirror = activeEndpoint?.kind === 'mirror'

  function change(id: string) {
    setActiveId(id)
    window.location.reload()
  }

  function add() {
    const message = addUserMirror(api, image)
    if (message) {
      setErr(message)
      return
    }
    setErr('')
    setApi('')
    setImage('')
    forceRender(v => v + 1)
  }

  return (
    <div className="rounded-lg border p-3">
      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-1.5 text-xs font-medium">
          <Server size={13} /> 数据通道
        </span>
        <Select value={active} onValueChange={change}>
          <SelectTrigger className="h-7 w-48 text-[11px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="auto">自动选择</SelectItem>
            {OFFICIAL_ENDPOINTS.map(e => (
              <SelectItem key={e.id} value={e.id}>{e.label}</SelectItem>
            ))}
            {BUILTIN_MIRRORS.map(e => (
              <SelectItem key={e.id} value={e.id}>{e.label}（内置镜像）</SelectItem>
            ))}
            {endpoints.filter(e => !e.builtin).map(e => (
              <SelectItem key={e.id} value={e.id}>{e.label}（自定义镜像）</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <p className="mt-2 text-[11px] leading-relaxed text-muted-foreground">
        自动：有登录凭据时优先官方（<b className="text-foreground">令牌只发官方</b>），官方不可达时使用镜像公开数据；
        无凭据时优先镜像。
      </p>
      <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
        官方 API 目前只有 <code>api.bgm.tv</code>；<code>bgm.tv</code> / <code>bangumi.tv</code> / <code>chii.in</code>
        是官方站点域名，不提供 <code>/v0</code> 接口。
      </p>
      <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
        自动模式在官方不可达时会使用内置镜像；如不希望使用第三方镜像，请选择「官方 api.bgm.tv」。
      </p>
      {activeIsMirror && (
        <p className="mt-1 text-[11px] leading-relaxed text-amber-600">
          当前为镜像通道：不会发送登录凭据，敏感内容可能不可见。
        </p>
      )}

      <div className="mt-3 space-y-1.5">
        <Label className="text-[11px] text-muted-foreground">添加镜像站（只保存在本机）</Label>
        <div className="grid gap-1.5 sm:grid-cols-[1fr_1fr_auto]">
          <Input
            value={api}
            onChange={e => setApi(e.target.value)}
            placeholder="API 地址 https://…"
            className="h-8 font-mono text-[11px]"
            spellCheck={false}
          />
          <Input
            value={image}
            onChange={e => setImage(e.target.value)}
            placeholder="图片镜像（可选）"
            className="h-8 font-mono text-[11px]"
            spellCheck={false}
          />
          <Button size="sm" variant="outline" onClick={add} disabled={!api.trim()} className="h-8 gap-1">
            <Plus size={12} /> 添加
          </Button>
        </div>
        {err && <p className="text-[11px] text-destructive">{err}</p>}
        {user.length > 0 && (
          <ul className="space-y-1 pt-1">
            {user.map(m => (
              <li key={m.apiBase} className="flex items-center gap-2 text-[11px] text-muted-foreground">
                <span className="truncate font-mono">{m.apiBase}</span>
                <button
                  type="button"
                  aria-label={`删除 ${m.apiBase}`}
                  className="ml-auto shrink-0 text-muted-foreground hover:text-destructive"
                  onClick={() => {
                    const wasActive = getActiveId() === `user:${m.apiBase}`
                    removeUserMirror(m.apiBase)
                    forceRender(v => v + 1)
                    if (wasActive) window.location.reload()
                  }}
                >
                  <Trash2 size={12} />
                </button>
              </li>
            ))}
          </ul>
        )}
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          镜像站由第三方运营，运营方能看到你的访问记录；只提供公开数据。不要填写来路不明的地址。
          内置镜像：{BUILTIN_MIRRORS.map(m => new URL(m.apiBase).host).join('、')}。
          自定义镜像未填图片地址时，封面仍走官方图床。
        </p>
      </div>
    </div>
  )
}

export function LoginDialog({ open, onOpenChange, onLoggedIn }: Props) {
  const credential = useCredential()
  const [token, setToken] = React.useState('')
  const [show, setShow] = React.useState(false)
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState('')

  React.useEffect(() => {
    if (!open) return
    setError('')
  }, [open])

  async function submitToken() {
    const trimmed = token.trim()
    if (!trimmed) {
      setError('请粘贴 Access Token')
      return
    }
    setBusy(true)
    setError('')
    try {
      const me = await bgmMe(trimmed)
      resetBgmCircuit()
      saveCredential(trimmed, { id: me.id, username: me.username, nickname: me.nickname })
      setToken('')
      onLoggedIn?.()
      onOpenChange(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : '校验失败')
    } finally {
      setBusy(false)
    }
  }

  function saveUnverified() {
    const trimmed = token.trim()
    if (!trimmed) {
      setError('请粘贴 Access Token')
      return
    }
    resetBgmCircuit()
    saveCredential(trimmed)
    setToken('')
    onLoggedIn?.()
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>登录 Bangumi</DialogTitle>
          <DialogDescription>
            使用你自己的 Bangumi 凭据。凭据只保存在本机浏览器，本站服务端没有接收用户凭据的接口。
          </DialogDescription>
        </DialogHeader>

        {credential ? (
          <div className="space-y-4">
            <div className="flex items-center gap-3 rounded-lg border p-3">
              <div className="flex size-9 items-center justify-center rounded-full bg-muted text-xs font-semibold">
                {(credential.user?.nickname || credential.user?.username || 'B').slice(0, 1)}
              </div>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">
                  {credential.user?.nickname || credential.user?.username || '已保存的凭据'}
                </p>
                <p className="text-[11px] text-muted-foreground">
                  {credential.user?.id ? `UID ${credential.user.id} · ` : ''}
                  保存于 {fmtDateTime(new Date(credential.savedAt).toISOString())}
                </p>
              </div>
            </div>
            <Button
              variant="outline"
              className="w-full"
              onClick={() => {
                clearCredential()
                onLoggedIn?.()
              }}
            >
              <LogOut size={14} /> 清除本机凭据并退出
            </Button>
          </div>
        ) : (
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="bgm-token">个人 Access Token</Label>
              <div className="relative">
                <Input
                  id="bgm-token"
                  type={show ? 'text' : 'password'}
                  value={token}
                  onChange={e => setToken(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter') void submitToken()
                  }}
                  placeholder="粘贴个人令牌"
                  autoComplete="off"
                  spellCheck={false}
                  className="pr-9 font-mono text-xs"
                />
                <button
                  type="button"
                  aria-label={show ? '隐藏令牌' : '显示令牌'}
                  onClick={() => setShow(s => !s)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                >
                  {show ? <EyeOff size={14} /> : <Eye size={14} />}
                </button>
              </div>
              <p className="text-[11px] leading-relaxed text-muted-foreground">
                在 Bangumi 的{' '}
                <a
                  href="https://next.bgm.tv/demo/access-token"
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-0.5 text-primary underline-offset-4 hover:underline"
                >
                  个人令牌页面 <ExternalLink size={10} />
                </a>{' '}
                生成后复制到此处。令牌等价于账号权限，请勿分享给他人。
              </p>
            </div>
            {error && (
              <div className="space-y-1">
                <p className="text-xs text-destructive">{error}</p>
                {error.includes('无法连接') || error.includes('不可达') || error.includes('无法校验') ? (
                  <button type="button" onClick={saveUnverified} className="text-[11px] text-muted-foreground underline">
                    官方域名不可达时，仅保存到本机（不校验）
                  </button>
                ) : null}
              </div>
            )}
            <Button className="w-full" disabled={busy} onClick={() => void submitToken()}>
              {busy ? <Loader2 size={14} className="animate-spin" /> : null}
              验证并保存到本机
            </Button>
          </div>
        )}

        <EndpointSettings />

        <div className="flex items-start gap-2 rounded-lg border border-dashed p-3 text-[11px] leading-relaxed text-muted-foreground">
          <ShieldCheck size={14} className="mt-0.5 shrink-0 text-emerald-600" />
          <div>
            <b className="text-foreground">凭据只在本机：</b>
            写入浏览器 localStorage；官方请求可携带令牌，<b className="text-foreground">镜像请求永不携带令牌</b>，
            本站服务端不接收、不转发、不记录用户令牌。公共电脑上请在使用后点击「退出」。
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
