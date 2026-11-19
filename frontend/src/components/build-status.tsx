import { useState } from 'react'
import { useMeta } from '@/lib/use-meta'
import { Button } from '@/components/ui/button'
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog'

// 展示用：正式版本号前缀 v（如 v1.0.0-4f2a9c1），开发标识原样显示。
function fmtBuild(s: string): string {
  return s.startsWith('dev') ? s : `v${s}`
}

// 页脚版本标识 + 前后端版本一致性检测。
// 服务端标识取 meta.build（version-commit，与构建注入的 __APP_BUILD__ 同源）；
// 旧后端 meta 无 build 字段时只展示 version，不做不一致判定。
// 不一致：生产弹窗引导刷新；开发环境（vite dev）不弹窗，仅在页脚版本处提示。
export function BuildStatus() {
  const meta = useMeta()
  const [dismissed, setDismissed] = useState(false)

  const serverBuild = meta?.build ?? null
  const display = meta ? meta.build || meta.version : null
  // 开发实例（本地裸构建标识为 dev）不参与判定
  const mismatch = !!serverBuild && serverBuild !== 'dev' && serverBuild !== __APP_BUILD__
  const dev = import.meta.env.DEV

  return (
    <>
      {display && (
        mismatch && dev ? (
          <span
            className="text-xs text-amber-600 dark:text-amber-400"
            title={`页面 ${fmtBuild(__APP_BUILD__)} · 服务端 ${fmtBuild(serverBuild)}`}
          >
            版本不一致（{fmtBuild(__APP_BUILD__)} / {fmtBuild(serverBuild)}）
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">{fmtBuild(display)}</span>
        )
      )}
      {mismatch && !dev && !dismissed && (
        <Dialog open onOpenChange={open => { if (!open) setDismissed(true) }}>
          <DialogContent className="sm:max-w-sm">
            <DialogHeader>
              <DialogTitle>页面版本与服务端不一致</DialogTitle>
              <DialogDescription>
                当前页面为 {fmtBuild(__APP_BUILD__)}，服务端为 {fmtBuild(serverBuild)}，通常是浏览器或 CDN 缓存了旧页面。刷新后即可加载最新版本。
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setDismissed(true)}>稍后</Button>
              <Button onClick={() => location.reload()}>立即刷新</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}
