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

let extCount = null

await check('健康检查', async () => {
  const { res, body } = await json('/api/health')
  assert(res.status === 200, `status=${res.status}`)
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

// ---------- 多数据源（扩展源未收获时相关断言按空态通过） ----------

await check('来源筛选：非法来源 400', async () => {
  const { res } = await json('/api/subjects?source=evil')
  assert(res.status === 400, `status=${res.status}`)
})

await check('来源筛选：bgm 组内语义（覆盖计数）', async () => {
  const { res, body } = await json('/api/subjects?source=bgm&page_size=5')
  assert(res.status === 200, `status=${res.status}`)
  // 组内语义：total = 含 bgm 记录的作品组数（覆盖口径）。展示行是各组主行，
  // 资料优先序更高的来源（设置可调）会代表整组出现，故不逐行断言 source。
  // 后台持续写入时与 meta 缓存快照间允许小幅漂移，只钉住连续调用的稳定性。
  const again = await json('/api/subjects?source=bgm&page_size=5')
  assert(body.total > 0 && body.total === again.body.total,
    `覆盖计数不稳定: ${body.total} vs ${again.body.total}`)
})

await check('来源筛选：itunes 组内语义（覆盖计数）', async () => {
  const { res, body } = await json('/api/subjects?source=itunes&page_size=5')
  assert(res.status === 200, `status=${res.status}`)
  assert(body.items.length > 0, 'itunes 已抓取但筛选为空')
  const again = await json('/api/subjects?source=itunes&page_size=5')
  assert(body.total === again.body.total,
    `覆盖计数不稳定: ${body.total} vs ${again.body.total}`)
})

await check('来源筛选：扩展源空态可用', async () => {
  const { res, body } = await json('/api/subjects?source=tmdb&page_size=5')
  assert(res.status === 200, `status=${res.status}`)
  assert(body.items.every(s => s.source === 'tmdb'), '结果混入其他来源')
})

await check('meta 统计含来源计数', async () => {
  const { res, body } = await json('/api/meta')
  assert(res.status === 200, `status=${res.status}`)
  assert(body.stats.by_source && typeof body.stats.by_source === 'object', '缺少 stats.by_source')
  extCount = body.stats.by_source
})

await check('meta 下发来源登记表 manifest', async () => {
  const { res, body } = await json('/api/meta')
  assert(res.status === 200, `status=${res.status}`)
  const sources = body.sources
  assert(Array.isArray(sources) && sources.length >= 1, '缺少 sources manifest')
  // 原生索引常驻且详情策略为直连；优先级升序（有效优先级排序）
  const native = sources.find(s => s.native)
  assert(native, 'manifest 缺原生索引来源')
  assert(native.configured, '原生索引应可用')
  assert(sources.every(s => typeof s.id === 'string' && typeof s.label === 'string' && Array.isArray(s.types)),
    'manifest 行结构异常')
  const prios = sources.map(s => s.priority)
  assert(prios.every((p, i) => i === 0 || p >= prios[i - 1]), `manifest 未按优先级排序: ${prios}`)
  // 类型词表来自登记表：每个词条带编码与中文标签；原生标签站点不改名
  assert(sources.some(s => s.types.some(t => t.code === 2 && t.label)), '类型词汇缺动画词条')
  assert(native.types.some(t => t.code === 6 && t.label === '三次元'), 'bgm 原生词表缺「三次元」')
  // 双层词表：kind 层（站点分类）随 manifest 下发，剧集类目含三次元编码
  assert(Array.isArray(body.kinds) && body.kinds.some(k => k.kind === 'tv' && k.label === '剧集'),
    'manifest 缺站点分类词表或剧集类目')
  const tvKinds = sources.flatMap(s => s.types).filter(t => t.kind === 'tv').map(t => t.code)
  assert(tvKinds.includes(6) && tvKinds.includes(102), `剧集类目应含三次元(6)与电视季(102): ${tvKinds}`)
  // 抓取能力照单下发：原生索引三种模式，扩展源 harvest 系
  assert(Array.isArray(native.modes) && native.modes.some(m => m.mode === 'full') &&
    native.modes.some(m => m.mode === 'refresh') && native.modes.every(m => m.label),
    '原生索引缺抓取模式声明')
  assert(sources.filter(s => !s.native).every(s => s.modes.every(m => m.mode.startsWith('harvest'))),
    '扩展源抓取模式应为 harvest 系')
  // 每源请求间隔与校验能力声明：默认值/下限随源下发，bgm 与 tmdb 有校验器
  assert(sources.every(s => typeof s.interval_default === 'number' && s.interval_default > 0),
    'manifest 缺每源请求间隔默认值')
  const tmdbRow = sources.find(s => s.id === 'tmdb')
  assert(tmdbRow && tmdbRow.validate === true &&
    tmdbRow.credentials.some(c => c.key === 'api_key') &&
    tmdbRow.credentials.some(c => c.key === 'read_token'),
    'TMDB 应声明校验能力与双鉴权凭据字段')
  // 校验能力由登记表声明：bgm（站长凭据）、tmdb（双鉴权）、mal（Client ID）
  assert(native.validate === true &&
    sources.every(s => ['bgm', 'tmdb', 'mal'].includes(s.id) || s.validate !== true),
    '校验能力声明应只在 bgm/tmdb/mal')
})

let extSubject = null
await check('扩展源索引详情带简介与外链（库数据，不回上游）', async () => {
  const source = ['tmdb', 'itunes', 'mal', 'anilist'].find(s => (extCount?.[s] ?? 0) > 0)
  if (!source) return // 尚未抓取扩展源：跳过
  const search = await json(`/api/subjects?source=${source}&page_size=1`)
  extSubject = search.body.items[0]
  assert(extSubject?.id >= 10000000000, `扩展 id 应 >= 1e10: ${extSubject?.id}`)
  const { res, body } = await json(`/api/subjects/${extSubject.id}`)
  assert(res.status === 200, `status=${res.status}`)
  assert(typeof body.summary === 'string', '索引详情缺 summary 字段')
  assert(typeof body.url === 'string' && body.url.startsWith('http'), '索引详情缺官方外链')
})

await check('索引详情带作品组来源清单', async () => {
  const { res, body } = await json(`/api/subjects/${first.id}`)
  assert(res.status === 200, `status=${res.status}`)
  assert(Array.isArray(body.sources) && body.sources.length >= 1, '缺少 sources 数组')
  assert(body.sources.every(s => typeof s.url === 'string' && typeof s.source === 'string'), 'sources 行结构异常')
})

await check('详情代理端点已下线（404）', async () => {
  const res = await fetch(base + `/api/subjects/${first.id}/detail`)
  assert(res.status === 404, `status=${res.status}`)
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
  assert(Array.isArray(body.harvesters) && body.harvesters.length === 4, '缺少 harvesters 清单')
  assert(body.harvesters.every(h => typeof h.configured === 'boolean'), 'harvesters 配置状态异常')
  // 新鲜度状态：每源带过期条目数（按各自有效保留期判定）
  assert(body.harvesters.every(h => typeof h.stale === 'number'), 'harvesters 缺 stale 计数')
  // 全源入口：上次全源抓取时间（扩展源行锚定，或退回原生主流程）
  assert('last_all_sources' in body, '缺少 last_all_sources')
})

await check('读取设置（敏感值脱敏）', async () => {
  const { res, body } = await json('/api/admin/settings', { headers: { Cookie: cookie } })
  assert(res.status === 200, `status=${res.status}`)
  assert(body.crawl && body.crawl.page_size > 0, 'crawl 设置异常')
  // 每源请求间隔与保留期覆盖：设置里逐源出现（0 = 用默认/全局默认）
  assert(body.sources && Object.values(body.sources).every(s => typeof s.interval_ms === 'number'),
    '来源设置缺 interval_ms')
  assert(body.sources && Object.values(body.sources).every(s => typeof s.refresh_ttl_days === 'number'),
    '来源设置缺 refresh_ttl_days')
  assert(!JSON.stringify(body.bgm.token ?? '').includes('eyJ'), '疑似泄露完整 Token')
})

await check('未登录访问管理接口被拒绝', async () => {
  const { res } = await json('/api/admin/overview')
  assert(res.status === 401, `status=${res.status}`)
})

await check('重跑作品匹配（幂等维护动作）', async () => {
  const { res, body } = await json('/api/admin/works/rematch', {
    method: 'POST',
    headers: { Cookie: cookie },
  })
  assert(res.status === 200, `status=${res.status}`)
  assert(typeof body.scanned === 'number' && typeof body.merged === 'number', '缺少匹配统计')
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
