import * as React from 'react'
import { Eye, EyeOff, Loader2, LogOut, Plus, Trash2 } from 'lucide-react'
import { UserAvatar } from './user-avatar'
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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

function ChannelPanel() {
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
    <section className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-medium">数据通道</h3>
        <Select value={active} onValueChange={change}>
          <SelectTrigger className="h-8 w-[190px]" aria-label="数据通道">
            <SelectValue />
          </SelectTrigger>
          <SelectContent align="end">
            <SelectItem value="auto">自动选择</SelectItem>
            {OFFICIAL_ENDPOINTS.map(e => (
              <SelectItem key={e.id} value={e.id}>
                {e.label}
              </SelectItem>
            ))}
            {BUILTIN_MIRRORS.map(e => (
              <SelectItem key={e.id} value={e.id}>
                {e.label}（内置镜像）
              </SelectItem>
            ))}
            {endpoints
              .filter(e => !e.builtin)
              .map(e => (
                <SelectItem key={e.id} value={e.id}>
                  {e.label}（自定义镜像）
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
      </div>

      <p className="text-xs leading-relaxed text-muted-foreground">
        自动：有凭据时优先官方，官方不可达时改用镜像的公开数据；无凭据时优先镜像。令牌只发往官方域名。
        官方 API 只有 <code className="rounded bg-muted px-1 py-0.5 text-[11px]">api.bgm.tv</code>。
      </p>

      {activeIsMirror && (
        <p className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs">
          当前为镜像通道：不会发送登录凭据，敏感内容可能不可见。
        </p>
      )}

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label className="text-xs text-muted-foreground">自定义镜像</Label>
          <span className="text-[11px] text-muted-foreground">只保存在本机</span>
        </div>
        <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
          <Input
            value={api}
            onChange={e => setApi(e.target.value)}
            placeholder="API 地址 https://…"
            className="h-8 font-mono text-xs"
            spellCheck={false}
          />
          <div className="flex gap-2">
            <Input
              value={image}
              onChange={e => setImage(e.target.value)}
              placeholder="图片镜像（可选）"
              className="h-8 min-w-0 flex-1 font-mono text-xs sm:w-40"
              spellCheck={false}
            />
            <Button type="button" size="sm" variant="outline" onClick={add} disabled={!api.trim()} className="gap-1">
              <Plus size={14} aria-hidden />
              添加
            </Button>
          </div>
        </div>
        {err && <p className="text-xs text-destructive">{err}</p>}

        {user.length > 0 && (
          <ul className="divide-y rounded-md border">
            {user.map(m => (
              <li key={m.apiBase} className="flex items-center gap-2 px-3 py-2">
                <span className="min-w-0 truncate font-mono text-xs">{m.apiBase}</span>
                <Badge variant="outline" className="shrink-0 text-[10px]">
                  自定义
                </Badge>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`删除 ${m.apiBase}`}
                  className="ml-auto shrink-0"
                  onClick={() => {
                    const wasActive = getActiveId() === `user:${m.apiBase}`
                    removeUserMirror(m.apiBase)
                    forceRender(v => v + 1)
                    if (wasActive) window.location.reload()
                  }}
                >
                  <Trash2 size={14} aria-hidden />
                </Button>
              </li>
            ))}
          </ul>
        )}
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          镜像由第三方运营，能看到你的访问记录，且只提供公开数据；不要填写来路不明的地址。内置镜像：
          {BUILTIN_MIRRORS.map(m => new URL(m.apiBase).host).join('、')}。
        </p>
      </div>
    </section>
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
      saveCredential(trimmed, {
        id: me.id,
        username: me.username,
        nickname: me.nickname,
        avatar: me.avatar?.large || me.avatar?.medium || me.avatar?.small,
      })
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
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-[620px]">
        <DialogHeader>
          <DialogTitle>登录与数据通道</DialogTitle>
          <DialogDescription>
            使用你自己的 Bangumi 凭据；凭据只保存到本机 localStorage，本站服务端没有接收用户凭据的接口。
          </DialogDescription>
        </DialogHeader>

        <section className="space-y-3">
          <h3 className="text-sm font-medium">账号</h3>

          {credential ? (
            <div className="rounded-lg border p-3">
              <div className="flex items-center gap-3">
                <UserAvatar
                  src={credential.user?.avatar}
                  alt={credential.user?.nickname || credential.user?.username || '已登录'}
                  className="size-9"
                />
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">
                    {credential.user?.nickname || credential.user?.username || '已保存的凭据'}
                  </p>
                  <p className="num mt-0.5 text-xs text-muted-foreground">
                    {credential.user?.id ? `UID ${credential.user.id} · ` : ''}
                    保存于 {fmtDateTime(new Date(credential.savedAt).toISOString())}
                  </p>
                </div>
              </div>
              <Button
                variant="outline"
                size="sm"
                className="mt-3 w-full gap-1.5"
                onClick={() => {
                  clearCredential()
                  onLoggedIn?.()
                }}
              >
                <LogOut size={14} aria-hidden /> 清除本机凭据并退出
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
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label={show ? '隐藏令牌' : '显示令牌'}
                    onClick={() => setShow(s => !s)}
                    className="absolute right-1 top-1/2 -translate-y-1/2"
                  >
                    {show ? <EyeOff size={14} aria-hidden /> : <Eye size={14} aria-hidden />}
                  </Button>
                </div>
                <p className="text-xs leading-relaxed text-muted-foreground">
                  在 Bangumi 的{' '}
                  <a
                    href="https://next.bgm.tv/demo/access-token"
                    target="_blank"
                    rel="noreferrer"
                    className="underline underline-offset-4 hover:text-foreground"
                  >
                    个人令牌页面
                  </a>{' '}
                  生成后复制到此处。令牌等价于账号权限，请勿分享。
                </p>
              </div>

              {error && (
                <div className="space-y-1">
                  <p className="text-xs text-destructive">{error}</p>
                  {error.includes('无法连接') || error.includes('不可达') || error.includes('无法校验') ? (
                    <Button
                      type="button"
                      variant="link"
                      size="xs"
                      className="h-auto px-0 text-xs"
                      onClick={saveUnverified}
                    >
                      官方域名不可达时，仅保存到本机（不校验）
                    </Button>
                  ) : null}
                </div>
              )}

              <Button className="w-full gap-1.5" disabled={busy} onClick={() => void submitToken()}>
                {busy && <Loader2 size={14} className="animate-spin" aria-hidden />}
                验证并保存到本机
              </Button>
            </div>
          )}
        </section>

        <div className="border-t pt-4">
          <ChannelPanel />
        </div>

        <p className="rounded-md bg-muted px-3 py-2 text-[11px] leading-relaxed text-muted-foreground">
          官方请求可携带令牌，镜像请求永不携带；本站不接收、不转发、不记录用户令牌。公共电脑请用完退出。
        </p>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
