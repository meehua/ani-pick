// 交互探针：逐个验证筛选弹窗与标签检索（改前端后先跑它）。
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

await step('标签：搜索 → 点选 → 手动新增', async () => {
  await page.getByRole('combobox', { name: '标签', exact: true }).click()
  const input = page.getByPlaceholder('搜索标签…')
  await input.fill('原创')
  await page.waitForTimeout(700)
  const opt = page.getByRole('option', { name: /原创/ }).first()
  await opt.waitFor({ timeout: 8000 })
  await opt.click()
  await page.waitForTimeout(500)
  assert(decodeURIComponent(page.url()).includes('tags='), '未写入 tags')
  const trigger = await page.getByRole('combobox', { name: '标签', exact: true }).textContent()
  assert((trigger ?? '').includes('原创'), '已选标签未显示在选择器内')

  await input.fill('不存在的自定义标签xyz')
  await page.waitForTimeout(500)
  const create = page.getByRole('option', { name: /新增「不存在的自定义标签xyz」/ })
  await create.waitFor({ timeout: 5000 })
  await create.click()
  await page.waitForTimeout(400)
  assert(decodeURIComponent(page.url()).includes('不存在的自定义标签xyz'), '手动创建未写入 URL')
  await page.keyboard.press('Escape')
})

await step('侧栏：年代 / 评分 / 占比 / 分类', async () => {
  await page.getByRole('button', { name: '2020s' }).click()
  await page.waitForTimeout(300)
  assert(page.url().includes('date_from=2020-01-01'), `URL 无 date_from: ${page.url()}`)

  await page.getByRole('radio', { name: '8+' }).click()
  await page.waitForTimeout(300)
  assert(page.url().includes('score_min=8'), `URL 无 score_min: ${page.url()}`)

  const pct = page.locator('#pct-min')
  await pct.fill('5')
  await page.waitForTimeout(400)
  assert(page.url().includes('pct_min=5'), `URL 无 pct_min: ${page.url()}`)

  await page.getByRole('combobox', { name: '分类标签', exact: true }).click()
  const opt = page.locator('[data-slot="popover-content"] [role="option"]').first()
  await opt.waitFor({ timeout: 8000 })
  await opt.click()
  await page.waitForTimeout(400)
  assert(decodeURIComponent(page.url()).includes('meta_tags='), '未写入 meta_tags')
  await page.keyboard.press('Escape')

  await page.getByLabel('起始', { exact: true }).fill('2000-01-01')
  await page.waitForTimeout(300)
  await page.getByLabel('结束', { exact: true }).fill('2010-12-31')
  await page.waitForTimeout(500)
  assert(page.url().includes('date_from=2000-01-01'), `URL 未更新 date_from: ${page.url()}`)
  assert(page.url().includes('date_to=2010-12-31'), `URL 无 date_to: ${page.url()}`)
})

await step('重置筛选', async () => {
  await page.getByRole('button', { name: '重置', exact: true }).first().click()
  await page.waitForTimeout(500)
  assert(!page.url().includes('tags='), '重置未清空标签')
  assert(!page.url().includes('pct_min='), '重置未清空占比')
  assert(!page.url().includes('date_from='), '重置未清空日期')
  assert(!page.url().includes('score_min='), '重置未清空评分')
})

await browser.close()
console.log(`\n通过 ${ok} / ${ok + bad}`)
if (bad > 0) process.exit(1)
