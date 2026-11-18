// 依赖方向门禁：pages → components → ui，api/lib 为叶子。
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

// fileURLToPath 在 Windows/UNC 下也正确（URL.pathname 会丢掉主机段导致路径拼错）。
const root = fileURLToPath(new URL('../src/', import.meta.url))
const files = []
function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p)
    else if (/\.(ts|tsx)$/.test(name)) files.push(p)
  }
}
walk(root)

const errors = []
for (const file of files) {
  const rel = relative(root, file)
  const text = readFileSync(file, 'utf8')
  const imports = [...text.matchAll(/from\s+['"]([^'"]+)['"]/g)].map(m => m[1])
  for (const raw of imports) {
    const spec = raw.startsWith('@/') ? raw.slice(2) : raw
    if (!spec.startsWith('api/') && !spec.startsWith('lib/') && !spec.startsWith('components/') && !spec.startsWith('pages/')) continue
    if (rel.startsWith('components/ui/')) {
      if (spec.startsWith('api/') || spec.startsWith('pages/') || spec.startsWith('components/') && !spec.startsWith('components/ui/')) {
        errors.push(`${rel} -> ${raw}（ui 只能依赖 ui/lib）`)
      }
    }
    if (rel.startsWith('api/') || rel.startsWith('lib/')) {
      if (spec.startsWith('components/') || spec.startsWith('pages/')) {
        errors.push(`${rel} -> ${raw}（api/lib 不能依赖上层）`)
      }
    }
    if (rel.startsWith('components/')) {
      if (spec.startsWith('pages/')) errors.push(`${rel} -> ${raw}（components 不能依赖 pages）`)
    }
  }
}

if (errors.length) {
  console.error(errors.join('\n'))
  process.exit(1)
}
console.log(`layer check ok: ${files.length} files`)
