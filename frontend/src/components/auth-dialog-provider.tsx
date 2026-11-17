import * as React from 'react'
import { LoginDialog } from './login-dialog'

const Ctx = React.createContext<{ openLogin: () => void } | null>(null)

export function AuthDialogProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = React.useState(false)
  const value = React.useMemo(() => ({ openLogin: () => setOpen(true) }), [])
  return (
    <Ctx.Provider value={value}>
      {children}
      <LoginDialog open={open} onOpenChange={setOpen} />
    </Ctx.Provider>
  )
}

export function useAuthDialog() {
  const ctx = React.useContext(Ctx)
  if (!ctx) throw new Error('useAuthDialog 必须在 AuthDialogProvider 内使用')
  return ctx
}
