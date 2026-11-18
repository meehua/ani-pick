import { lazy, Suspense, useEffect, useRef, type ReactNode } from 'react'
import { Link, Route, Routes, useLocation, useNavigationType, useSearchParams } from 'react-router-dom'
import { Home, LayoutGrid, MonitorSmartphone, Moon, Sun, Trophy, UserRound } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { UserAvatar } from '@/components/user-avatar'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { AuthDialogProvider, useAuthDialog } from '@/components/auth-dialog-provider'
import { useTheme, type Theme } from '@/components/theme-provider'
import { RouteLoading } from '@/components/ui-kit'
import { useCredential } from './lib/credential'
import HomePage from './pages/HomePage'
import SubjectPage from './pages/subject/SubjectPage'
import NotFoundPage from './pages/NotFoundPage'

const BrowsePage = lazy(() => import('./pages/BrowsePage'))
const AdminLoginPage = lazy(() => import('./pages/AdminLoginPage'))
const AdminPage = lazy(() => import('./pages/AdminPage'))

function ScrollManager() {
  const { pathname } = useLocation()
  const navType = useNavigationType()
  const first = useRef(true)

  useEffect(() => {
    if ('scrollRestoration' in history) history.scrollRestoration = 'manual'
  }, [])

  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    if (navType === 'PUSH') requestAnimationFrame(() => window.scrollTo(0, 0))
  }, [pathname, navType])

  return null
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
    const name = credential.user?.nickname || credential.user?.username || '已登录'
    return (
      <Button
        variant="ghost"
        size="sm"
        onClick={openLogin}
        className="gap-2 px-1.5"
        aria-label="账号与数据通道"
      >
        <UserAvatar src={credential.user?.avatar} alt={name} />
        <span className="hidden max-w-24 truncate md:block">{name}</span>
      </Button>
    )
  }

  return (
    <Button variant="outline" size="sm" onClick={openLogin}>
      登录
    </Button>
  )
}

function Wordmark() {
  return (
    <Link to="/" className="flex shrink-0 items-center gap-2.5" aria-label="AniPick 首页">
      <span className="grid size-7 place-items-center rounded-lg bg-primary text-[13px] font-semibold text-primary-foreground">
        A
      </span>
      <span className="text-[16px] font-semibold tracking-tight">AniPick</span>
    </Link>
  )
}

// 导航高亮判定：高分榜是 /browse 的特例（types=2 + score + rating_min=50），
// 顶栏与移动底栏共用同一份结论。
function useNavActive() {
  const location = useLocation()
  const [sp] = useSearchParams()
  const rank =
    location.pathname === '/browse' &&
    sp.get('types') === '2' &&
    sp.get('sort') === 'score' &&
    sp.get('rating_min') === '50'
  const browse =
    (location.pathname === '/browse' && !rank) || location.pathname.startsWith('/subject/')
  return { pathname: location.pathname, rank, browse }
}

function TopNav() {
  const { pathname, rank, browse } = useNavActive()
  const links = [
    { to: '/', label: '首页', active: pathname === '/' },
    { to: '/browse', label: '作品索引', active: browse },
    { to: '/browse?types=2&sort=score&rating_min=50', label: '高分榜', active: rank },
  ]

  return (
    <nav className="ml-1 hidden items-center md:flex" aria-label="主导航">
      {links.map(l => (
        <Link
          key={l.to}
          to={l.to}
          className={cn(
            'relative flex h-16 items-center px-3 text-sm transition-colors',
            l.active
              ? 'font-medium text-foreground'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {l.label}
          {l.active && (
            <span className="absolute inset-x-2 bottom-0 h-[2px] rounded-full bg-foreground" />
          )}
        </Link>
      ))}
    </nav>
  )
}

function MobileTabBar() {
  const { pathname, rank, browse } = useNavActive()
  const { openLogin } = useAuthDialog()

  const item = (active: boolean) =>
    cn(
      'flex flex-1 flex-col items-center justify-center gap-1 text-[11px] transition-colors',
      active ? 'font-medium text-foreground' : 'text-muted-foreground',
    )

  return (
    <nav
      className="fixed inset-x-0 bottom-0 z-40 flex h-14 border-t bg-background/95 pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden"
      aria-label="底部导航"
    >
      <Link to="/" className={item(pathname === '/')}>
        <Home size={19} aria-hidden /> 首页
      </Link>
      <Link to="/browse" className={item(browse)}>
        <LayoutGrid size={19} aria-hidden /> 索引
      </Link>
      <Link to="/browse?types=2&sort=score&rating_min=50" className={item(rank)}>
        <Trophy size={19} aria-hidden /> 高分
      </Link>
      <button type="button" onClick={openLogin} className={item(false)}>
        <UserRound size={19} aria-hidden /> 我的
      </button>
    </nav>
  )
}

function Shell() {
  const location = useLocation()

  return (
    <div className="min-h-dvh">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-primary focus:px-3 focus:py-2 focus:text-sm focus:text-primary-foreground"
      >
        跳到主要内容
      </a>

      <header className="sticky top-0 z-40 border-b bg-background/90 backdrop-blur">
        <div className="mx-auto flex h-16 max-w-[1200px] items-center gap-3 px-4 sm:px-6">
          <Wordmark />
          <TopNav />

          <div className="ml-auto flex shrink-0 items-center gap-1">
            <AccountButton />
            <ThemeSwitch />
          </div>
        </div>
      </header>

      <main id="main" className="mx-auto w-full max-w-[1200px] px-4 pb-24 pt-6 sm:px-6 md:pb-10">
        <Suspense fallback={<RouteLoading />}>
          <div key={location.pathname} className="page-in">
            <Routes>
              <Route path="/" element={<HomePage />} />
              <Route path="/browse" element={<BrowsePage />} />
              <Route path="/subject/:id" element={<SubjectPage />} />
              <Route path="/admin/login" element={<AdminLoginPage />} />
              <Route path="/admin" element={<AdminPage />} />
              <Route path="*" element={<NotFoundPage />} />
            </Routes>
          </div>
        </Suspense>

        <footer className="mt-12 border-t pt-6">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <div className="flex items-center gap-2">
                <span className="grid size-6 place-items-center rounded-md bg-primary text-[11px] font-semibold text-primary-foreground">
                  A
                </span>
                <span className="text-sm font-semibold">AniPick</span>
              </div>
            </div>
            <div className="flex flex-wrap gap-x-5 gap-y-2 text-xs text-muted-foreground">
              <a href="https://bgm.tv" target="_blank" rel="noreferrer" className="hover:text-foreground">
                bgm.tv
              </a>

            </div>
          </div>
        </footer>
      </main>

      <MobileTabBar />
    </div>
  )
}

export default function App() {
  return (
    <AuthDialogProvider>
      <ScrollManager />
      <Shell />
    </AuthDialogProvider>
  )
}
