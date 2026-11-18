// 浏览器门禁：无横向溢出 / 结果网格列数 / 选项卡不滚动。
import { chromium } from 'playwright'

const BASE = process.env.BASE ?? 'http://127.0.0.1:5173'
const browser = await chromium.launch()
const errors = []

async function open(path, width, height) {
  const ctx = await browser.newContext({ viewport: { width, height } })
  await ctx.addInitScript(() => localStorage.setItem('anipick.endpoint.v1', 'mirror-anibt'))
  const page = await ctx.newPage()
  await page.goto(BASE + path, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(3500)
  return { ctx, page }
}

for (const [width, height] of [[1440, 900], [1024, 768], [768, 1024], [390, 844]]) {
  const { ctx, page } = await open('/', width, height)
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1)
  if (overflow) errors.push(`home ${width}: horizontal overflow`)

  const cols = await page.evaluate(() => {
    const el = document.querySelector('[data-testid="subject-grid"]')
    return el ? getComputedStyle(el).gridTemplateColumns.split(' ').filter(Boolean).length : 0
  })
  const expected = width >= 1024 ? 6 : width >= 640 ? 3 : 2
  if (cols && cols !== expected) errors.push(`home ${width}: grid ${cols}, expected ${expected}`)

  const typeCols = await page.evaluate(() => {
    const el = document.querySelector('[data-testid="type-grid"]')
    return el ? getComputedStyle(el).gridTemplateColumns.split(' ').filter(Boolean).length : 0
  })
  const typeExpected = width >= 1024 ? 5 : width >= 640 ? 3 : 2
  if (typeCols && typeCols !== typeExpected) errors.push(`home ${width}: type grid ${typeCols}, expected ${typeExpected}`)

  if (width === 1440) {
    const dups = await page.evaluate(() => {
      const seen = new Map()
      const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
      while (walker.nextNode()) {
        const el = walker.currentNode.parentElement
        if (!el || el.closest('script,style,noscript')) continue
        const text = (walker.currentNode.textContent ?? '').trim().replace(/\s+/g, ' ')
        if (text.length < 12) continue
        if (/^[\d\s年月日/:.,%-]+$/.test(text)) continue
        seen.set(text, (seen.get(text) ?? 0) + 1)
      }
      return [...seen.entries()].filter(([, n]) => n > 1).map(([t]) => t)
    })
    if (dups.length) errors.push(`home duplicate copy: ${dups.slice(0, 3).join(' | ')}`)
  }
  await ctx.close()
}

// 索引页：5/4/3/2 列，视图切换组必须在结果工具条最右。
for (const [width, height] of [[1440, 900], [1024, 768], [768, 1024], [390, 844]]) {
  const { ctx, page } = await open('/browse?types=2', width, height)
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1)
  if (overflow) errors.push(`browse ${width}: horizontal overflow`)

  const cols = await page.evaluate(() => {
    const el = document.querySelector('[data-testid="subject-grid"]')
    return el ? getComputedStyle(el).gridTemplateColumns.split(' ').filter(Boolean).length : 0
  })
  const expected = width >= 1280 ? 5 : width >= 1024 ? 4 : width >= 768 ? 3 : 2
  if (cols && cols !== expected) errors.push(`browse ${width}: grid ${cols}, expected ${expected}`)

  const right = await page.evaluate(() => {
    const group = document.querySelector('[aria-label="视图切换"]')
    const bar = group?.closest('.sticky')
    if (!group || !bar) return null
    return { group: group.getBoundingClientRect().right, bar: bar.getBoundingClientRect().right }
  })
  if (right && right.group < right.bar - 32) errors.push(`browse ${width}: view group not right (${Math.round(right.group)}/${Math.round(right.bar)})`)
  await ctx.close()
}

await browser.close()
if (errors.length) {
  console.error(errors.join('\n'))
  process.exit(1)
}
console.log('ui audit ok')
