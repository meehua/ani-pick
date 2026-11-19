import { execSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'

// 生产构建的版本标识（version-commit）与 Go 侧 ldflags（main.version/main.commit）
// 同源，注入 __APP_BUILD__ 供页脚展示与前后端一致性检测。
// Docker 构建经 ANIPICK_VERSION/ANIPICK_COMMIT 注入（见 Dockerfile）；
// 本地构建回退 package.json version + git 短哈希，取不到哈希时退化为仅版本号。
// 开发模式（vite dev）标识固定为 dev，不参与真实版本语义。
function resolveVersion(): string {
  if (process.env.ANIPICK_VERSION) return process.env.ANIPICK_VERSION
  const pkg = JSON.parse(readFileSync(path.resolve(__dirname, 'package.json'), 'utf8'))
  return pkg.version as string
}

function resolveCommit(): string | null {
  const env = process.env.ANIPICK_COMMIT
  if (env !== undefined) return env !== 'none' && env !== '' ? env : null
  try {
    return (
      execSync('git rev-parse --short HEAD', { stdio: ['ignore', 'pipe', 'ignore'] })
        .toString()
        .trim() || null
    )
  } catch {
    return null
  }
}

function resolveAppBuild(mode: string): string {
  if (mode === 'development') return 'dev'
  const commit = resolveCommit()
  return commit ? `${resolveVersion()}-${commit}` : resolveVersion()
}

// 开发模式下 /api 代理到 Go 后端（go run . 默认 :8787）
export default defineConfig(({ mode }) => ({
  define: {
    __APP_BUILD__: JSON.stringify(resolveAppBuild(mode)),
  },
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8787',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    chunkSizeWarningLimit: 1200,
  },
}))
