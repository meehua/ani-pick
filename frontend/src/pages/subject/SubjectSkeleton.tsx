import { Skeleton } from '@/components/ui/skeleton'
import { DarkStage } from '@/components/dark-stage'

// 与正式内容共用同一骨架：深色场景 hero（封面/标题/数据条）→ 全宽标签区 → 左 tabs + 右资料栏。
export function SubjectSkeleton() {
  return (
    <div className="space-y-5" aria-busy="true" aria-label="条目加载中">
      <Skeleton className="mb-2 h-4 w-16" />

      <DarkStage className="p-5 sm:p-7">
        <div className="grid gap-x-6 gap-y-4 max-sm:grid-cols-[110px_minmax(0,1fr)] sm:grid-cols-[190px_minmax(0,1fr)] lg:grid-cols-[190px_minmax(0,1fr)_300px] lg:gap-x-8">
          <Skeleton className="aspect-[5/7] w-[110px] rounded-lg bg-white/10 sm:w-full sm:rounded-xl" />
          <div className="min-w-0 space-y-3">
            <div className="flex gap-1.5">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-5 w-14 bg-white/10" />
              ))}
            </div>
            <Skeleton className="h-9 w-3/4 bg-white/10" />
            <Skeleton className="h-4 w-1/2 bg-white/10" />
          </div>
          <div className="min-w-0 space-y-3 max-sm:col-span-2">
            <div className="flex items-end gap-8">
              {Array.from({ length: 4 }).map((_, i) => (
                <div key={i} className="space-y-1.5">
                  <Skeleton className="h-3 w-12 bg-white/10" />
                  <Skeleton className="h-8 w-16 bg-white/10" />
                </div>
              ))}
            </div>
            <Skeleton className="h-4 w-72 bg-white/10" />
            <div className="flex gap-2">
              <Skeleton className="h-8 w-36 rounded-md bg-white/10" />
              <Skeleton className="h-8 w-40 rounded-md bg-white/10" />
            </div>
            <Skeleton className="h-20 w-full bg-white/10" />
          </div>
        </div>

        {/* 磨砂面板占位：lg+ 右列；sm–md 底部两卡横排；窄屏竖排 */}
        <div className="grid col-span-full grid-cols-1 gap-3 sm:grid-cols-2 lg:col-span-1 lg:col-start-3 lg:row-span-2 lg:row-start-1 lg:grid-cols-1">
          <div className="rounded-xl bg-white/[0.07] p-3 ring-1 ring-white/15">
            <Skeleton className="mb-2 h-4 w-16 bg-white/10" />
            <Skeleton className="mx-auto size-[132px] rounded-full bg-white/10" />
          </div>
          <div className="rounded-xl bg-white/[0.07] p-3 ring-1 ring-white/15">
            <Skeleton className="mb-2 h-4 w-16 bg-white/10" />
            <div className="flex items-end gap-1.5 px-5">
              {Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className="w-12 shrink-0 space-y-1">
                  <Skeleton className="w-full rounded-t bg-white/10" style={{ height: 20 + ((i * 17) % 44) }} />
                  <Skeleton className="h-3 w-full bg-white/10" />
                </div>
              ))}
            </div>
          </div>
        </div>
      </DarkStage>

      <div className="space-y-2">
        <Skeleton className="h-4 w-12" />
        <div className="flex flex-wrap gap-1">
          {Array.from({ length: 14 }).map((_, i) => (
            <Skeleton key={i} className="h-5 w-20 rounded-md border border-border" />
          ))}
        </div>
      </div>

      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="min-w-0 space-y-4">
          <Skeleton className="h-10 w-full rounded-none border-b border-border" />
          <div className="grid grid-cols-2 gap-x-6 sm:grid-cols-3">
            {Array.from({ length: 9 }).map((_, i) => (
              <div key={i} className="flex items-center gap-2.5 py-1">
                <Skeleton className="size-9 shrink-0 rounded-md" />
                <div className="min-w-0 flex-1 space-y-1">
                  <Skeleton className="h-3.5 w-3/4" />
                  <Skeleton className="h-3 w-1/2" />
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="space-y-4">
          <Skeleton className="h-44 rounded-xl" />
          <Skeleton className="h-80 rounded-xl" />
        </div>
      </div>
    </div>
  )
}
