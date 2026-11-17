import * as React from 'react'
import { CalendarRange } from 'lucide-react'
import type { DateRange } from 'react-day-picker'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

const fmt = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

// 日期范围：官方「Date Picker」模式（Popover + Calendar range）
// 选中完整区间或清空时写回；只选了起点时保留在本地草稿
export function DateRangePicker({
  from,
  to,
  onChange,
  className,
  ariaLabel,
}: {
  from?: string
  to?: string
  onChange: (from?: string, to?: string) => void
  className?: string
  ariaLabel?: string
}) {
  const [open, setOpen] = React.useState(false)
  const [draft, setDraft] = React.useState<DateRange | undefined>(() => ({
    from: from ? new Date(from) : undefined,
    to: to ? new Date(to) : undefined,
  }))

  React.useEffect(() => {
    setDraft({
      from: from ? new Date(from) : undefined,
      to: to ? new Date(to) : undefined,
    })
  }, [from, to])

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          aria-label={ariaLabel}
          className={cn(
            'h-9 w-full justify-start text-left font-normal',
            !from && !to && 'text-muted-foreground',
            className,
          )}
        >
          <CalendarRange size={14} className="opacity-60" />
          {from || to ? (
            <span className="truncate tabular-nums">
              {from ?? '…'} → {to ?? '…'}
            </span>
          ) : (
            <span>选择开播 / 发售日期范围</span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align="start">
        <Calendar
          mode="range"
          selected={draft}
          onSelect={r => {
            setDraft(r)
            if (!r?.from) {
              onChange(undefined, undefined)
              return
            }
            if (r.from && r.to) onChange(fmt(r.from), fmt(r.to))
          }}
          numberOfMonths={2}
          defaultMonth={draft?.from}
        />
      </PopoverContent>
    </Popover>
  )
}
