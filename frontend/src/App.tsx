import { lazy, Suspense, type ReactNode } from 'react'
import { Link, NavLink, Route, Routes } from 'react-router-dom'
import { Compass, Home, KeyRound, Loader2, Moon, Shield, Sun, MonitorSmartphone, UserRound } from 'lucide-react'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { AuthDialogProvider, useAuthDialog } from '@/components/auth-dialog-provider'
import { useTheme, type Theme } from '@/components/theme-provider'
import { useCredential } from './lib/credential'
import HomePage from './pages/HomePage'
import NotFoundPage from './pages/NotFoundPage'

// 非首屏页面按需加载：把浏览/条目/管理代码从首屏 bundle 里拆出去
const BrowsePage = lazy(() => import('./pages/BrowsePage'))
const SubjectPage = lazy(() => import('./pages/SubjectPage'))
const AdminLoginPage = lazy(() => import('./pages/AdminLoginPage'))
const AdminPage = lazy(() => import('./pages/AdminPage'))

function RouteLoading() {
  return (
    <div className="flex items-center justify-center py-24 text-muted-foreground">
      <Loader2 className="animate-spin" size={20} />
    </div>
  )
}

function ThemeSwitch() {
  const { theme, setTheme } = useTheme()
  const items: { value: Theme; label: string; icon: ReactNode }[] = [
    { value: 'light', label: '浅色', icon: <Sun size={14} /> },
    { value: 'dark', label: '深色', icon: <Moon size={14} /> },
    { value: 'system', label: '跟随系统', icon: <MonitorSmartphone size={14} /> },
  ]
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="切换主题" className="size-8">
          {theme === 'light' ? <Sun size={15} /> : theme === 'dark' ? <Moon size={15} /> : <MonitorSmartphone size={15} />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {items.map(it => (
          <DropdownMenuItem key={it.value} onClick={() => setTheme(it.value)} className="gap-2">
            {it.icon} {it.label}
            {theme === it.value && <span className="ml-auto text-[10px] text-muted-foreground">当前</span>}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function AccountButton() {
  const credential = useCredential()
  const { openLogin } = useAuthDialog()
  if (credential) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="ghost" size="sm" onClick={openLogin} className="gap-1.5 px-2">
            <UserRound size={14} />
            <span className="max-w-28 truncate text-xs">
              {credential.user?.nickname || credential.user?.username || '已登录'}
            </span>
          </Button>
        </TooltipTrigger>
        <TooltipContent>凭据保存在本机浏览器，点击管理</TooltipContent>
      </Tooltip>
    )
  }
  return (
    <Button variant="outline" size="sm" onClick={openLogin} className="gap-1.5">
      <KeyRound size={13} /> 登录
    </Button>
  )
}

const navClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm transition-colors',
    isActive ? 'bg-secondary text-foreground' : 'text-muted-foreground hover:text-foreground',
  )

function Shell() {
  return (
    <div className="flex min-h-dvh flex-col">
      <header className="sticky top-0 z-40 border-b bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-14 w-full max-w-7xl items-center gap-2 px-4">
          <Link to="/" className="mr-2 flex items-center gap-2 font-semibold tracking-tight">
            <span className="flex size-6 items-center justify-center rounded bg-primary text-[11px] font-bold text-primary-foreground">
              A
            </span>
            AniPick
          </Link>
          <nav className="flex items-center gap-1">
            <NavLink to="/" end className={navClass}>
              <Home size={14} /> 首页
            </NavLink>
            <NavLink to="/browse" className={navClass}>
              <Compass size={14} /> 浏览
            </NavLink>
          </nav>
          <div className="ml-auto flex items-center gap-1.5">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon" asChild className="size-8">
                  <Link to="/admin" aria-label="管理后台">
                    <Shield size={15} />
                  </Link>
                </Button>
              </TooltipTrigger>
              <TooltipContent>管理后台</TooltipContent>
            </Tooltip>
            <AccountButton />
            <ThemeSwitch />
          </div>
        </div>
      </header>

      <main className="flex-1">
        <div className="mx-auto w-full max-w-7xl px-4 py-6">
          <Suspense fallback={<RouteLoading />}>
            <Routes>
              <Route path="/" element={<HomePage />} />
              <Route path="/browse" element={<BrowsePage />} />
              <Route path="/subject/:id" element={<SubjectPage />} />
              <Route path="/admin/login" element={<AdminLoginPage />} />
              <Route path="/admin" element={<AdminPage />} />
              <Route path="*" element={<NotFoundPage />} />
            </Routes>
          </Suspense>
        </div>
      </main>

      <footer className="border-t py-6">
        <div className="mx-auto flex w-full max-w-7xl flex-col items-center justify-between gap-2 px-4 text-xs text-muted-foreground sm:flex-row">
          <p>
            AniPick · 本站索引（自建） + 浏览器直连 Bangumi · 数据版权归 Bangumi 及原作者所有
          </p>
          <div className="flex items-center gap-3">
            <a href="https://bgm.tv" target="_blank" rel="noreferrer" className="hover:text-foreground">
              bgm.tv
            </a>
            <Link to="/admin" className="hover:text-foreground">管理</Link>
          </div>
        </div>
      </footer>
    </div>
  )
}

export default function App() {
  return (
    <AuthDialogProvider>
      <Shell />
    </AuthDialogProvider>
  )
}
