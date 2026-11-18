import * as React from 'react'

// 登录弹窗按需加载：弹窗里的 Dialog / Select / Command 不进入首屏包
const LoginDialog = React.lazy(() =>
  import('./login-dialog').then(m => ({ default: m.LoginDialog })),
)

const Ctx = React.createContext<{ openLogin: () => void } | null>(null)

export function AuthDialogProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = React.useState(false)
  const value = React.useMemo(() => ({ openLogin: () => setOpen(true) }), [])

  return (
    <Ctx.Provider value={value}>
      {children}
      {open && (
        <React.Suspense fallback={null}>
          <LoginDialog open={open} onOpenChange={setOpen} />
        </React.Suspense>
      )}
    </Ctx.Provider>
  )
}

export function useAuthDialog() {
  const ctx = React.useContext(Ctx)
  if (!ctx) throw new Error('useAuthDialog 必须在 AuthDialogProvider 内使用')
  return ctx
}
