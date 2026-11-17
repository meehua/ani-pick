import { Link } from 'react-router-dom'
import { Compass } from 'lucide-react'
import { Button } from '@/components/ui/button'

export default function NotFoundPage() {
  return (
    <div className="flex flex-col items-center py-24 text-center">
      <Compass size={30} strokeWidth={1.5} className="mb-4 text-muted-foreground" />
      <h1 className="text-xl font-semibold">页面不存在</h1>
      <p className="mt-1 text-sm text-muted-foreground">地址可能写错了，或者条目已被移除。</p>
      <Button asChild size="sm" className="mt-4">
        <Link to="/">回到首页</Link>
      </Button>
    </div>
  )
}
