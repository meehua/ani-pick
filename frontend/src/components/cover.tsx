import * as React from 'react'
import { cn } from '@/lib/utils'
import { ImageOff } from 'lucide-react'
import { img, imgOriginal } from '../api/client'

// 封面：镜像图床 → 官方原图 → 占位。圆角由调用方通过 className 控制。
export function Cover({
  src,
  alt,
  className,
  imgClassName,
  fallback = '无封面',
  priority = false,
}: {
  src?: string
  alt: string
  className?: string
  imgClassName?: string
  fallback?: string
  priority?: boolean
}) {
  const [failed, setFailed] = React.useState(false)
  const [loaded, setLoaded] = React.useState(false)
  const imgRef = React.useRef<HTMLImageElement>(null)

  React.useEffect(() => {
    setFailed(false)
    setLoaded(false)
    // 缓存图/懒加载图可能在 effect 之前就 complete，不会再有 load 事件
    const el = imgRef.current
    if (el?.complete && el.naturalWidth > 0) setLoaded(true)
  }, [src])

  if (!src || failed) {
    return (
      <div
        className={cn(
          'cover-frame flex flex-col items-center justify-center gap-1 border text-muted-foreground',
          className,
        )}
        role="img"
        aria-label={alt}
      >
        <ImageOff size={14} aria-hidden />
        <span className="text-xs leading-none">{fallback}</span>
      </div>
    )
  }

  return (
    <div className={cn('cover-frame', className, !loaded && 'animate-pulse bg-muted')}>
      <img
        ref={imgRef}
        referrerPolicy="no-referrer"
        src={img(src)}
        alt={alt}
        loading={priority ? 'eager' : 'lazy'}
        fetchPriority={priority ? 'high' : 'auto'}
        decoding="async"
        className={cn(
          imgClassName,
          'transition-opacity duration-300',
          loaded ? 'opacity-100' : 'opacity-0',
        )}
        onLoad={() => setLoaded(true)}
        onError={e => {
          const original = imgOriginal(src)
          if (e.currentTarget.src !== original) e.currentTarget.src = original
          else setFailed(true)
        }}
      />
    </div>
  )
}
