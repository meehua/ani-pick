import * as React from 'react'
import { Check, ChevronsUpDown, Loader2, Plus, X } from 'lucide-react'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import {
  Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList,
} from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { getTags } from '../api/client'
import type { TagStat } from '../api/types'

// 标签选择器：候选项从本站索引实时检索（不是静态列表），支持手动创建。
export function TagPicker({
  value,
  onChange,
  types,
  placeholder,
  allowCreate = true,
  className,
  ariaLabel,
}: {
  value: string[]
  onChange: (v: string[]) => void
  types: number[]
  placeholder: string
  allowCreate?: boolean
  className?: string
  ariaLabel?: string
}) {
  const [open, setOpen] = React.useState(false)
  const [search, setSearch] = React.useState('')
  const [options, setOptions] = React.useState<TagStat[]>([])
  const [loading, setLoading] = React.useState(false)
  const typesKey = types.join(',')

  React.useEffect(() => {
    if (!open) return
    const ctrl = new AbortController()
    const timer = window.setTimeout(async () => {
      setLoading(true)
      try {
        const res = await getTags(search.trim(), typesKey ? typesKey.split(',').map(Number) : [], 40, ctrl.signal)
        setOptions(res.items)
      } catch (e) {
        if ((e as Error).name !== 'AbortError') setOptions([])
      } finally {
        setLoading(false)
      }
    }, 200)
    return () => {
      window.clearTimeout(timer)
      ctrl.abort()
    }
  }, [open, search, typesKey])

  const toggle = (v: string) =>
    onChange(value.includes(v) ? value.filter(x => x !== v) : [...value, v])

  const trimmed = search.trim()
  const canCreate =
    allowCreate && trimmed !== '' && !value.includes(trimmed) && !options.some(o => o.name === trimmed)

  return (
    <Popover
      open={open}
      onOpenChange={o => {
        setOpen(o)
        if (!o) setSearch('')
      }}
    >
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          aria-label={ariaLabel}
          className={cn(
            'h-auto min-h-9 w-full justify-between px-3 py-1.5 font-normal',
            !value.length && 'text-muted-foreground',
            className,
          )}
        >
          <span className="flex min-w-0 flex-1 flex-wrap items-center gap-1 text-left">
            {value.length === 0 && <span className="truncate">{placeholder}</span>}
            {value.slice(0, 3).map(v => (
              <span
                key={v}
                className="inline-flex items-center gap-1 rounded-md bg-secondary px-1.5 py-0.5 text-xs font-medium text-secondary-foreground"
                onClick={e => {
                  e.stopPropagation()
                  toggle(v)
                }}
                title="点击移除"
              >
                {v}
                <X size={10} className="opacity-60" />
              </span>
            ))}
            {value.length > 3 && <span className="text-xs text-muted-foreground">+{value.length - 3}</span>}
          </span>
          <ChevronsUpDown size={14} className="shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[280px] p-0" align="start">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder="搜索标签…"
            value={search}
            onValueChange={setSearch}
            className="h-9"
          />
          <CommandList>
            {loading && (
              <div className="flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground">
                <Loader2 size={12} className="animate-spin" /> 正在检索…
              </div>
            )}
            <CommandEmpty>{loading ? '' : '没有匹配的标签'}</CommandEmpty>
            {options.length > 0 && (
              <CommandGroup heading="索引中的标签">
                {options.map(o => (
                  <CommandItem
                    key={o.name}
                    value={o.name}
                    onSelect={() => {
                      toggle(o.name)
                      setSearch('')
                    }}
                  >
                    <Check
                      size={14}
                      className={cn('mr-2', value.includes(o.name) ? 'opacity-100' : 'opacity-0')}
                    />
                    <span className="flex-1 truncate">{o.name}</span>
                    <span className="text-[10px] tabular-nums text-muted-foreground">{o.count}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {canCreate && (
              <CommandGroup heading="手动添加">
                <CommandItem
                  value={`__create__${trimmed}`}
                  onSelect={() => {
                    toggle(trimmed)
                    setSearch('')
                  }}
                >
                  <Plus size={14} className="mr-2" />
                  使用「{trimmed}」
                </CommandItem>
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
