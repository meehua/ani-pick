import React from 'react'

// 跟随系统的主题管理（next-themes 的 Vite 版等价实现）
// theme = 'system' 时监听 prefers-color-scheme 变化；手动选择后写入 localStorage
export type Theme = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'anipick-theme'

type ThemeContextValue = {
  theme: Theme
  resolvedTheme: 'light' | 'dark'
  setTheme: (t: Theme) => void
}

const ThemeContext = React.createContext<ThemeContextValue | null>(null)

function systemPrefersDark(): boolean {
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

function resolve(theme: Theme): 'light' | 'dark' {
  if (theme === 'system') return systemPrefersDark() ? 'dark' : 'light'
  return theme
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = React.useState<Theme>(() => {
    const saved = localStorage.getItem(STORAGE_KEY)
    return saved === 'light' || saved === 'dark' ? saved : 'system'
  })
  const [resolvedTheme, setResolved] = React.useState<'light' | 'dark'>(() =>
    resolve(theme),
  )

  React.useEffect(() => {
    const apply = () => {
      const r = resolve(theme)
      setResolved(r)
      document.documentElement.classList.toggle('dark', r === 'dark')
      document.documentElement.style.colorScheme = r
    }
    apply()
    if (theme !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    mq.addEventListener('change', apply)
    return () => mq.removeEventListener('change', apply)
  }, [theme])

  const setTheme = React.useCallback((t: Theme) => {
    localStorage.setItem(STORAGE_KEY, t)
    setThemeState(t)
  }, [])

  return (
    <ThemeContext.Provider value={{ theme, resolvedTheme, setTheme }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme(): ThemeContextValue {
  const ctx = React.useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme 必须在 ThemeProvider 内使用')
  return ctx
}
