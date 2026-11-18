import * as React from 'react'
import { UserRound } from 'lucide-react'
import { cn } from '@/lib/utils'
import { img } from '../api/client'

// 有头像显示头像；没有/加载失败一律回退到用户图标，不显示昵称首字母。
export function UserAvatar({
  src,
  alt,
  className,
}: {
  src?: string
  alt: string
  className?: string
}) {
  const [failed, setFailed] = React.useState(false)
  React.useEffect(() => {
    setFailed(false)
  }, [src])

  const url = src ? img(src) : ''

  return (
    <span
      className={cn(
        'grid size-7 shrink-0 place-items-center overflow-hidden rounded-full bg-secondary text-muted-foreground',
        className,
      )}
    >
      {url && !failed ? (
        <img
          src={url}
          alt={alt}
          referrerPolicy="no-referrer"
          className="size-full object-cover"
          onError={() => setFailed(true)}
        />
      ) : (
        <UserRound size={14} aria-hidden />
      )}
    </span>
  )
}
