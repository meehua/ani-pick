import { Link } from 'react-router-dom'
import { Cover } from './cover'
import { Table, TableBody, TableHeader, TableRow } from './ui/table'
import { Th, Td } from './table-bits'
import { TYPE_LABEL, type Subject } from '../api/types'
import { fmtNum, yearOf } from '../lib/format'

function titleOf(s: Subject): string {
  return s.name_cn || s.name
}

function scoreOf(s: Subject): string {
  return s.score > 0 ? s.score.toFixed(1) : '—'
}

function tagLine(s: Subject): string {
  const meta = [...new Set(s.meta_tags ?? [])].slice(0, 3)
  if (meta.length) return meta.join(' · ')
  return (s.tags ?? []).slice(0, 3).map(t => t.name).join(' · ')
}

// 网格：封面优先，图下两行信息，6 列铺满宽屏。
export function PosterCard({ subject, priority = false }: { subject: Subject; priority?: boolean }) {
  return (
    <Link
      to={`/subject/${subject.id}`}
      data-subject-id={subject.id}
      className="card-hover group block min-w-0"
    >
      <div className="relative">
        <Cover
          src={subject.image}
          alt={titleOf(subject)}
          priority={priority}
          className="aspect-[5/7] w-full rounded-md border"
        />
        <div className="pointer-events-none absolute inset-0 rounded-md bg-gradient-to-t from-black/55 via-transparent to-transparent opacity-0 transition-opacity duration-200 group-hover:opacity-100" />
        <span className="pointer-events-none absolute right-1.5 top-1.5 rounded bg-black/70 px-1.5 py-0.5 text-[11px] font-medium text-white opacity-0 transition-opacity duration-200 group-hover:opacity-100">
          {scoreOf(subject)} 分
        </span>
        <span className="pointer-events-none absolute bottom-2 left-2 right-2 hidden text-[11px] font-medium text-white opacity-0 transition-opacity duration-200 group-hover:opacity-100 sm:block">
          查看条目
        </span>
      </div>
      <h3 className="mt-2 line-clamp-2 text-sm font-medium leading-snug underline-offset-2 group-hover:underline">
        {titleOf(subject)}
      </h3>
      {subject.name_cn && subject.name_cn !== subject.name && (
        <p className="mt-0.5 line-clamp-1 text-xs text-muted-foreground">{subject.name}</p>
      )}
      <p className="num mt-1 flex flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground">
        <span>{yearOf(subject.date)}</span>
        <span className="text-border">·</span>
        <span>{TYPE_LABEL[subject.type] ?? '其他'}</span>
        {subject.score > 0 && (
          <>
            <span className="text-border">·</span>
            <span className="font-medium text-foreground">{scoreOf(subject)}</span>
          </>
        )}
        {subject.rank > 0 && (
          <>
            <span className="text-border">·</span>
            <span>#{subject.rank}</span>
          </>
        )}
      </p>
      {subject.rating_total > 0 && (
        <p className="num mt-0.5 text-xs text-muted-foreground">
          {fmtNum(subject.rating_total)} 人评分
        </p>
      )}
      {subject.tags && subject.tags.length > 0 && (
        <div className="mt-1 flex flex-wrap gap-1">
          {subject.tags.slice(0, 3).map(t => (
            <span
              key={t.name}
              className="inline-flex max-w-full items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground"
              title={`${t.name}：${t.votes} 票，占该条目标签票数 ${t.pct.toFixed(1)}%`}
            >
              <span className="truncate">{t.name}</span>
              <span className="tabular-nums opacity-70">{t.pct.toFixed(0)}%</span>
            </span>
          ))}
        </div>
      )}
    </Link>
  )
}

// 列表：默认视图。一行 64px，左侧缩略图，右侧两行文字。
export function DenseRow({ subject }: { subject: Subject }) {
  const tags = tagLine(subject)

  return (
    <Link
      to={`/subject/${subject.id}`}
      data-subject-id={subject.id}
      className="row-link group -mx-2 flex gap-3 rounded-md px-2 py-2.5"
    >
      <Cover
        src={subject.image}
        alt={titleOf(subject)}
        className="h-[62px] w-11 shrink-0 rounded border"
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <h3 className="min-w-0 truncate text-sm font-medium underline-offset-2 group-hover:underline">
            {titleOf(subject)}
          </h3>
          <span className="shrink-0 text-xs text-muted-foreground">
            {TYPE_LABEL[subject.type] ?? '其他'} · {yearOf(subject.date)}
          </span>
        </div>
        {subject.name_cn && subject.name_cn !== subject.name && (
          <p className="mt-0.5 truncate text-xs text-muted-foreground">{subject.name}</p>
        )}
        <div className="num mt-1 flex items-center gap-2 text-xs text-muted-foreground">
          {subject.rank > 0 && <span>#{subject.rank}</span>}
          {subject.score > 0 && <span className="font-medium text-foreground">{scoreOf(subject)}</span>}
          {subject.rating_total > 0 && <span>{fmtNum(subject.rating_total)} 人</span>}
          {tags && <span className="hidden truncate sm:inline">· {tags}</span>}
        </div>
      </div>
    </Link>
  )
}

// 表格：给逐列比较用，行高 36px。
export function SubjectTable({ subjects }: { subjects: Subject[] }) {
  return (
    <Table containerClassName="scrollbar-thin rounded-lg border" className="min-w-[780px]">
      <TableHeader>
        <TableRow>
          <Th right className="w-12">ID</Th>
          <Th>作品</Th>
          <Th className="w-16">类型</Th>
          <Th right className="w-16">年份</Th>
          <Th right className="w-14">评分</Th>
          <Th right className="w-20">人数</Th>
          <Th right className="w-16">排名</Th>
          <Th right className="w-20">收藏</Th>
        </TableRow>
      </TableHeader>
      <TableBody>
        {subjects.map(s => (
          <TableRow key={s.id} data-subject-id={s.id}>
            <Td className="num text-right text-xs text-muted-foreground">{s.id}</Td>
            <Td className="max-w-[360px]">
              <div className="flex items-center gap-2">
                <Cover
                  src={s.image}
                  alt={titleOf(s)}
                  className="h-9 w-6 shrink-0 rounded-[3px] border"
                />
                <div className="min-w-0">
                  <Link
                    to={`/subject/${s.id}`}
                    className="block truncate font-medium underline-offset-2 hover:underline"
                    title={titleOf(s)}
                  >
                    {titleOf(s)}
                  </Link>
                  {s.name_cn && s.name_cn !== s.name && (
                    <span className="block truncate text-[11px] text-muted-foreground">{s.name}</span>
                  )}
                </div>
              </div>
            </Td>
            <Td className="text-muted-foreground">{TYPE_LABEL[s.type] ?? '其他'}</Td>
            <Td className="num text-right text-muted-foreground">{yearOf(s.date)}</Td>
            <Td className="num text-right font-medium">{scoreOf(s)}</Td>
            <Td className="num text-right text-muted-foreground">{fmtNum(s.rating_total)}</Td>
            <Td className="num text-right text-muted-foreground">
              {s.rank > 0 ? s.rank : '—'}
            </Td>
            <Td className="num text-right text-muted-foreground">{fmtNum(s.collect)}</Td>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
