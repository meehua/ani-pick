// 多源信息融合的不变式测试（npm test）：
//   1. 融合依据只有组内行的排列顺序（管理员设置的有效优先序）——先到先得；
//   2. 不存在任何基线来源：完全不含原生索引行的组与含它的组走同一代码路径，
//      移除组内任意一行后按剩余行序重建；
//   3. 判重归一化与后端合并器（internal/store/workmatch.go 的 NormalizeTitle）
//      同一实现语义——测试用例与该函数的 Go 测试逐条对应；
//   4. 评分行归组与简介归并消费同一份行序。
// fusion.ts 是 TS 源码：这里用 esbuild 即时编译成 ESM 再断言。
import { deepEqual, equal } from 'node:assert/strict'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { build } from 'esbuild'

const root = new URL('..', import.meta.url).pathname
const tmp = mkdtempSync(join(tmpdir(), 'fusion-test-'))
const outfile = join(tmp, 'fusion.mjs')

try {
  await build({
    entryPoints: [join(root, 'src/pages/subject/fusion.ts')],
    bundle: true,
    format: 'esm',
    outfile,
    logLevel: 'silent',
  })
  const { normText, variantOf, buildFusedFacts, groupScoreRows, mergeSummaries } =
    await import(pathToFileURL(outfile))

  // ---------- normText 与 Go NormalizeTitle 同语义（用例与 Go 测试一致） ----------
  equal(normText('少女終末旅行'), '少女終末旅行')
  equal(normText("  Girls' Last Tour! "), 'girlslasttour')
  equal(normText('Ｆｕｌｌｍｅｔａｌ Ａｌｃｈｅｍｉｓｔ'), 'fullmetalalchemist')
  equal(normText('Steins;Gate'), 'steinsgate')
  equal(normText('千と彩色の…（第二版） [BD 1080p]'), '千と彩色の第二版bd1080p')
  equal(normText('!!!'), '')
  equal(normText("Girls' Last Tour"), normText('girls last tour!!'))

  // ---------- 字段融合表：顺序驱动、无基线 ----------
  const rowA = (source, variant, facts) => ({ source, variant, facts })
  const dirX = { k: '导演', v: 'X' }
  const dirY = { k: '导演', v: 'Y' }
  const studio = { k: '制作', v: 'ufotable' }
  const eps = { k: '集数', v: '1' }
  const runtimeTV = { k: '片长', v: '24分钟' }
  const runtimeMovie = { k: '片长', v: '110分钟' }

  // a. 完全不含原生索引行的组（两行扩展源）：同一代码路径、同一规则
  {
    const rows = [rowA('s2', '剧场版', [dirY, eps]), rowA('s1', 'TV', [dirX, runtimeTV])]
    const table = buildFusedFacts(rows)
    equal(table.length, 3)
    // 先到先得：第一行的值是主值；第二行的异值追加
    equal(table[0].key, '导演')
    equal(table[0].entries.length, 2)
    equal(table[0].entries[0].source, 's2')
    equal(table[0].entries[0].value, 'Y')
    equal(table[0].entries[1].source, 's1')
    equal(table[0].entries[1].value, 'X')
    equal(table[1].key, '集数')
    equal(table[1].entries.length, 1)
    equal(table[2].key, '片长')
    equal(table[2].entries.length, 1)
  }

  // b. 顺序翻转：同一组行，调换顺序后主值/并陈值随序翻转（设置调序的语义）
  {
    const rows = [rowA('s1', 'TV', [dirX, runtimeTV]), rowA('s2', '剧场版', [dirY, eps])]
    const table = buildFusedFacts(rows)
    equal(table[0].entries[0].source, 's1')
    equal(table[0].entries[0].value, 'X')
    equal(table[0].entries[1].source, 's2')
    // 字段出现顺序也跟随行序：第一行的片长先于第二行的集数
    deepEqual(table.map(r => r.key), ['导演', '片长', '集数'])
  }

  // c. 删源：移除组内任意一行后，融合表按剩余行重建（原异值并陈的字段只剩一行）
  {
    const withBoth = buildFusedFacts([rowA('s1', 'TV', [dirX]), rowA('s2', '剧场版', [dirY])])
    equal(withBoth[0].entries.length, 2)
    const afterRemove = buildFusedFacts([rowA('s2', '剧场版', [dirY, eps])])
    equal(afterRemove[0].entries.length, 1)
    equal(afterRemove[0].entries[0].value, 'Y')
    equal(afterRemove[1].key, '集数')
  }

  // d. 同字段同值静默去重（归一化后相等：全角半角/大小写/标点差异）
  {
    const table = buildFusedFacts([
      rowA('s1', '', [{ k: '制作', v: 'ufotable' }]),
      rowA('s2', '', [{ k: '制作', v: 'ufotable' }]),
      rowA('s3', '', [{ k: '制作', v: 'UFOTABLE！' }]),
    ])
    equal(table[0].entries.length, 1)
    equal(table[0].entries[0].source, 's1')
  }

  // e. 同一来源组内多行：值不同并陈并保留变体，值相同留一条
  {
    const table = buildFusedFacts([
      rowA('s2', '剧场版', [{ k: '片长', v: runtimeMovie.v }]),
      rowA('s2', 'TV', [{ k: '片长', v: runtimeTV.v }]),
    ])
    equal(table[0].entries.length, 2)
    equal(table[0].entries[0].variant, '剧场版')
    equal(table[0].entries[1].variant, 'TV')
    const same = buildFusedFacts([
      rowA('s2', '剧场版', [{ k: '制作', v: studio.v }]),
      rowA('s2', 'TV', [{ k: '制作', v: studio.v }]),
    ])
    equal(same[0].entries.length, 1)
  }

  // ---------- 评分行归组：消费同一份行序，同源多行标变体、单行不标 ----------
  {
    const groups = groupScoreRows([
      { id: 1, source: 's1', score: 7.8, rating_total: 100, url: 'u1', platform: '', date: '' },
      { id: 2, source: 's2', score: 8.2, rating_total: 50, url: 'u2', platform: 'TV', date: '2023-04-01' },
      { id: 3, source: 's2', score: 8.4, rating_total: 30, url: 'u3', platform: '剧场版', date: '' },
    ])
    equal(groups.length, 2)
    equal(groups[0].source, 's1')
    equal(groups[0].rows[0].variant, '')
    equal(groups[1].source, 's2')
    deepEqual(groups[1].rows.map(r => r.variant), ['TV', '剧场版'])
    // 删源：移除一行后剩余行照常归组
    const afterRemove = groupScoreRows([
      { id: 1, source: 's1', score: 7.8, rating_total: 100, url: 'u1', platform: '', date: '' },
    ])
    deepEqual(afterRemove.map(g => g.source), ['s1'])
  }

  // ---------- 简介归并：同来源一个折叠、段内标变体、全同只留一条 ----------
  {
    const groups = mergeSummaries('主简介', [
      { source: 's2', variant: '剧场版', text: '剧场版简介' },
      { source: 's2', variant: 'TV', text: 'TV简介' },
      { source: 's1', variant: '', text: '主简介' }, // 与主简介相同：不出现
    ])
    equal(groups.length, 1)
    equal(groups[0].source, 's2')
    deepEqual(groups[0].segments, [
      { variant: '剧场版', text: '剧场版简介' },
      { variant: 'TV', text: 'TV简介' },
    ])
    // 单条简介不标变体
    const single = mergeSummaries('', [{ source: 's2', variant: 'TV', text: '只有一条' }])
    deepEqual(single[0].segments, [{ text: '只有一条' }])
  }

  // ---------- variantOf：平台优先，其次年份 ----------
  equal(variantOf({ platform: 'TV', date: '2023-04-01' }), 'TV')
  equal(variantOf({ platform: '', date: '2023-04-01' }), '2023')

  console.log('fusion test ok')
} finally {
  rmSync(tmp, { recursive: true, force: true })
}
