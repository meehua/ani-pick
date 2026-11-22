import * as React from 'react'
import { RotateCcw } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ctl } from '@/lib/ctl'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  Sheet, SheetClose, SheetContent, SheetDescription, SheetHeader, SheetTitle,
} from '@/components/ui/sheet'
import { RemovableChip } from './ui-kit'
import { codesOfKind, kindCounts, kindLabel, kindOf, sourceLabel, typeLabel } from '../api/vocab'
import type { TagStat } from '../api/types'
import { fmtNum } from '../lib/format'
import { MetaTagField, TagField } from './tag-field'
import { ERAS } from '../lib/eras'
import { useTagOptions } from '../lib/use-tag-options'

export type PatchValue = string | number | number[] | string[] | undefined
export type PatchFn = (next: Record<string, PatchValue>, resetPage?: boolean) => void

export interface ActiveFilterState {
  keyword?: string
  source?: string
  types: number[]
  tags: string[]
  exclude: string[]
  metaTags: string[]
  tagMode: 'and' | 'or'
  pctMin?: number
  dateFrom?: string
  dateTo?: string
  rankMin?: number
  rankMax?: number
  scoreMin?: number
  ratingMin?: number
  nsfw: string
}

const NSFW_LABEL: Record<string, string> = {
  hide: '隐藏敏感内容',
  only: '只看敏感内容',
  show: '包含敏感内容',
}

const SCORE_TILES = [
  { value: 0, label: '不限' },
  { value: 6, label: '6+' },
  { value: 7, label: '7+' },
  { value: 8, label: '8+' },
  { value: 9, label: '9+' },
]


function SectionLabel({ children }: { children: React.ReactNode }) {
  return <p className="mb-2 text-xs font-medium text-muted-foreground">{children}</p>
}

// 作品类型 chips 是站点分类（kind 层）：选一个分类 = 选中它名下的全部
// 原生类型编码（如「剧集」同时含三次元与电视季）。各源的原生叫法在
// 来源徽标与详情页展示，筛选这里用站点通用类目。
function TypeButtons({
  types,
  onChange,
  className,
  byType,
}: {
  types: number[]
  onChange: (v: number[]) => void
  className?: string
  /** 索引里各分类的实际条目数（chips 按此出现；meta 未到时全部分类都出现） */
  byType?: Record<string, number>
}) {
  const cards = kindCounts(byType)
  const selected = new Set(types.map(t => kindOf(t)).filter((k): k is string => !!k))
  const value = selected.size ? [...selected] : ['all']
  return (
    <ToggleGroup
      type="multiple"
      value={value}
      onValueChange={v => {
        const hasAll = v.includes('all')
        const kinds = v.filter(x => x !== 'all')
        // 分类 → 该分类名下全部类型编码的并集
        const codes = [...new Set(kinds.flatMap(k => codesOfKind(k)))]
        // 从「全部」直接点分类时 ToggleGroup 会同时带上 'all'：此时按分类走；
        // 已有选中时点「全部」= 清空类型。
        onChange(hasAll ? (types.length === 0 ? codes : []) : codes)
      }}
      className={cn('flex w-full flex-wrap gap-1.5', className)}
    >
      <ToggleGroupItem value="all" className={cn(ctl.bar, 'px-2 text-xs', ctl.on)}>
        全部
      </ToggleGroupItem>
      {cards.map(k => (
        <ToggleGroupItem key={k.kind} value={k.kind} className={cn(ctl.bar, 'px-2 text-xs', ctl.on)}>
          {kindLabel(k.kind)}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

// 多标签筛选：已选 chips 只在 TagField trigger 内，一处；不加热门标签。
function TagFilter({
  value,
  onChange,
  types,
  tagMode,
  onTagModeChange,
  pctMin,
  patch,
}: {
  value: string[]
  onChange: (v: string[]) => void
  types: number[]
  tagMode: 'and' | 'or'
  onTagModeChange: (v: 'and' | 'or') => void
  pctMin?: number
  patch: PatchFn
}) {
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-xs font-medium text-muted-foreground">标签</span>
        <ToggleGroup
          type="single"
          value={tagMode}
          onValueChange={v => v && onTagModeChange(v as 'and' | 'or')}
          aria-label="标签匹配"
          className="h-auto"
        >
          <ToggleGroupItem value="and" className={cn(ctl.bar, 'px-2 text-xs', ctl.on)}>
            全部匹配
          </ToggleGroupItem>
          <ToggleGroupItem value="or" className={cn(ctl.bar, 'px-2 text-xs', ctl.on)}>
            任意匹配
          </ToggleGroupItem>
        </ToggleGroup>
      </div>

      <TagField
        value={value}
        onChange={onChange}
        types={types}
        ariaLabel="标签"
        label="标签"
        placeholder="选择标签（可多选）"
        compact={false}
      />

      <div className="flex items-center gap-2">
        <Label htmlFor="pct-min" className="shrink-0 text-xs text-muted-foreground">
          占比 ≥
        </Label>
        <Input
          id="pct-min"
          type="number"
          inputMode="numeric"
          min={0}
          max={100}
          placeholder="不限"
          value={pctMin ?? ''}
          onChange={e => patch({ pct_min: e.target.value === '' ? undefined : Number(e.target.value) })}
          className={cn(ctl.bar, 'w-20 text-xs')}
        />
        <span className="text-xs text-muted-foreground">%</span>
      </div>
    </div>
  )
}

function DateInputs({
  from,
  to,
  patch,
}: {
  from?: string
  to?: string
  patch: PatchFn
}) {
  return (
    <div className="grid grid-cols-2 gap-2">
      <div className="space-y-1">
        <Label htmlFor="date-from" className="text-xs text-muted-foreground">
          起始
        </Label>
        <Input
          id="date-from"
          type="date"
          value={from ?? ''}
          onChange={e => patch({ date_from: e.target.value || undefined })}
          className={cn(ctl.field, 'px-2 text-xs')}
        />
      </div>
      <div className="space-y-1">
        <Label htmlFor="date-to" className="text-xs text-muted-foreground">
          结束
        </Label>
        <Input
          id="date-to"
          type="date"
          value={to ?? ''}
          onChange={e => patch({ date_to: e.target.value || undefined })}
          className={cn(ctl.field, 'px-2 text-xs')}
        />
      </div>
    </div>
  )
}

function EraButtons({
  from,
  to,
  patch,
}: {
  from?: string
  to?: string
  patch: PatchFn
}) {
  return (
    <div className="flex flex-wrap gap-1">
      {ERAS.map(e => (
        <Button
          key={e.label}
          type="button"
          size="xs"
          variant={from === e.from && to === e.to ? 'default' : 'outline'}
          className={cn(ctl.bar, 'px-2 text-xs')}
          onClick={() => patch({ date_from: e.from, date_to: e.to })}
        >
          {e.label}
        </Button>
      ))}
      <Button
        type="button"
        size="xs"
        variant="ghost"
        className={cn(ctl.bar, 'px-2 text-xs')}
        onClick={() => patch({ date_from: undefined, date_to: undefined })}
        disabled={!from && !to}
      >
        不限
      </Button>
    </div>
  )
}

function RankAndRating({
  ratingMin,
  rankMin,
  rankMax,
  patch,
}: {
  ratingMin?: number
  rankMin?: number
  rankMax?: number
  patch: PatchFn
}) {
  return (
    <div className="grid grid-cols-3 gap-2">
      <div className="space-y-1">
        <Label htmlFor="rating-min" className="text-xs text-muted-foreground">
          评分人数
        </Label>
        <Input
          id="rating-min"
          type="number"
          inputMode="numeric"
          placeholder="不限"
          value={ratingMin ?? ''}
          onChange={e =>
            patch({ rating_min: e.target.value === '' ? undefined : Number(e.target.value) })
          }
          className={cn(ctl.field, 'px-2 text-xs')}
        />
      </div>
      <div className="space-y-1">
        <Label htmlFor="rank-min" className="text-xs text-muted-foreground">
          排名下限
        </Label>
        <Input
          id="rank-min"
          type="number"
          inputMode="numeric"
          placeholder="不限"
          value={rankMin ?? ''}
          onChange={e =>
            patch({ rank_min: e.target.value === '' ? undefined : Number(e.target.value) })
          }
          className={cn(ctl.field, 'px-2 text-xs')}
        />
      </div>
      <div className="space-y-1">
        <Label htmlFor="rank-max" className="text-xs text-muted-foreground">
          排名上限
        </Label>
        <Input
          id="rank-max"
          type="number"
          inputMode="numeric"
          placeholder="不限"
          value={rankMax ?? ''}
          onChange={e =>
            patch({ rank_max: e.target.value === '' ? undefined : Number(e.target.value) })
          }
          className={cn(ctl.field, 'px-2 text-xs')}
        />
      </div>
    </div>
  )
}

// 排除标签用轻量输入；避免再引入一套复杂浮层。
function TagFieldCompact({
  value,
  onChange,
  types,
}: {
  value: string[]
  onChange: (v: string[]) => void
  types: number[]
}) {
  const [text, setText] = React.useState('')
  const { items } = useTagOptions('', types, 20, true)

  return (
    <div className="space-y-1.5">
      {value.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {value.map(v => (
            <RemovableChip
              key={v}
              label={v}
              removeLabel={`移除排除标签 ${v}`}
              maxWidth="max-w-28"
              onRemove={() => onChange(value.filter(x => x !== v))}
            />
          ))}
        </div>
      )}
      <Input
        value={text}
        onChange={e => setText(e.target.value)}
        onKeyDown={e => {
          if (e.key === 'Enter') {
            e.preventDefault()
            const t = text.trim()
            if (t && !value.includes(t)) onChange([...value, t])
            setText('')
          }
        }}
        placeholder="输入后回车排除"
        className={cn(ctl.bar, 'text-xs')}
        list="exclude-tag-options"
      />
      <datalist id="exclude-tag-options">
        {items.map(t => (
          <option key={t.name} value={t.name} />
        ))}
      </datalist>
    </div>
  )
}

export function FilterPanel({
  filters,
  patch,
  metaTagOptions,
  byType,
}: {
  filters: ActiveFilterState
  patch: PatchFn
  metaTagOptions: TagStat[]
  /** 索引里各分类的条目数（作品分类 chips 按此显隐） */
  byType?: Record<string, number>
}) {
  return (
    <div className="space-y-5">
      <section className="space-y-1.5">
        <SectionLabel>作品分类</SectionLabel>
        <TypeButtons types={filters.types} onChange={v => patch({ types: v })} byType={byType} />
      </section>

      <section>
        <TagFilter
          value={filters.tags}
          onChange={v => patch({ tags: v })}
          types={filters.types}
          tagMode={filters.tagMode}
          onTagModeChange={v => patch({ tag_mode: v === 'and' ? undefined : v })}
          pctMin={filters.pctMin}
          patch={patch}
        />
      </section>

      <section className="space-y-1.5">
        <SectionLabel>排除标签</SectionLabel>
        <TagFieldCompact
          value={filters.exclude}
          onChange={v => patch({ exclude: v })}
          types={filters.types}
        />
      </section>

      <section className="space-y-1.5">
        <SectionLabel>分类标签</SectionLabel>
        <MetaTagField
          value={filters.metaTags}
          onChange={v => patch({ meta_tags: v })}
          options={metaTagOptions}
          placeholder="选择分类标签"
          ariaLabel="分类标签"
          compact={false}
        />
      </section>

      <section className="space-y-1.5">
        <SectionLabel>日期范围</SectionLabel>
        <EraButtons from={filters.dateFrom} to={filters.dateTo} patch={patch} />
        <DateInputs from={filters.dateFrom} to={filters.dateTo} patch={patch} />
      </section>

      <section className="space-y-2">
        <SectionLabel>评分</SectionLabel>
        <ToggleGroup
          type="single"
          value={String(filters.scoreMin ?? 0)}
          onValueChange={v => patch({ score_min: v === '0' ? undefined : Number(v) })}
          className="grid w-full grid-cols-5 gap-1.5"
        >
          {SCORE_TILES.map(s => (
            <ToggleGroupItem key={s.value} value={String(s.value)} className={cn(ctl.bar, 'text-xs', ctl.on)}>
              {s.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </section>

      <section className="space-y-2">
        <SectionLabel>排名 / 评分人数</SectionLabel>
        <RankAndRating
          ratingMin={filters.ratingMin}
          rankMin={filters.rankMin}
          rankMax={filters.rankMax}
          patch={patch}
        />
      </section>

      <section className="space-y-1.5">
        <SectionLabel>内容范围</SectionLabel>
        <Select value={filters.nsfw} onValueChange={v => patch({ nsfw: v === 'hide' ? undefined : v })}>
          <SelectTrigger className={cn(ctl.bar, 'w-full text-xs')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {Object.entries(NSFW_LABEL).map(([value, label]) => (
              <SelectItem key={value} value={value}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </section>
    </div>
  )
}

export function FilterSheet({
  open,
  onOpenChange,
  filters,
  patch,
  metaTagOptions,
  byType,
  total,
  onResetAll,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  filters: ActiveFilterState
  patch: PatchFn
  metaTagOptions: TagStat[]
  /** 索引里各分类的条目数（作品分类 chips 按此显隐） */
  byType?: Record<string, number>
  total?: number
  onResetAll: () => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="bottom" className="flex h-[90dvh] flex-col gap-0 p-0">
        <SheetHeader className="border-b px-4 py-3">
          <SheetTitle className="text-base">筛选</SheetTitle>
          <SheetDescription className="text-xs">
            条件会写入 URL，返回或分享时保持原样。
          </SheetDescription>
        </SheetHeader>

        <div className="scrollbar-none min-h-0 flex-1 overflow-y-auto p-4">
          <FilterPanel
            filters={filters}
            patch={patch}
            metaTagOptions={metaTagOptions}
            byType={byType}
          />
        </div>

        <div className="flex items-center gap-2 border-t p-3">
          <Button type="button" variant="ghost" onClick={onResetAll} className="gap-1.5">
            <RotateCcw size={14} aria-hidden /> 重置
          </Button>
          <SheetClose asChild>
            <Button className="flex-1">
              查看 {total === undefined ? '…' : fmtNum(total)} 条结果
            </Button>
          </SheetClose>
        </div>
      </SheetContent>
    </Sheet>
  )
}

export function ActiveFilters({
  filters,
  patch,
  onResetAll,
}: {
  filters: ActiveFilterState
  patch: PatchFn
  onResetAll: () => void
}) {
  const chips: { key: string; label: string; remove: () => void }[] = []

  if (filters.keyword) {
    chips.push({
      key: 'kw',
      label: `关键词：${filters.keyword}`,
      remove: () => patch({ keyword: undefined }),
    })
  }
  if (filters.source) {
    chips.push({
      key: 'source',
      label: `来源：${sourceLabel(filters.source)}`,
      remove: () => patch({ source: undefined }),
    })
  }
  // 类型筛选按站点分类（kind 层）汇总成 chips：某分类的全部编码都在选中集
  // 里时合成一枚（如「剧集」= 三次元 + 电视季）；部分选中或未归类的编码
  // 退回原生标签逐枚展示。
  const kindsSeen = new Map<string, number[]>()
  const loose: number[] = []
  for (const t of filters.types) {
    const k = kindOf(t)
    if (!k) {
      loose.push(t)
      continue
    }
    const arr = kindsSeen.get(k) ?? []
    arr.push(t)
    kindsSeen.set(k, arr)
  }
  for (const [k, picked] of kindsSeen) {
    const all = codesOfKind(k)
    if (all.length > 0 && all.every(c => picked.includes(c))) {
      chips.push({
        key: `kind-${k}`,
        label: kindLabel(k),
        remove: () => patch({ types: filters.types.filter(x => !all.includes(x)) }),
      })
    } else {
      for (const t of picked) {
        chips.push({
          key: `type-${t}`,
          label: typeLabel(t),
          remove: () => patch({ types: filters.types.filter(x => x !== t) }),
        })
      }
    }
  }
  for (const t of filters.tags) {
    chips.push({
      key: `tag-${t}`,
      label: t,
      remove: () => patch({ tags: filters.tags.filter(x => x !== t) }),
    })
  }
  for (const t of filters.metaTags) {
    chips.push({
      key: `meta-${t}`,
      label: `分类：${t}`,
      remove: () => patch({ meta_tags: filters.metaTags.filter(x => x !== t) }),
    })
  }
  for (const t of filters.exclude) {
    chips.push({
      key: `ex-${t}`,
      label: `排除：${t}`,
      remove: () => patch({ exclude: filters.exclude.filter(x => x !== t) }),
    })
  }
  if (filters.dateFrom || filters.dateTo) {
    chips.push({
      key: 'date',
      label: `${filters.dateFrom ?? '…'} → ${filters.dateTo ?? '…'}`,
      remove: () => patch({ date_from: undefined, date_to: undefined }),
    })
  }
  if (filters.scoreMin) {
    chips.push({
      key: 'score',
      label: `评分 ≥ ${filters.scoreMin}`,
      remove: () => patch({ score_min: undefined }),
    })
  }
  if (filters.ratingMin) {
    chips.push({
      key: 'rating',
      label: `评分人数 ≥ ${filters.ratingMin}`,
      remove: () => patch({ rating_min: undefined }),
    })
  }
  if (filters.pctMin) {
    chips.push({
      key: 'pct',
      label: `标签占比 ≥ ${filters.pctMin}%`,
      remove: () => patch({ pct_min: undefined }),
    })
  }
  if (filters.rankMin || filters.rankMax) {
    chips.push({
      key: 'rank',
      label: `排名 ${filters.rankMin ?? '不限'}–${filters.rankMax ?? '不限'}`,
      remove: () => patch({ rank_min: undefined, rank_max: undefined }),
    })
  }
  if (filters.nsfw !== 'hide') {
    chips.push({
      key: 'nsfw',
      label: NSFW_LABEL[filters.nsfw] ?? filters.nsfw,
      remove: () => patch({ nsfw: undefined }),
    })
  }

  if (chips.length === 0) return null

  return (
    <div className="flex flex-wrap items-center gap-1.5 max-sm:flex-nowrap max-sm:overflow-x-auto max-sm:pb-0.5 scrollbar-none">
      <span className="shrink-0 text-xs text-muted-foreground">已选</span>
      {chips.map(c => (
        <RemovableChip
          key={c.key}
          label={c.label}
          removeLabel={`移除筛选 ${c.label}`}
          maxWidth="max-w-48"
          onRemove={c.remove}
        />
      ))}
      <Button type="button" variant="ghost" size="xs" onClick={onResetAll}>
        清空全部
      </Button>
    </div>
  )
}
