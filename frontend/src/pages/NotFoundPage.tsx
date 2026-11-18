import { Link } from 'react-router-dom'
import { Button } from '@/components/ui/button'

export default function NotFoundPage() {
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center text-center">
      <p className="num text-sm text-muted-foreground">404</p>
      <h1 className="mt-2 text-2xl font-semibold">这一页不在索引里</h1>
      <p className="mt-2 max-w-md text-sm leading-relaxed text-muted-foreground">
        链接可能已经失效，或条目在最近一次清理中被移除。回到索引重新查找。
      </p>
      <div className="mt-5 flex gap-2">
        <Button asChild variant="outline" size="sm">
          <Link to="/browse">浏览索引</Link>
        </Button>
        <Button asChild size="sm">
          <Link to="/">返回总览</Link>
        </Button>
      </div>
    </div>
  )
}
