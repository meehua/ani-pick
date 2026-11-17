// 真实服务冒烟：需要先启动 ./anipick 且索引中已有数据。
const base = process.env.SMOKE_BASE ?? 'http://127.0.0.1:8787'
const password = process.env.SMOKE_ADMIN_PASSWORD ?? 'anipick'

let pass = 0
let fail = 0

async function check(name, fn) {
  try {
    await fn()
    pass++
    console.log(`  ✓ ${name}`)
  } catch (e) {
    fail++
    console.log(`  ✗ ${name}\n    ${e.message}`)
  }
}

function assert(cond, msg) {
  if (!cond) throw new Error(msg)
}

async function json(path, init) {
  const res = await fetch(base + path, init)
  const text = await res.text()
  let body
  try { body = JSON.parse(text) } catch { throw new Error(`${path} 非 JSON: ${text.slice(0, 120)}`) }
  return { res, body }
}

console.log(`冒烟测试 ${base}`)

await check('健康检查', async () => {
  const { res, body } = await json('/api/health')
  assert(res.status === 200 && body.ok === true, `status=${res.status}`)
})

let stats
await check('站点元信息', async () => {
  const { res, body } = await json('/api/meta')
  assert(res.status === 200, `status=${res.status}`)
  assert(typeof body.stats.total === 'number', '缺少 stats.total')
  stats = body.stats
})

await check('索引非空', async () => {
  assert(stats.total > 0, '索引为空，请先在管理后台启动抓取')
})

let first
await check('本地检索 + 排序', async () => {
  const { res, body } = await json('/api/subjects?types=2&page_size=5&sort=score&order=desc')
  assert(res.status === 200, `status=${res.status}`)
  assert(body.items.length > 0, '没有结果')
  assert(body.total >= body.items.length, 'total 异常')
  first = body.items[0]
})

await check('条目详情（索引）', async () => {
  const { res, body } = await json(`/api/subjects/${first.id}`)
  assert(res.status === 200, `status=${res.status}`)
  assert(body.id === first.id, 'id 不匹配')
  assert(Array.isArray(body.tags) && body.tags.length > 0, '缺少标签')
  assert(body.tags[0].pct > 0 && body.tags[0].pct <= 100, `占比异常: ${body.tags[0].pct}`)
})

await check('标签候选', async () => {
  const { res, body } = await json('/api/tags?types=2&limit=5')
  assert(res.status === 200 && body.items.length > 0, '没有标签候选')
})

let tag
await check('按标签筛选（AND + 占比门槛）', async () => {
  const { body } = await json('/api/tags?types=2&limit=1')
  tag = body.items[0].name
  const all = await json(`/api/subjects?types=2&page_size=5&tags=${encodeURIComponent(tag)}`)
  const pct = await json(`/api/subjects?types=2&page_size=5&tags=${encodeURIComponent(tag)}&pct_min=1`)
  assert(all.body.total > 0, `标签 ${tag} 没有结果`)
  assert(pct.body.total <= all.body.total, '占比门槛未生效')
})

await check('排除标签', async () => {
  const { res, body } = await json(`/api/subjects?types=2&page_size=5&exclude=${encodeURIComponent(tag)}`)
  assert(res.status === 200, `status=${res.status}`)
  assert(typeof body.total === 'number', '缺少 total')
})

await check('分类标签候选', async () => {
  const { res, body } = await json('/api/meta-tags?type=2&limit=5')
  assert(res.status === 200 && body.items.length > 0, '没有分类标签')
})

await check('不提供任何 API/图片中继', async () => {
  const bgm = await fetch(base + '/api/bgm/v0/subjects/2')
  assert(bgm.status === 404, `bgm 中继不应存在: ${bgm.status}`)
  const img = await fetch(base + '/api/img?u=' + encodeURIComponent('https://lain.bgm.tv/a.png'))
  assert(img.status === 404, `图片代理不应存在: ${img.status}`)
})

let cookie = ''
await check('管理员登录失败', async () => {
  const { res } = await json('/api/admin/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: 'wrong-password' }),
  })
  assert(res.status === 401, `status=${res.status}`)
})

await check('管理员登录成功', async () => {
  const res = await fetch(base + '/api/admin/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  assert(res.status === 200, `status=${res.status}`)
  const set = res.headers.getSetCookie?.() ?? []
  cookie = (set[0] ?? '').split(';')[0]
  assert(cookie.startsWith('anipick_admin='), '未返回会话 Cookie')
})

await check('管理概览', async () => {
  const { res, body } = await json('/api/admin/overview', { headers: { Cookie: cookie } })
  assert(res.status === 200, `status=${res.status}`)
  assert(body.stats.total > 0, 'stats.total 异常')
  assert(Array.isArray(body.runs), '缺少 runs')
  assert(typeof body.stale === 'number', '缺少 stale')
})

await check('读取设置（敏感值脱敏）', async () => {
  const { res, body } = await json('/api/admin/settings', { headers: { Cookie: cookie } })
  assert(res.status === 200, `status=${res.status}`)
  assert(body.crawl && body.crawl.page_size > 0, 'crawl 设置异常')
  assert(!JSON.stringify(body.bgm.token ?? '').includes('eyJ'), '疑似泄露完整 Token')
})

await check('未登录访问管理接口被拒绝', async () => {
  const { res } = await json('/api/admin/overview')
  assert(res.status === 401, `status=${res.status}`)
})

await check('抓取出口诊断（DoH/ECH）', async () => {
  const { res, body } = await json('/api/admin/network', { headers: { Cookie: cookie } })
  assert(res.status === 200, `status=${res.status}`)
  assert(body.doh === true, 'DoH 未启用')
  assert(body.diagnostics && Array.isArray(body.diagnostics.addresses) && body.diagnostics.addresses.length > 0,
    'DoH 解析结果为空')
})

console.log(`\n通过 ${pass} / ${pass + fail}`)
if (fail > 0) process.exit(1)
