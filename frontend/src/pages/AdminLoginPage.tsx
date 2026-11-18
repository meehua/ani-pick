import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import { Loader2, LockKeyhole } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
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
    <div className="flex min-h-[70vh] items-center justify-center py-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <div className="mb-1 grid size-9 place-items-center rounded-lg bg-secondary">
            <LockKeyhole size={17} aria-hidden />
          </div>
          <CardTitle>管理员登录</CardTitle>
          <CardDescription>管理抓取、索引与站点设置。</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="space-y-4"
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

            <Button type="submit" className="w-full gap-1.5" disabled={busy || !password}>
              {busy && <Loader2 size={14} className="animate-spin" aria-hidden />}
              登录
            </Button>

            <p className="text-[11px] leading-relaxed text-muted-foreground">
              初始密码由 <code className="rounded bg-muted px-1 py-0.5">ANIPICK_ADMIN_PASSWORD</code>{' '}
              决定（首次启动写入数据库）。登录后请立即在设置中修改；公开部署时请限制
              <code className="mx-1 rounded bg-muted px-1 py-0.5">/admin</code> 的访问来源。
            </p>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
