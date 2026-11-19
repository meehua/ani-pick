import * as React from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { SearchInput } from '@/components/search-input'

// 全局搜索：浏览页写入当前 URL 的 keyword，其他页面新建浏览查询。
export function SearchBox({
  size = 'sm',
  className,
  placeholder,
  onDark = false,
}: {
  size?: 'sm' | 'lg'
  className?: string
  placeholder?: string
  onDark?: boolean
}) {
  const navigate = useNavigate()
  const location = useLocation()
  const [sp] = useSearchParams()
  const onBrowse = location.pathname === '/browse'
  const current = onBrowse ? (sp.get('keyword') ?? '') : ''
  const [value, setValue] = React.useState(current)

  React.useEffect(() => {
    setValue(current)
  }, [current])

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const next = new URLSearchParams(onBrowse ? sp : undefined)
    const q = value.trim()
    if (q) next.set('keyword', q)
    else next.delete('keyword')
    next.delete('page')
    navigate(`/browse${next.toString() ? `?${next.toString()}` : ''}`)
  }

  const lg = size === 'lg'

  return (
    <form onSubmit={submit} className={cn('flex items-center gap-2', className)}>
      <SearchInput
        value={value}
        onChange={setValue}
        size={size}
        showKbd={!lg}
        placeholder={placeholder ?? (lg ? '搜索作品名、原名或标签' : '搜索作品')}
      />
      {lg && (
        <Button
          type="submit"
          size="lg"
          className={cn('h-11 shrink-0 px-5', onDark && 'dark:bg-white dark:text-black dark:hover:bg-white/90')}
        >
          搜索
        </Button>
      )}
    </form>
  )
}
