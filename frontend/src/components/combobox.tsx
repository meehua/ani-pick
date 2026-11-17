import * as React from 'react'
import { Check, ChevronsUpDown, Plus, X } from 'lucide-react'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import {
  Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList,
} from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

export interface ComboOption {
  label: string
  value: string
  hint?: string
}

// 多选 Combobox：官方「Combobox」模式（Popover + Command），支持手动创建候选项
export function MultiCombobox({
  value,
  onChange,
  options,
  placeholder,
  searchPlaceholder = '搜索…',
  emptyText = '没有匹配项',
  allowCreate,
  className,
  ariaLabel,
}: {
  value: string[]
  onChange: (v: string[]) => void
  options: ComboOption[]
  placeholder: string
  searchPlaceholder?: string
  emptyText?: string
  allowCreate?: boolean
  className?: string
  ariaLabel?: string
}) {
  const [open, setOpen] = React.useState(false)
  const [search, setSearch] = React.useState('')

  const toggle = (v: string) =>
    onChange(value.includes(v) ? value.filter(x => x !== v) : [...value, v])

  const q = search.trim().toLowerCase()
  const filtered = options.filter(
    o =>
      !value.includes(o.value) &&
      (!q || o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q)),
  )
  const trimmed = search.trim()
  const canCreate =
    allowCreate && trimmed !== '' && !value.includes(trimmed) && !options.some(o => o.value === trimmed)

  const selected = value.map(v => options.find(o => o.value === v) ?? { label: v, value: v })

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
            {selected.length === 0 && <span>{placeholder}</span>}
            {selected.slice(0, 4).map(s => (
              <span
                key={s.value}
                className="inline-flex items-center gap-1 rounded-md bg-secondary px-1.5 py-0.5 text-xs font-medium text-secondary-foreground"
                onClick={e => {
                  e.stopPropagation()
                  toggle(s.value)
                }}
                title="点击移除"
              >
                {s.label}
                <X size={10} className="opacity-60" />
              </span>
            ))}
            {selected.length > 4 && (
              <span className="text-xs text-muted-foreground">+{selected.length - 4}</span>
            )}
          </span>
          <ChevronsUpDown size={14} className="shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[260px] p-0" align="start">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={searchPlaceholder}
            value={search}
            onValueChange={setSearch}
            className="h-9"
          />
          <CommandList>
            <CommandEmpty>{filtered.length ? '' : emptyText}</CommandEmpty>
            {filtered.length > 0 && (
              <CommandGroup>
                {filtered.slice(0, 80).map(o => (
                  <CommandItem
                    key={o.value}
                    value={o.value}
                    onSelect={() => {
                      toggle(o.value)
                      setSearch('')
                    }}
                  >
                    <Check
                      size={14}
                      className={cn('mr-2', value.includes(o.value) ? 'opacity-100' : 'opacity-0')}
                    />
                    <span className="flex-1">{o.label}</span>
                    {o.hint && <span className="text-xs text-muted-foreground">{o.hint}</span>}
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
                  新增「{trimmed}」
                </CommandItem>
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
