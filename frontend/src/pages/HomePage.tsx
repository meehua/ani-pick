import * as React from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ArrowRight, Database, KeyRound, Search, ShieldCheck } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { SubjectCard } from '@/components/subject-card'
import { MoreLink, SectionTitle } from '@/components/blocks'
import { getMeta, searchSubjects } from '../api/client'
import { TYPE_LABEL, TYPE_ORDER, type MetaResponse, type Subject } from '../api/types'
import { fmtBytes, fmtDateTime, fmtNum } from '../lib/format'

const TYPE_HINT: Record<number, string> = {
  1: '漫画 / 小说 / 画集',
  2: 'TV / 剧场版 / OVA',
  3: '专辑 / 单曲 / OST',
  4: 'PC / 主机 / 手游',
  6: '日剧 / 欧美剧 / 综艺',
}

export default function HomePage() {
  const navigate = useNavigate()
  const [keyword, setKeyword] = React.useState('')
  const [meta, setMeta] = React.useState<MetaResponse | null>(null)
  const [top, setTop] = React.useState<Subject[]>([])
  const [recent, setRecent] = React.useState<Subject[]>([])
  const [loading, setLoading] = React.useState(true)

  React.useEffect(() => {
    let alive = true
    void Promise.allSettled([
      getMeta(),
      searchSubjects({ types: [2], sort: 'score', order: 'desc', rating_min: 50, page_size: 12 }),
      searchSubjects({ sort: 'id', page_size: 12 }),
    ]).then(([m, t, r]) => {
      if (!alive) return
      if (m.status === 'fulfilled') setMeta(m.value)
      if (t.status === 'fulfilled') setTop(t.value.items)
      if (r.status === 'fulfilled') setRecent(r.value.items)
      setLoading(false)
    })
    return () => {
      alive = false
    }
  }, [])

  const total = meta?.stats.total ?? 0

  return (
    <div>
      <section className="mx-auto max-w-3xl py-10 text-center sm:py-16">
        <p className="mb-3 inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] text-muted-foreground">
          <Database size={11} /> 本站索引 · {fmtNum(total)} 个条目
        </p>
        <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">
          按标签、排名与评分，<br className="sm:hidden" />找到下一部想看的作品
        </h1>
        <p className="mx-auto mt-3 max-w-xl text-sm leading-relaxed text-muted-foreground">
          AniPick 在本站维护 Bangumi 条目索引，筛选与排序不再依赖上游分页；条目详情由你的浏览器
          直接向 Bangumi 请求。
        </p>

        <form
          className="mx-auto mt-6 flex max-w-xl items-center gap-2"
          onSubmit={e => {
            e.preventDefault()
            const q = keyword.trim()
            navigate(q ? `/browse?keyword=${encodeURIComponent(q)}` : '/browse')
          }}
        >
          <div className="relative flex-1">
            <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={e => setKeyword(e.target.value)}
              placeholder="搜索作品名称（中文或原名）"
              className="h-10 pl-9"
              aria-label="搜索作品"
            />
          </div>
          <Button type="submit" size="lg" className="h-10">搜索</Button>
        </form>

        {meta && (
          <div className="mt-3 flex flex-wrap items-center justify-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
            <span>标签 {fmtNum(meta.stats.tags)}</span>
            <span>标签关联 {fmtNum(meta.stats.subject_tags)}</span>
            <span>数据库 {fmtBytes(meta.stats.db_size)}</span>
            <span>最近收录 {fmtDateTime(meta.stats.last_fetched)}</span>
          </div>
        )}
      </section>

      {!loading && total === 0 && (
        <div className="mx-auto mb-8 max-w-xl rounded-lg border border-dashed p-6 text-center">
          <p className="text-sm font-medium">索引还是空的</p>
          <p className="mt-1 text-xs text-muted-foreground">
            进入管理后台用站长账号启动一次抓取，或在设置里配置访问凭据后开启自动增量。
          </p>
          <Button asChild size="sm" className="mt-3">
            <Link to="/admin">打开管理后台 <ArrowRight size={13} /></Link>
          </Button>
        </div>
      )}

      <SectionTitle>按类型浏览</SectionTitle>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
        {TYPE_ORDER.map(t => {
          const count = meta?.stats.by_type[String(t)] ?? 0
          return (
            <Link
              key={t}
              to={`/browse?types=${t}&sort=rank`}
              className="group rounded-lg border p-3 transition-colors hover:bg-muted/50"
            >
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">{TYPE_LABEL[t]}</span>
                <span className="text-[11px] tabular-nums text-muted-foreground">{fmtNum(count)}</span>
              </div>
              <p className="mt-0.5 text-[11px] text-muted-foreground">{TYPE_HINT[t]}</p>
            </Link>
          )
        })}
      </div>

      <SectionTitle extra={<MoreLink to="/browse?types=2&sort=score" />}>动画高分</SectionTitle>
      <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 lg:grid-cols-6">
        {loading
          ? Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="aspect-[5/7] rounded-lg" />)
          : top.map(s => <SubjectCard key={s.id} subject={s} />)}
      </div>

      <SectionTitle extra={<MoreLink to="/browse?sort=id" />}>最近收录</SectionTitle>
      <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 lg:grid-cols-6">
        {loading
          ? Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="aspect-[5/7] rounded-lg" />)
          : recent.map(s => <SubjectCard key={s.id} subject={s} />)}
      </div>

      <SectionTitle>它是怎么工作的</SectionTitle>
      <div className="grid gap-3 sm:grid-cols-3">
        {[
          {
            icon: <Database size={15} />,
            title: '后台抓取，只存筛选信息',
            text: '站长账号在后台抓取条目的 ID、类型、标签及标签占比、排名、评分与日期；不保存简介、角色等详情。',
          },
          {
            icon: <KeyRound size={15} />,
            title: '浏览器直连详情',
            text: '条目详情由你的浏览器直接向 api.bgm.tv 请求，可携带你自己的凭据；本站服务端不中转用户请求。',
          },
          {
            icon: <ShieldCheck size={15} />,
            title: '凭据只在本机',
            text: '登录状态写入 localStorage，退出或清除浏览器数据即失效；本站不上传、不记录、不转发。',
          },
        ].map(card => (
          <div key={card.title} className="rounded-lg border p-4">
            <div className="mb-2 inline-flex size-7 items-center justify-center rounded bg-primary/10 text-primary">
              {card.icon}
            </div>
            <p className="text-sm font-medium">{card.title}</p>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{card.text}</p>
          </div>
        ))}
      </div>
    </div>
  )
}
