import * as React from 'react'
import { Search, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/input'

// 搜索输入共享层：左图标 + 输入 + 可选清除按钮 + "/" 快捷聚焦。
// 提交语义由调用方决定（首页跳转 / 索引页防抖过滤），这里只管输入本身。
export function SearchInput({
  value,
  onChange,
  onClear,
  placeholder,
  className,
  inputClassName,
  size = 'sm',
  showKbd = false,
  ariaLabel = '搜索作品',
}: {
  value: string
  onChange: (v: string) => void
  onClear?: () => void
  placeholder?: string
  className?: string
  inputClassName?: string
  size?: 'sm' | 'lg'
  showKbd?: boolean
  ariaLabel?: string
}) {
  const inputRef = React.useRef<HTMLInputElement>(null)

  React.useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const el = document.activeElement
      const typing =
        el instanceof HTMLInputElement ||
        el instanceof HTMLTextAreaElement ||
        (el instanceof HTMLElement && el.isContentEditable)
      if (e.key === '/' && !typing) {
        e.preventDefault()
        inputRef.current?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const lg = size === 'lg'

  return (
    <div className={cn('relative min-w-0 flex-1', className)}>
      <Search
        size={lg ? 18 : 15}
        aria-hidden
        className={cn(
          'pointer-events-none absolute top-1/2 -translate-y-1/2 text-muted-foreground',
          lg ? 'left-3' : 'left-2.5',
        )}
      />
      <Input
        ref={inputRef}
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={ariaLabel}
        className={cn(
          lg ? 'h-11 pl-10 pr-12 text-[15px]' : 'h-9 pl-8 pr-8',
          'bg-card',
          inputClassName,
        )}
      />
      {value && onClear && (
        <button
          type="button"
          aria-label="清空搜索"
          onClick={onClear}
          className={cn(
            'absolute top-1/2 -translate-y-1/2 rounded-sm p-1 text-muted-foreground hover:bg-accent hover:text-foreground',
            lg ? 'right-2.5' : 'right-1.5',
          )}
        >
          <X size={13} aria-hidden />
        </button>
      )}
      {/* 清除按钮出现时让位；两者不会同时占住右端 */}
      {showKbd && !(value && onClear) && (
        <kbd className="pointer-events-none absolute right-2 top-1/2 hidden -translate-y-1/2 rounded border bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground sm:block">
          /
        </kbd>
      )}
    </div>
  )
}
