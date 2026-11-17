// AniPick 端到端测试：需要后端已启动且索引非空。
// 用法：node e2e.mjs   （BASE=http://127.0.0.1:8787）
import { chromium } from 'playwright'

const BASE = process.env.BASE ?? 'http://127.0.0.1:8787'
const ADMIN_PASSWORD = process.env.ADMIN_PASSWORD ?? 'anipick'
const SHOTS = '/tmp/anipick_e2e'

let pass = 0
let fail = 0
const failures = []

async function check(name, fn) {
  try {
    await fn()
    pass++
    console.log(`✓ ${name}`)
  } catch (e) {
    fail++
    failures.push(`${name}: ${e.message}`)
    console.log(`✗ ${name}\n  ${e.message}`)
  }
}

function assert(cond, msg) {
  if (!cond) throw new Error(msg)
}

// 预先取一个真实存在的标签，避免测试依赖写死数据
const tagRes = await fetch(`${BASE}/api/tags?types=2&limit=1`).then(r => r.json())
const knownTag = tagRes.items?.[0]?.name
if (!knownTag) {
  console.error('索引里没有标签，请先启动抓取再运行 e2e')
  process.exit(1)
}

const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1440, height: 960 } })
const page = await ctx.newPage()
const consoleErrors = []
page.on('console', m => {
  if (m.type() === 'error') consoleErrors.push(m.text())
})
page.on('pageerror', e => consoleErrors.push(String(e)))

await check('首页渲染与索引统计', async () => {
  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('h1')
  const h1 = await page.locator('h1').innerText()
  assert(h1.includes('找到下一部'), `首页标题异常: ${h1}`)
  assert((await page.title()).includes('AniPick'), '标题不是 AniPick')
  await page.screenshot({ path: `${SHOTS}/home.png`, fullPage: true })
})

await check('浏览页展示结果', async () => {
  await page.goto(BASE + '/browse?types=2&sort=score', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('[data-subject-id]')
  const n = await page.locator('[data-subject-id]').count()
  assert(n > 0, '没有条目卡片')
  assert((await page.locator('body').innerText()).includes('条结果'), '缺少结果计数')
})

await check('类型筛选写入 URL', async () => {
  await page.goto(BASE + '/browse', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('[data-subject-id]')
  await page.getByRole('button', { name: '动画', exact: true }).click()
  await page.waitForTimeout(500)
  assert(page.url().includes('types=2'), `URL 不含 types=2: ${page.url()}`)
})

await check('标签可搜索、可点选', async () => {
  await page.goto(BASE + '/browse?types=2', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('[data-subject-id]')
  await page.getByRole('combobox', { name: '标签', exact: true }).click()
  const input = page.getByPlaceholder('搜索标签…')
  await input.waitFor({ state: 'visible', timeout: 5000 })
  await input.fill(knownTag)
  const option = page.getByRole('option', { name: new RegExp(knownTag) }).first()
  await option.waitFor({ state: 'visible', timeout: 8000 })
  await option.click()
  await page.waitForTimeout(600)
  assert(decodeURIComponent(page.url()).includes(`tags=${knownTag}`), `URL 未写入标签: ${page.url()}`)
  const chips = await page.locator('span', { hasText: knownTag }).count()
  assert(chips > 0, '未显示已选标签')
  await page.screenshot({ path: `${SHOTS}/browse-tag.png`, fullPage: true })
})

await check('排序切换', async () => {
  await page.goto(BASE + '/browse?types=2', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('[data-subject-id]')
  await page.locator('button[role=combobox]').last().click()
  await page.getByRole('option', { name: '评分', exact: true }).click()
  await page.waitForTimeout(500)
  assert(page.url().includes('sort=score'), `URL 未写入 sort=score: ${page.url()}`)
})

await check('分页可用', async () => {
  await page.goto(BASE + '/browse?types=2&sort=id', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('[data-subject-id]')
  const next = page.getByRole('button', { name: '下一页' })
  if (await next.isEnabled()) {
    await next.click()
    await page.waitForTimeout(600)
    assert(page.url().includes('page=2'), `URL 未翻页: ${page.url()}`)
    assert((await page.locator('[data-subject-id]').count()) > 0, '第二页没有条目')
  }
})

await check('条目详情页（本站索引 + 标签占比）', async () => {
  await page.goto(BASE + '/browse?types=2&sort=score', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('[data-subject-id]')
  const href = await page.locator('[data-subject-id]').first().getAttribute('href')
  await page.goto(BASE + href, { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('h1')
  await page.getByRole('tab', { name: /制作人员|角色/ }).waitFor({ timeout: 40000 }).catch(() => {})
  const body = await page.locator('body').innerText()
  assert(body.includes('标签与占比'), '缺少标签占比区块')
  assert(body.includes('Bangumi'), '缺少 Bangumi 说明')
  assert(body.includes('制作人员') || body.includes('角色'), '镜像通道未能加载详情')
  await page.screenshot({ path: `${SHOTS}/subject.png`, fullPage: true })
})

await check('登录框说明本机保存且不泄露', async () => {
  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: '登录', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.waitFor()
  const text = await dialog.innerText()
  assert(text.includes('localStorage'), '未说明 localStorage')
  assert(text.includes('不转发'), '未说明服务端不转发')
  assert(text.includes('公共电脑'), '缺少公共电脑提示')
  // 真实网络下无法校验令牌，这里只验证说明与入口；网络状态由下一项检查覆盖。
  await page.getByLabel('个人 Access Token').isVisible()
  await page.screenshot({ path: `${SHOTS}/login-dialog.png` })
  await page.getByRole('button', { name: '关闭' }).click()
})

await check('镜像通道加载详情且不携带令牌', async () => {
  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  await page.evaluate(() => {
    localStorage.setItem('anipick.credential.v1', JSON.stringify({ token: 'fake-token-for-test', savedAt: Date.now() }))
    localStorage.setItem('anipick.endpoint.v1', 'mirror-anibt')
  })

  const authHeaders = []
  const onReq = (req) => {
    if (req.url().includes('bgmapi.anibt.net')) {
      authHeaders.push(req.headers()['authorization'] ?? '')
    }
  }
  page.on('request', onReq)
  await page.goto(BASE + '/subject/326', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('h1')
  await page.getByRole('tab', { name: /制作人员|角色/ }).waitFor({ timeout: 45000 }).catch(() => {})
  page.off('request', onReq)
  const body = await page.locator('body').innerText()
  assert(body.includes('当前详情来自镜像站'), '缺少镜像来源提示')
  assert(authHeaders.length > 0, '没有走镜像通道')
  assert(authHeaders.every(h => h === ''), '镜像请求携带了令牌')

  await page.evaluate(() => {
    localStorage.removeItem('anipick.credential.v1')
    localStorage.removeItem('anipick.endpoint.v1')
  })
  await page.screenshot({ path: `${SHOTS}/mirror-subject.png`, fullPage: true })
})

await check('自定义镜像可添加可删除', async () => {
  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: '登录', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.waitFor()
  await dialog.getByPlaceholder('API 地址 https://…').fill('https://example.com/api')
  await dialog.getByRole('button', { name: '添加' }).click()
  await page.waitForTimeout(400)
  const saved = await page.evaluate(() => localStorage.getItem('anipick.mirrors.v1') ?? '')
  assert(saved.includes('https://example.com/api'), '镜像未写入本机')

  await dialog.getByRole('button', { name: '删除 https://example.com/api' }).click()
  await page.waitForTimeout(400)
  const after = await page.evaluate(() => localStorage.getItem('anipick.mirrors.v1') ?? '')
  assert(!after.includes('https://example.com/api'), '镜像未删除')
  await page.getByRole('button', { name: '关闭' }).click()
})

await check('令牌只发给官方域名', async () => {
  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  await page.evaluate(() => {
    localStorage.setItem('anipick.credential.v1', JSON.stringify({ token: 'fake-token-for-test', savedAt: Date.now() }))
    localStorage.setItem('anipick.endpoint.v1', 'official-bgm')
  })
  let auth = ''
  await page.route('https://api.bgm.tv/v0/subjects/326', async route => {
    auth = route.request().headers()['authorization'] ?? ''
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: 326, type: 2, name: 'Test', name_cn: 'Test', date: '2004-01-01', platform: 'TV',
        images: {}, summary: '', infobox: [],
        rating: { rank: 1, total: 0, score: 0, count: {} },
        collection: { wish: 0, collect: 0, doing: 0, on_hold: 0, dropped: 0 },
        tags: [],
      }),
    })
  })
  await page.goto(BASE + '/subject/326', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(1500)
  await page.unroute('https://api.bgm.tv/v0/subjects/326')
  assert(auth === 'Bearer fake-token-for-test', `官方请求未携带令牌: ${auth}`)
  await page.evaluate(() => {
    localStorage.removeItem('anipick.credential.v1')
    localStorage.removeItem('anipick.endpoint.v1')
  })
})

await check('管理后台登录与页面', async () => {
  await page.goto(BASE + '/admin/login', { waitUntil: 'domcontentloaded' })
  const main = page.getByRole('main')
  await page.fill('#admin-password', 'wrong-password')
  await main.getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForTimeout(800)
  assert((await page.locator('body').innerText()).includes('密码错误'), '错误密码没有提示')

  await page.fill('#admin-password', ADMIN_PASSWORD)
  await main.getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL('**/admin', { timeout: 8000 })
  await page.waitForSelector('text=索引条目')
  const body = await page.locator('body').innerText()
  for (const t of ['概览', '抓取', '数据', '设置']) assert(body.includes(t), `缺少标签页 ${t}`)
  await page.screenshot({ path: `${SHOTS}/admin.png`, fullPage: true })

  await page.getByRole('tab', { name: '数据' }).click()
  await page.waitForTimeout(800)
  assert((await page.locator('table').count()) > 0, '数据页没有表格')
  await page.getByRole('tab', { name: '设置' }).click()
  await page.waitForTimeout(500)
  assert((await page.locator('body').innerText()).includes('抓取设置'), '设置页缺少抓取设置')
})

await check('主题跟随系统并可手动切换', async () => {
  const darkCtx = await browser.newContext({ viewport: { width: 1200, height: 800 }, colorScheme: 'dark' })
  const darkPage = await darkCtx.newPage()
  await darkPage.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  await darkPage.waitForTimeout(500)
  assert(await darkPage.evaluate(() => document.documentElement.classList.contains('dark')), '未跟随系统深色')
  await darkPage.locator('button[aria-label="切换主题"]').click()
  await darkPage.getByRole('menuitem', { name: '浅色' }).click()
  await darkPage.waitForTimeout(400)
  assert(!(await darkPage.evaluate(() => document.documentElement.classList.contains('dark'))), '手动浅色未生效')
  await darkPage.screenshot({ path: `${SHOTS}/home-light.png`, fullPage: true })
  await darkCtx.close()
})

await check('页面不出现明示受限词', async () => {
  for (const path of ['/', '/browse?types=2', '/admin']) {
    await page.goto(BASE + path, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(400)
    const text = await page.locator('body').innerText()
    assert(!/\bR18\b/i.test(text), `${path} 出现明示词 R18`)
  }
})

const serious = consoleErrors.filter(e =>
  !e.includes('favicon') &&
  !e.includes('401 (Unauthorized)') &&
  !e.includes('net::ERR_') &&
  !e.includes('Failed to load resource'),
)
await check('没有前端运行时报错', async () => {
  assert(serious.length === 0, serious.slice(0, 3).join(' | '))
})

await browser.close()

console.log(`\n通过 ${pass} / ${pass + fail}`)
if (fail > 0) {
  console.log(failures.join('\n'))
  process.exit(1)
}
