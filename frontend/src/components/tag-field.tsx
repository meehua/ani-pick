import * as React from 'react'
import { Check, ChevronDown, Plus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ctl } from '@/lib/ctl'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList,
} from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { RemovableChip } from './ui-kit'
import type { TagStat } from '../api/types'
import { fmtNum } from '../lib/format'
import { useTagOptions } from '../lib/use-tag-options'

// 多选检索核：Popover + Command 浮层（搜索 / 已选 chips / 候选列表 / 清除-完成栏）。
// 触发器两种形态：compact 显示「标签 + 计数」，全宽显示已选 chips 或占位文案。
// open/search 由调用方持有——TagField 的远程候选依赖这两个状态做请求。
function MultiSelectCore({
  value,
  onChange,
  options,
  open,
  onOpenChange,
  search,
  onSearchChange,
  loading = false,
  allowCreate = false,
  compact,
  compactLabel,
  fullLabel,
  fullPlaceholder,
  searchPlaceholder,
  emptyText,
  ariaLabel,
  popoverWidth,
}: {
  value: string[]
  onChange: (v: string[]) => void
  options: TagStat[]
  open: boolean
  onOpenChange: (v: boolean) => void
  search: string
  onSearchChange: (v: string) => void
  loading?: boolean
  allowCreate?: boolean
  compact: boolean
  compactLabel: string
  /** 全宽且已选时显示；缺省用「已选 N 个」 */
  fullLabel?: React.ReactNode
  fullPlaceholder: string
  searchPlaceholder: string
  emptyText: string
  ariaLabel: string
  popoverWidth: string
}) {
  const toggle = (name: string) => {
    onChange(value.includes(name) ? value.filter(v => v !== name) : [...value, name])
  }

  const trimmed = search.trim()
  const canCreate =
    allowCreate && trimmed !== '' && !value.includes(trimmed) && !options.some(i => i.name === trimmed)

  return (
    <Popover
      open={open}
      onOpenChange={next => {
        onOpenChange(next)
        if (!next) onSearchChange('')
      }}
    >
      <PopoverTrigger asChild>
        <Button
          type="button"
          role="combobox"
          variant="outline"
          size={compact ? 'sm' : 'default'}
          aria-expanded={open}
          aria-label={ariaLabel}
          className={cn(
            'gap-1.5 font-normal',
            compact ? 'shrink-0' : `${ctl.wrap} w-full justify-between px-3 py-1.5`,
          )}
        >
          <span
            className={cn(
              'flex min-w-0 flex-1 items-center gap-1',
              compact ? 'truncate' : 'flex-wrap text-left',
            )}
          >
            {compact ? (
              compactLabel
            ) : value.length > 0 ? (
              fullLabel ?? <span className="truncate text-muted-foreground">已选 {value.length} 个</span>
            ) : (
              <span className="truncate text-muted-foreground">{fullPlaceholder}</span>
            )}
          </span>
          {value.length > 0 && compact && (
            <Badge variant="secondary" className="num h-5 px-1.5 text-[10px]">
              {value.length}
            </Badge>
          )}
          <ChevronDown size={14} className="shrink-0 opacity-60" aria-hidden />
        </Button>
      </PopoverTrigger>

      <PopoverContent align="start" className={`${popoverWidth} p-0`}>
        <Command shouldFilter={false}>
          <CommandInput placeholder={searchPlaceholder} value={search} onValueChange={onSearchChange} />
          {value.length > 0 && (
            <div className="flex flex-wrap gap-1.5 border-b p-2">
              {value.map(v => (
                <RemovableChip key={v} label={v} onRemove={() => toggle(v)} />
              ))}
            </div>
          )}
          <CommandList className="max-h-72">
            <CommandEmpty>{loading ? '检索中…' : emptyText}</CommandEmpty>
            {options.length > 0 && (
              <CommandGroup>
                {options.map(t => (
                  <CommandItem
                    key={t.name}
                    value={t.name}
                    onSelect={() => {
                      toggle(t.name)
                      onSearchChange('')
                    }}
                  >
                    <Check
                      size={14}
                      aria-hidden
                      className={cn(value.includes(t.name) ? 'opacity-100' : 'opacity-0')}
                    />
                    <span className="min-w-0 flex-1 truncate">{t.name}</span>
                    <span className="num shrink-0 text-[11px] text-muted-foreground">
                      {fmtNum(t.count)}
                    </span>
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
                    onSearchChange('')
                  }}
                >
                  <Plus size={14} aria-hidden />
                  <span className="truncate">新增「{trimmed}」</span>
                </CommandItem>
              </CommandGroup>
            )}
          </CommandList>
          <div className="flex items-center justify-between border-t p-2">
            <Button
              type="button"
              variant="ghost"
              size="xs"
              onClick={() => onChange([])}
              disabled={value.length === 0}
            >
              清除
            </Button>
            <Button type="button" size="xs" onClick={() => onOpenChange(false)}>
              完成
            </Button>
          </div>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

// 标签检索：打开浮层才请求候选，输入 150ms 防抖；支持多选与手动新增。
export function TagField({
  value,
  onChange,
  types,
  label = '标签',
  placeholder = '搜索或新增标签',
  ariaLabel,
  allowCreate = true,
  compact = true,
}: {
  value: string[]
  onChange: (v: string[]) => void
  types: number[]
  label?: string
  placeholder?: string
  ariaLabel: string
  allowCreate?: boolean
  compact?: boolean
}) {
  const [open, setOpen] = React.useState(false)
  const [search, setSearch] = React.useState('')
  const { items, loading } = useTagOptions(search, types, 40, open)

  return (
    <MultiSelectCore
      value={value}
      onChange={onChange}
      options={items}
      loading={loading}
      allowCreate={allowCreate}
      open={open}
      onOpenChange={setOpen}
      search={search}
      onSearchChange={setSearch}
      compact={compact}
      compactLabel={label}
      fullLabel={
        value.length > 0
          ? value.map(v => (
              <Badge key={v} variant="secondary" className="max-w-full font-normal">
                <span className="truncate">{v}</span>
              </Badge>
            ))
          : undefined
      }
      fullPlaceholder={placeholder}
      searchPlaceholder="搜索标签…"
      emptyText="没有匹配的标签"
      ariaLabel={ariaLabel}
      popoverWidth="w-[min(340px,calc(100vw-2rem))]"
    />
  )
}

// 分类标签：候选来自 /api/meta-tags，本地过滤。
export function MetaTagField({
  value,
  onChange,
  options,
  placeholder,
  ariaLabel,
  compact = true,
}: {
  value: string[]
  onChange: (v: string[]) => void
  options: TagStat[]
  placeholder: string
  ariaLabel: string
  compact?: boolean
}) {
  const [open, setOpen] = React.useState(false)
  const [search, setSearch] = React.useState('')
  const q = search.trim().toLowerCase()
  const filtered = options.filter(
    o => !value.includes(o.name) && (!q || o.name.toLowerCase().includes(q)),
  )

  return (
    <MultiSelectCore
      value={value}
      onChange={onChange}
      options={filtered}
      open={open}
      onOpenChange={setOpen}
      search={search}
      onSearchChange={setSearch}
      compact={compact}
      compactLabel="分类"
      fullLabel={value.length > 0 ? `已选 ${value.length} 个分类` : undefined}
      fullPlaceholder={placeholder}
      searchPlaceholder="搜索分类…"
      emptyText="没有匹配的分类"
      ariaLabel={ariaLabel}
      popoverWidth="w-[min(320px,calc(100vw-2rem))]"
    />
  )
}
