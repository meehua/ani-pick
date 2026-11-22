import { Link } from 'react-router-dom'
import { Cover } from './cover'
import { Table, TableBody, TableHeader, TableRow } from './ui/table'
import { Th, Td } from './table-bits'
import { isExtSource, sourceLabel, typeLabel } from '../api/vocab'
import type { Subject } from '../api/types'
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

// 网格：封面优先。右上角常驻「年份 · 类型」徽标（半透明深底保证可读）；悬停时封面
// 下半部渐变遮罩浮现，集中展示评分 / 排名 / 标签占比 / 评分人数——评分做主视觉。
// 封面下常驻标题与原名；悬停反馈：封面轻微缩放、标题下划线、遮罩浮现。
export function PosterCard({ subject, priority = false }: { subject: Subject; priority?: boolean }) {
  const fullName =
    subject.name_cn && subject.name_cn !== subject.name
      ? `${titleOf(subject)}（${subject.name}）`
      : titleOf(subject)
  const tagLine = subject.tags
    ?.slice(0, 3)
    .map(t => (t.pct > 0 ? `${t.name} ${t.pct.toFixed(0)}%` : t.name)) // 扩展源无占比，只显示名字
    .join(' · ')
  const badge = [typeLabel(subject.type, subject.source), subject.date ? yearOf(subject.date) : '']
    .filter(Boolean)
    .join(' · ')
  const hasOverlay =
    subject.score > 0 || subject.rank > 0 || Boolean(tagLine) || subject.rating_total > 0

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
        <span className="num pointer-events-none absolute right-1.5 top-1.5 rounded bg-black/55 px-1.5 py-1 text-[11px] font-medium leading-none text-white/95 backdrop-blur-[2px]">
          {badge}
        </span>
        {hasOverlay && (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 flex h-1/2 flex-col justify-end gap-1 rounded-b-md bg-gradient-to-t from-black/90 via-black/55 to-transparent px-2.5 pb-2.5 pt-4 opacity-0 transition-opacity duration-200 group-hover:opacity-100">
            {(subject.score > 0 || subject.rank > 0) && (
              <p className="flex items-baseline gap-2">
                {subject.score > 0 && (
                  <span className="num text-xl font-semibold leading-none text-white">
                    {scoreOf(subject)}
                    <span className="ml-0.5 text-[11px] font-normal text-white/55">分</span>
                  </span>
                )}
                {subject.rank > 0 && (
                  <span className="num text-xs font-medium text-white/75">#{subject.rank}</span>
                )}
              </p>
            )}
            {tagLine && <p className="truncate text-[11px] leading-snug text-white/85">{tagLine}</p>}
            {subject.rating_total > 0 && (
              <p className="num truncate text-[11px] leading-snug text-white/55">
                {fmtNum(subject.rating_total)} 人评分
              </p>
            )}
          </div>
        )}
      </div>
      <h3
        className="mt-2 line-clamp-2 text-sm font-medium leading-snug underline-offset-2 group-hover:underline"
        title={fullName}
      >
        {titleOf(subject)}
      </h3>
      {subject.name_cn && subject.name_cn !== subject.name && (
        <p className="mt-0.5 truncate text-xs text-muted-foreground" title={subject.name}>
          {subject.name}
        </p>
      )}
      {/* 来源徽标：小字号 muted，只标注扩展源（bgm 是主体不重复刷屏） */}
      {isExtSource(subject.source) && (
        <p className="mt-0.5 truncate text-[11px] text-muted-foreground">
          {sourceLabel(subject.source)}
        </p>
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
            {typeLabel(subject.type, subject.source)} · {yearOf(subject.date)}
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
            <Td className="text-muted-foreground">{typeLabel(s.type, s.source)}</Td>
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
