import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import { Loader2, Shield } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { adminLogin, adminSession } from '../api/client'

export default function AdminLoginPage() {
  const navigate = useNavigate()
  const [password, setPassword] = React.useState('')
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState('')

  React.useEffect(() => {
    adminSession().then(() => navigate('/admin', { replace: true })).catch(() => undefined)
  }, [navigate])

  return (
    <div className="mx-auto flex max-w-sm flex-col items-center py-16">
      <div className="mb-4 flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
        <Shield size={18} />
      </div>
      <h1 className="text-lg font-semibold">管理员登录</h1>
      <p className="mt-1 text-xs text-muted-foreground">
        管理索引抓取、访问凭据与站点设置。
      </p>

      <form
        className="mt-6 w-full space-y-3"
        onSubmit={async e => {
          e.preventDefault()
          setBusy(true)
          setError('')
          try {
            await adminLogin(password)
            navigate('/admin', { replace: true })
          } catch (err) {
            setError(err instanceof Error ? err.message : '登录失败')
          } finally {
            setBusy(false)
          }
        }}
      >
        <div className="space-y-1.5">
          <Label htmlFor="admin-password">管理员密码</Label>
          <Input
            id="admin-password"
            type="password"
            value={password}
            onChange={e => setPassword(e.target.value)}
            autoComplete="current-password"
            placeholder="请输入管理员密码"
          />
        </div>
        {error && <p className="text-xs text-destructive">{error}</p>}
        <Button type="submit" className="w-full" disabled={busy || !password}>
          {busy && <Loader2 size={14} className="animate-spin" />}
          登录
        </Button>
      </form>

      <p className="mt-6 text-center text-[11px] leading-relaxed text-muted-foreground">
        初始密码由环境变量 <code>ANIPICK_ADMIN_PASSWORD</code> 决定（首次启动写入数据库）。
        登录后请立即在「设置」中修改；公开部署时请通过反向代理或防火墙限制
        <code>/admin</code> 与 <code>/api/admin</code> 的访问来源。
      </p>
    </div>
  )
}
