// 交互探针：逐个验证筛选侧栏的复杂控件（改前端后先跑它）。
// 用法：node probe.mjs   （BASE=http://127.0.0.1:8787）
import { chromium } from 'playwright'

const BASE = process.env.BASE ?? 'http://127.0.0.1:8787'
const browser = await chromium.launch()
const page = await (await browser.newContext({ viewport: { width: 1440, height: 1000 } })).newPage()

let ok = 0
let bad = 0
async function step(name, fn) {
  try {
    await fn()
    ok++
    console.log(`✓ ${name}`)
  } catch (e) {
    bad++
    console.log(`✗ ${name}\n  ${e.message}`)
  }
}
const assert = (c, m) => { if (!c) throw new Error(m) }

await page.goto(BASE + '/browse?types=2', { waitUntil: 'domcontentloaded' })
await page.waitForSelector('[data-subject-id]')

await step('标签：搜索 → 点选 → 生成标签 → 移除', async () => {
  const trigger = page.getByRole('combobox', { name: '标签', exact: true })
  await trigger.click()
  const input = page.getByPlaceholder('搜索标签…')
  await input.fill('原创')
  await page.waitForTimeout(600)
  const opt = page.getByRole('option', { name: /原创/ }).first()
  await opt.waitFor({ timeout: 8000 })
  await opt.click()
  await page.waitForTimeout(500)
  assert(decodeURIComponent(page.url()).includes('tags='), '未写入 tags')
  assert((await trigger.innerText()).includes('原创'), '触发器未显示已选标签')

  // 手动创建（选择后弹出层保持打开，直接继续输入）
  if (!(await input.isVisible())) await trigger.click()
  await input.fill('不存在的自定义标签xyz')
  await page.waitForTimeout(500)
  const create = page.getByRole('option', { name: /使用「不存在的自定义标签xyz」/ })
  await create.waitFor({ timeout: 5000 })
  await create.click()
  await page.waitForTimeout(500)
  assert(decodeURIComponent(page.url()).includes('不存在的自定义标签xyz'), '手动创建未写入 URL')

  // 移除（点击芯片）
  await trigger.click()
  await page.keyboard.press('Escape')
})

await step('占比门槛输入', async () => {
  const input = page.locator('aside input').nth(0)
  await input.fill('5')
  await input.blur()
  await page.waitForTimeout(500)
  assert(page.url().includes('pct_min=5'), `URL 无 pct_min: ${page.url()}`)
})

await step('分类标签：打开 → 选择', async () => {
  await page.getByRole('combobox', { name: '分类标签', exact: true }).click()
  const opt = page.getByRole('option').first()
  await opt.waitFor({ timeout: 8000 })
  const name = (await opt.innerText()).trim()
  await opt.click()
  await page.waitForTimeout(500)
  assert(decodeURIComponent(page.url()).includes('meta_tags='), '未写入 meta_tags')
  console.log('   选中分类标签：', name)
  await page.keyboard.press('Escape')
})

await step('日期范围：打开日历 → 选区间', async () => {
  await page.getByRole('button', { name: /选择开播/ }).click()
  await page.waitForTimeout(400)
  const days = page.locator('[data-day]')
  const n = await days.count()
  assert(n > 10, '日历没有日期按钮')
  await days.nth(5).click()
  await days.nth(12).click()
  await page.waitForTimeout(500)
  assert(page.url().includes('date_from='), `URL 无 date_from: ${page.url()}`)
  await page.keyboard.press('Escape')
})

await step('重置筛选', async () => {
  await page.getByRole('button', { name: /重置/ }).click()
  await page.waitForTimeout(500)
  assert(!page.url().includes('tags='), '重置未清空标签')
  assert(!page.url().includes('pct_min='), '重置未清空占比')
})

await browser.close()
console.log(`\n通过 ${ok} / ${ok + bad}`)
if (bad > 0) process.exit(1)
