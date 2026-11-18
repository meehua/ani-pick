import { Link } from 'react-router-dom'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Cover } from '@/components/cover'
import type {
  BgmCharacter,
  BgmEpisode,
  BgmPerson,
  BgmRelation,
} from '../../api/types'
import { titleOf } from './shared'

// tabs 用 stock line 变体（自带下划线激活态）；内容全部行式列表（无卡片盒）：行高小、同屏条数多。

export function SubjectTabs({
  tab,
  onTabChange,
  characters,
  persons,
  relations,
  episodes,
}: {
  tab: string | null
  onTabChange: (v: string) => void
  characters: BgmCharacter[]
  persons: BgmPerson[]
  relations: BgmRelation[]
  episodes: BgmEpisode[]
}) {
  const defaultTab = characters.length
    ? 'characters'
    : persons.length
      ? 'staff'
      : relations.length
        ? 'related'
        : 'episodes'

  return (
    <Tabs value={tab ?? defaultTab} onValueChange={onTabChange}>
      <TabsList variant="line" className="h-auto w-full justify-start gap-6 border-b pb-1">
        <TabsTrigger value="characters" className="gap-1.5">
          角色 <span className="num text-xs text-muted-foreground">{characters.length}</span>
        </TabsTrigger>
        <TabsTrigger value="staff" className="gap-1.5">
          制作人员 <span className="num text-xs text-muted-foreground">{persons.length}</span>
        </TabsTrigger>
        <TabsTrigger value="related" className="gap-1.5">
          关联 <span className="num text-xs text-muted-foreground">{relations.length}</span>
        </TabsTrigger>
        <TabsTrigger value="episodes" className="gap-1.5">
          剧集 <span className="num text-xs text-muted-foreground">{episodes.length}</span>
        </TabsTrigger>
      </TabsList>

      <TabsContent value="characters">
        {characters.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">暂无角色信息</p>
        ) : (
          <div className="grid grid-cols-1 gap-x-6 sm:grid-cols-2 xl:grid-cols-3">
            {characters.slice(0, 40).map(c => (
              <div key={c.id} className="flex items-center gap-2.5 py-1">
                <Cover
                  src={c.images?.grid || c.images?.medium}
                  alt={c.name}
                  fallback=""
                  className="size-9 shrink-0 rounded-md"
                />
                <div className="min-w-0">
                  <p className="truncate text-[13px] font-medium" title={c.name}>
                    {c.name}
                  </p>
                  <p
                    className="truncate text-xs text-muted-foreground"
                    title={c.actors?.[0] ? `${c.relation} · 声优 ${c.actors[0].name}` : c.relation}
                  >
                    {c.relation}
                    {c.actors?.[0] ? ` · 声优 ${c.actors[0].name}` : ''}
                  </p>
                </div>
              </div>
            ))}
          </div>
        )}
      </TabsContent>

      <TabsContent value="staff">
        {persons.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">暂无制作人员信息</p>
        ) : (
          <div className="grid grid-cols-1 gap-x-6 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {persons.map(p => (
              <p
                key={`${p.id}-${p.relation}`}
                className="truncate py-[5px] text-[13px]"
                title={`${p.name} · ${p.relation}${p.career?.length ? ` · ${p.career.join(' / ')}` : ''}`}
              >
                <span className="font-medium">{p.name}</span>
                <span className="text-xs text-muted-foreground">
                  {' '}
                  {p.relation}
                  {p.career?.length ? ` · ${p.career.join(' / ')}` : ''}
                </span>
              </p>
            ))}
          </div>
        )}
      </TabsContent>

      <TabsContent value="related">
        {relations.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">暂无关联条目</p>
        ) : (
          <div className="grid grid-cols-1 gap-x-6 sm:grid-cols-2 xl:grid-cols-3">
            {relations.map(r => (
              <Link
                key={`${r.id}-${r.relation}`}
                to={`/subject/${r.id}`}
                className="group flex items-center gap-2.5 py-1"
              >
                <Cover
                  src={r.images?.grid || r.images?.medium}
                  alt={r.name}
                  fallback=""
                  className="h-12 w-9 shrink-0 rounded"
                />
                <div className="min-w-0">
                  <p className="truncate text-[13px] font-medium group-hover:underline">
                    {titleOf(r)}
                  </p>
                  <p className="truncate text-xs text-muted-foreground">
                    {r.relation}
                    {r.date ? ` · ${r.date}` : ''}
                  </p>
                </div>
              </Link>
            ))}
          </div>
        )}
      </TabsContent>

      <TabsContent value="episodes">
        {episodes.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">暂无剧集信息</p>
        ) : (
          <div className="divide-y rounded-lg border">
            {episodes.map(ep => (
              <div key={ep.id} className="flex items-baseline gap-3 px-3 py-1.5 text-[13px]">
                <span className="num w-10 shrink-0 text-right text-xs text-muted-foreground">
                  {ep.sort ?? ep.ep ?? '—'}
                </span>
                <span className="min-w-0 flex-1 truncate">
                  {ep.name_cn || ep.name || '未命名'}
                  {ep.name_cn && ep.name && (
                    <span className="ml-2 text-xs text-muted-foreground">{ep.name}</span>
                  )}
                </span>
                <span className="num shrink-0 text-xs text-muted-foreground">{ep.airdate || ''}</span>
              </div>
            ))}
          </div>
        )}
      </TabsContent>
    </Tabs>
  )
}
