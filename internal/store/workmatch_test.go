package store

import (
	"strconv"
	"testing"

	"anipick/internal/registry"
)

func TestNormalizeTitle(t *testing.T) {
	cases := map[string]string{
		"少女終末旅行":               "少女終末旅行", // 汉字保留
		"  Girls' Last Tour! ": "girlslasttour",
		"Ｆｕｌｌｍｅｔａｌ Ａｌｃｈｅｍｉｓｔ": "fullmetalalchemist", // 全角 → 半角
		"Steins;Gate": "steinsgate",
		"Re:ZERO -Starting Life in Another World-": "rezerostartinglifeinanotherworld",
		"千と彩色の…（第二版） [BD 1080p]":                   "千と彩色の第二版bd1080p", // 标点/括号全去
		"":    "",
		"!!!": "",
	}
	for in, want := range cases {
		if got := NormalizeTitle(in); got != want {
			t.Errorf("NormalizeTitle(%q) = %q, 期望 %q", in, got, want)
		}
	}
	// 归一化后相等（大小写/标点/空白差异不参与比较）
	if NormalizeTitle("Girls' Last Tour") != NormalizeTitle("girls last tour!!") {
		t.Error("等价标题应归一化到同一结果")
	}
}

func TestKindClassOf(t *testing.T) {
	// 站点分类以来源登记表的声明为准：每个源自己声明哪些类型编码属于哪个
	// kind（双层词表的外层）；未声明的组合不归类。
	cases := []struct {
		source string
		typ    int
		kind   string
	}{
		{registry.SourceBGM, registry.TypeAnime, string(registry.KindAnime)},
		{registry.SourceMAL, registry.TypeAnime, string(registry.KindAnime)},
		{registry.SourceAniList, registry.TypeAnime, string(registry.KindAnime)},
		{registry.SourceTMDB, registry.TypeMovie, string(registry.KindMovie)},
		{registry.SourceITunes, registry.TypeMovie, string(registry.KindMovie)},
		{registry.SourceTMDB, registry.TypeTVSeries, string(registry.KindTV)},
		{registry.SourceBGM, registry.TypeReal, string(registry.KindTV)},   // 三次元与剧集同物可比
		{registry.SourceBGM, registry.TypeBook, string(registry.KindBook)}, // 书籍归类但无跨源同物
		{registry.SourceBGM, registry.TypeMusic, string(registry.KindMusic)},
		{registry.SourceBGM, registry.TypeGame, string(registry.KindGame)},
		{registry.SourceITunes, registry.TypeTVSeries, ""}, // iTunes 只产出电影
		{registry.SourceTMDB, registry.TypeAnime, ""},      // 不存在的组合
		{"unknown", registry.TypeAnime, ""},                // 未登记来源
	}
	for _, c := range cases {
		if got := KindClassOf(c.source, c.typ); got != c.kind {
			t.Errorf("KindClassOf(%s,%d) = %q, 期望 %q", c.source, c.typ, got, c.kind)
		}
	}
	// 启发式合并只进可跨源比对的分类（动画/电影/剧集）；
	// 书籍/音乐/游戏直接自成作品，不查候选。
	for _, k := range registry.KindOrder {
		want := k == registry.KindAnime || k == registry.KindMovie || k == registry.KindTV
		if registry.Mergeable(k) != want {
			t.Errorf("Mergeable(%s) = %v, 期望 %v", k, registry.Mergeable(k), want)
		}
	}
	if registry.Mergeable(registry.KindOther) {
		t.Error("未分类不应参与合并")
	}
}

func TestPickWork(t *testing.T) {
	mk := func(id int64, source string, titles ...string) matchCandidate {
		return matchCandidate{ID: id, Source: source, WorkID: id, Titles: TitleSetOf("", "", titles)}
	}
	in := func(titles ...string) matchRecord {
		return matchRecord{Source: SourceAniList, Titles: TitleSetOf("", "", titles)}
	}

	// 无候选 → 独立
	if w, amb, _ := pickWork(in("少女終末旅行"), nil); w != 0 || amb {
		t.Fatalf("无候选应独立: w=%d amb=%v", w, amb)
	}
	// 标题不相交 → 独立
	if w, amb, _ := pickWork(in("少女終末旅行"), []matchCandidate{mk(1, SourceBGM, "夏日大作战")}); w != 0 || amb {
		t.Fatalf("不相交应独立: w=%d amb=%v", w, amb)
	}
	// 唯一命中 → 该组（候选行的标题集含 name_cn 与 alt_titles 双方）
	if w, amb, _ := pickWork(in("Girls' Last Tour"), []matchCandidate{
		mk(42, SourceBGM, "少女終末旅行", "Girls' Last Tour"),
	}); w != 42 || amb {
		t.Fatalf("唯一命中应返回 42: w=%d amb=%v", w, amb)
	}
	// 候选标题集（name 之外还有 alt_titles）也可命中
	if w, _, _ := pickWork(in("少女終末旅行"), []matchCandidate{
		mk(42, SourceBGM, "Girls' Last Tour", "少女終末旅行"),
	}); w != 42 {
		t.Fatalf("alt 命中失败: w=%d", w)
	}
	// 多行同组（work_id 相同）→ 一个候选作品，合并
	if w, amb, _ := pickWork(in("少女終末旅行"), []matchCandidate{
		mk(42, SourceBGM, "少女終末旅行", "Shoujo Shuumatsu Ryokou"),
		mk(10000000001, SourceMAL, "Shoujo Shuumatsu Ryokou"),
	}); w != 42 || amb {
		t.Fatalf("同组多行应合并到 42: w=%d amb=%v", w, amb)
	}
	// 两个不同作品打平 → 拒绝（宁缺毋滥）
	w, amb, reason := pickWork(in("同名同年的两部作品"), []matchCandidate{
		mk(1, SourceBGM, "同名同年的两部作品"),
		mk(2, SourceBGM, "同名同年的两部作品"),
	})
	if w != 0 || !amb || reason == "" {
		t.Fatalf("歧义应拒绝: w=%d amb=%v reason=%q", w, amb, reason)
	}
}

func TestTitleSetOf(t *testing.T) {
	set := TitleSetOf("Name", "中文名", []string{"", "  ", "Name", "别名"})
	if len(set) != 3 {
		t.Fatalf("去重/去空后应 3 项: %v", set)
	}
}

// 组内来源行的顺序必须跟管理员设置的有效优先序走（登记表默认只是兜底）：
// 设置里调序，GroupSourceSort 与 extSourceOrder 的基准要立即跟着变——
// 组页的评分行归组/简介归并/融合表全部消费这份序。
func TestGroupSourceSortFollowsSettings(t *testing.T) {
	gs := []GroupSource{
		{ID: 3, Source: registry.SourceAniList},
		{ID: 1, Source: registry.SourceBGM},
		{ID: 2, Source: registry.SourceMAL},
	}
	// 登记表默认：bgm(10) < anilist(30)? 以登记表声明为准——这里只断言
	// 设置覆写后顺序翻转，不写死默认值。
	GroupSourceSort(gs)
	defaultOrder := make([]string, len(gs))
	for i, g := range gs {
		defaultOrder[i] = g.Source
	}

	// 设置把 anilist 提到最前、mal 其次、bgm 垫底 → 顺序立即翻转
	SetSourcePriorities(map[string]int{
		registry.SourceAniList: 1,
		registry.SourceMAL:     2,
		registry.SourceBGM:     3,
	})
	defer SetSourcePriorities(nil) // 回落登记表默认
	settled := []GroupSource{{ID: 1, Source: registry.SourceBGM}, {ID: 2, Source: registry.SourceMAL}, {ID: 3, Source: registry.SourceAniList}}
	GroupSourceSort(settled)
	want := []string{registry.SourceAniList, registry.SourceMAL, registry.SourceBGM}
	for i, g := range settled {
		if g.Source != want[i] {
			t.Fatalf("设置优先序未生效: got %v want %v", settled, want)
		}
	}
	if defaultOrder[0] == want[0] {
		t.Fatalf("对照无效：默认序与设置序相同，翻转无从证明（默认 %v）", defaultOrder)
	}

	// extSourceOrder（清理重指主行的 SQL 排序基准）消费同一份快照
	extWant := []string{registry.SourceAniList, registry.SourceMAL}
	extGot := extSourceOrder()
	pos := func(ids []string, id string) int {
		for i, v := range ids {
			if v == id {
				return i
			}
		}
		return -1
	}
	for _, id := range extWant {
		if pos(extGot, id) < 0 {
			t.Fatalf("extSourceOrder 缺扩展源 %s: %v", id, extGot)
		}
	}
	if pos(extGot, extWant[0]) > pos(extGot, extWant[1]) {
		t.Fatalf("extSourceOrder 未按设置序: %v（期望 %s 在 %s 前）", extGot, extWant[0], extWant[1])
	}
}

// TestPickWorkPriorityConflict 多作品打平：按源优先级归类（bgm > tmdb），
// 同级打平仍拒绝（宁缺毋滥）。
func TestPickWorkPriorityConflict(t *testing.T) {
	in := matchRecord{Source: "tmdb", Name: "X", Titles: []string{"同名作品"}}
	cands := []matchCandidate{
		{ID: 2, Source: "tmdb", WorkID: 200, Titles: []string{"同名作品"}},
		{ID: 1, Source: "bgm", WorkID: 100, Titles: []string{"同名作品"}},
	}
	SetSourcePriorities(map[string]int{"bgm": 10, "tmdb": 40})
	w, amb, _ := pickWork(in, cands)
	if amb || w != 100 {
		t.Fatalf("应按源优先级归入 bgm 作品 100: w=%d amb=%v", w, amb)
	}

	// 同级（两个都是 bgm）打平 → 拒绝
	cands2 := []matchCandidate{
		{ID: 1, Source: "bgm", WorkID: 100, Titles: []string{"同名作品"}},
		{ID: 3, Source: "bgm", WorkID: 300, Titles: []string{"同名作品"}},
	}
	w, amb, reason := pickWork(in, cands2)
	if !amb || w != 0 {
		t.Fatalf("同级打平应拒绝: w=%d amb=%v (%s)", w, amb, reason)
	}
}

// TestPairablesOf 组驱动候选：锚点所属全部组的成员并集（排除同源、跨组去重）；
// 不在任何组 → 无候选。
func TestPairablesOf(t *testing.T) {
	SetMatchTypeGroups([][]registry.TypeRef{
		{{Source: "bgm", Type: 6}, {Source: "tmdb", Type: 102}, {Source: "itunes", Type: 1}},
		{{Source: "bgm", Type: 2}, {Source: "tmdb", Type: 102}}, // tmdb 剧集同时进动画组
	})
	srcs, codes := pairablesOf("tmdb", 102)
	// 并集 = {bgm:6, itunes:1, bgm:2}（排除 tmdb 自己）
	got := map[string]int{}
	for i, s := range srcs {
		got[s+":"+strconv.Itoa(codes[i])] = 1
	}
	for _, want := range []string{"bgm:6", "itunes:1", "bgm:2"} {
		if got[want] != 1 {
			t.Fatalf("候选缺 %s: %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("候选应跨组去重后 3 项: %v", got)
	}

	// 不在组内：bgm:1（书籍）自成作品
	srcs, _ = pairablesOf("bgm", 1)
	if len(srcs) != 0 {
		t.Fatalf("不在组内应无候选: %v", srcs)
	}
}
