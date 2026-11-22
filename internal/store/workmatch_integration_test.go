package store

import (
	"context"
	"testing"

	"anipick/internal/registry"
)

// TestWorkMatching 集成：ref 命中保持、mal_id 确定性链、标题启发合并、
// 歧义拒绝、主行按源优先级重排。
func TestWorkMatching(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()

	// ① (source, source_ref) 重写：work_id 稳定
	rec := Subject{Source: SourceTMDB, SourceRef: "movie/500", Type: TypeMovie, Name: "Inception", Date: "2010-07-16"}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{rec}, 0); err != nil {
		t.Fatal(err)
	}
	var w1 int64
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE source='tmdb' AND source_ref='movie/500'`).Scan(&w1); err != nil {
		t.Fatal(err)
	}
	if w1 == 0 {
		t.Fatal("首写后 work_id 应为自身 id")
	}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{rec}, 0); err != nil {
		t.Fatal(err)
	}
	var w1b int64
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE source='tmdb' AND source_ref='movie/500'`).Scan(&w1b); err != nil {
		t.Fatal(err)
	}
	if w1b != w1 {
		t.Fatalf("重写应保持 work_id: %d != %d", w1b, w1)
	}

	// ② mal_id 确定性链：mal 先入自成一组；anilist 同 mal_id → 并入同组，
	// 主行按源优先级重排为 anilist 行
	malRow := Subject{Source: SourceMAL, SourceRef: "99420", ExtKey: "99420", Type: 2,
		Name: "Shoujo Shuumatsu Ryokou", Date: "2017-10-06"}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{malRow}, 0); err != nil {
		t.Fatal(err)
	}
	anilist := Subject{Source: SourceAniList, SourceRef: "99420", ExtKey: "99420", Type: 2,
		Name: "Girls' Last Tour", AltTitles: []string{"少女終末旅行"}, Date: "2017-10-06"}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{anilist}, 0); err != nil {
		t.Fatal(err)
	}
	var workJ, workA, idA int64
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE source='mal' AND source_ref='99420'`).Scan(&workJ); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT work_id, id FROM subjects WHERE source='anilist' AND source_ref='99420'`).Scan(&workA, &idA); err != nil {
		t.Fatal(err)
	}
	if workJ != workA {
		t.Fatalf("mal_id 链应同组: mal=%d anilist=%d", workJ, workA)
	}
	if workA != idA {
		t.Fatalf("主行应重排为 anilist 行（优先级更高）: work=%d id=%d", workA, idA)
	}

	// ③ 标题启发：bgm 行（name 与 anilist 的 alt_titles 归一化相等）加入同组，
	// 主行重排为 bgm 行
	bgm := Subject{ID: 352661, Type: 2, Name: "少女終末旅行", Date: "2017-10-06"}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{bgm}, 0); err != nil {
		t.Fatal(err)
	}
	var workB, workJ2 int64
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE id = 352661`).Scan(&workB); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE source='mal' AND source_ref='99420'`).Scan(&workJ2); err != nil {
		t.Fatal(err)
	}
	if workB != 352661 {
		t.Fatalf("主行应重排为 bgm 行: %d", workB)
	}
	if workJ2 != 352661 {
		t.Fatalf("组内扩展源行应跟随重排: %d", workJ2)
	}

	// 查询塌缩：该作品只出一行（bgm 主行）；source 筛选语义为
	// 「组内含此来源」——筛 anilist/mal 也应命中同一个 bgm 主行
	res, err := st.Search(ctx, SearchQuery{Keyword: "少女終末旅行", Types: []int{2}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || len(res.Items) != 1 || res.Items[0].ID != 352661 {
		t.Fatalf("塌缩后应只出 bgm 主行: total=%d", res.Total)
	}
	// 组内来源是 anilist/mal/bgm：筛任一来源都应命中同一主行（组语义）
	for _, src := range []string{SourceAniList, SourceMAL} {
		r2, err := st.Search(ctx, SearchQuery{Source: src, Keyword: "少女終末旅行"})
		if err != nil {
			t.Fatal(err)
		}
		if r2.Total != 1 || r2.Items[0].ID != 352661 {
			t.Fatalf("source=%s 应按组语义命中主行: total=%d", src, r2.Total)
		}
	}
}

// TestWorkMatchAmbiguous 歧义拒绝：两行 bgm 同名同年（不同作品），
// 扩展源新行标题同时命中 → 不合并、自成作品。
func TestWorkMatchAmbiguous(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()
	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		{ID: 500001, Type: 2, Name: "同名同年的两部", Date: "2020-04-01"},
		{ID: 500002, Type: 2, Name: "同名同年的两部", Date: "2020-05-01"},
	}, 0); err != nil {
		t.Fatal(err)
	}
	ext := Subject{Source: SourceAniList, SourceRef: "555", Type: 2, Name: "同名同年的两部", Date: "2020-06-01"}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{ext}, 0); err != nil {
		t.Fatal(err)
	}
	var work, self int64
	if err := pool.QueryRow(ctx, `SELECT work_id, id FROM subjects WHERE source='anilist' AND source_ref='555'`).Scan(&work, &self); err != nil {
		t.Fatal(err)
	}
	if work != self {
		t.Fatalf("歧义应拒绝合并（自成作品）: work=%d self=%d", work, self)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subjects WHERE work_id = 500001`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("既有作品不得被改动: %d", n)
	}
}

// TestPruneDissolveAnchoredGroup bgm 全量清理删除组锚点时，
// 作品组重指到最优扩展源行继续存活。
func TestPruneDissolveAnchoredGroup(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()
	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		{ID: 600001, Type: 2, Name: "锚点将被清理的作品", Date: "2021-01-01"},
	}, 7); err != nil {
		t.Fatal(err)
	}
	ext := Subject{Source: SourceAniList, SourceRef: "777", Type: 2, Name: "锚点将被清理的作品", Date: "2021-02-01"}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{ext}, 0); err != nil {
		t.Fatal(err)
	}
	var workExt int64
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE source='anilist' AND source_ref='777'`).Scan(&workExt); err != nil {
		t.Fatal(err)
	}
	if workExt != 600001 {
		t.Fatalf("扩展源行应已并入 bgm 作品: %d", workExt)
	}
	pruned, err := st.Prune(ctx, 8, []int{1, 2, 3, 4, 6})
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Fatalf("应清理未覆盖的 bgm 行 1 条: %d", pruned)
	}
	var workNew int64
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE source='anilist' AND source_ref='777'`).Scan(&workNew); err != nil {
		t.Fatal(err)
	}
	if workNew == 600001 {
		t.Fatal("锚点删除后组应重指到扩展源行")
	}
	var alive int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subjects`).Scan(&alive); err != nil {
		t.Fatal(err)
	}
	if alive != 1 {
		t.Fatalf("作品应由扩展源行继续存活: %d", alive)
	}
}

// TestRematchWorks 重新配对作品（覆盖存量回填根因）：无键且无标题集的行先保持
// 独立；完整回填（含 alt_titles）后重跑，启发式经回填的日文标题并进 bgm 作品；
// 确定性键回填走 ② 链；已在多源组的行保持不动（幂等）。
func TestRematchWorks(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()
	// bgm 行：日文名（bgm 的 name 多为日文原名）
	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		{ID: 700001, Type: 2, Name: "少女終末旅行", Date: "2017-10-06"},
	}, 0); err != nil {
		t.Fatal(err)
	}
	// anilist 行：模拟 0006 之前的存量（主名英文、ext_key 空、alt_titles 空）
	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		{Source: SourceAniList, SourceRef: "888", Type: 2, Name: "Girls' Last Tour", Date: "2017-10-06"},
	}, 0); err != nil {
		t.Fatal(err)
	}
	// mal 行：有键、罗马字标题（与 anilist 确定性键可互配）
	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		{Source: SourceMAL, SourceRef: "999", ExtKey: "999", Type: 2, Name: "Shoujo Shuumatsu Ryokou", Date: "2017-10-06"},
	}, 0); err != nil {
		t.Fatal(err)
	}
	stats, err := st.RematchWorks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Merged != 0 || stats.Independent != 2 {
		t.Fatalf("回填前重跑应保持独立（英文名碰不上 bgm 日文名，两行各自成组）: %+v", stats)
	}

	// 模拟 crawler 的完整回填（AniList.BackfillRecords → SetBackfill）：
	// 补 alt_titles（罗马字/日文/同义词）与确定性键
	if n, err := st.SetBackfill(ctx, SourceAniList, map[string]BackfillRecord{
		"888": {ExtKey: "999", AltTitles: []string{"Girls' Last Tour", "Shoujo Shuumatsu Ryokou", "少女終末旅行"}},
	}); err != nil || n != 1 {
		t.Fatalf("回填落库应更新 1 行: n=%d err=%v", n, err)
	}
	stats, err = st.RematchWorks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Merged != 2 {
		// anilist 行：② 确定性键命中 mal 组后 ③ 补充又对上 bgm（双重同意），
		// 三组一次并成组——计数上该行从独立组挪进了 bgm 组；
		// mal 行：处理到它时组锚点已重排为 bgm，work_id 跟随更新，同计一次合并。
		t.Fatalf("回填标题后重跑应把两行都并入 bgm 作品: %+v", stats)
	}
	// 组主行是 bgm（优先级最高），anilist/mal 行跟随
	var workB, workA, workJ int64
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE id = 700001`).Scan(&workB); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE source='anilist' AND source_ref='888'`).Scan(&workA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE source='mal' AND source_ref='999'`).Scan(&workJ); err != nil {
		t.Fatal(err)
	}
	if workB != 700001 || workA != 700001 || workJ != 700001 {
		t.Fatalf("三行应同组且主行为 bgm: bgm=%d anilist=%d mal=%d", workB, workA, workJ)
	}

	// 幂等：再跑一次，全部行已在多源组中，不再变动
	stats2, err := st.RematchWorks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Merged != 0 || stats2.AlreadyIn != 2 {
		t.Fatalf("重跑应幂等（两行都已在多源组）: %+v", stats2)
	}
}

// TestSettingsPriorityReRank 管理设置调整来源优先级后主行跟着重排：
// 默认序下 bgm 是主行；把 tmdb 调到 bgm 之前后，重新触达该组即翻转为 tmdb 主行。
func TestSettingsPriorityReRank(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()
	defer SetSourcePriorities(registry.Priorities()) // 测试结束恢复登记表默认

	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		{ID: 800001, Type: registry.TypeReal, Name: "优先级重排的作品", Date: "2019-07-01"},
	}, 0); err != nil {
		t.Fatal(err)
	}
	tmdb := Subject{Source: registry.SourceTMDB, SourceRef: "tv/555", Type: registry.TypeTVSeries,
		Name: "优先级重排的作品", NameCN: "优先级重排的作品", Date: "2019-08-01"}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{tmdb}, 0); err != nil {
		t.Fatal(err)
	}
	var work1 int64
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE id = 800001`).Scan(&work1); err != nil {
		t.Fatal(err)
	}
	if work1 != 800001 {
		t.Fatalf("默认优先级下主行应为 bgm 行: %d", work1)
	}

	// 设置把 tmdb 提到 bgm 之前；重新配对作品对既有组统一重排主行
	SetSourcePriorities(map[string]int{
		registry.SourceBGM:  50,
		registry.SourceTMDB: 5,
	})
	if _, _, err := st.UpsertSubjects(ctx, []Subject{tmdb}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RematchWorks(ctx); err != nil {
		t.Fatal(err)
	}
	var work2, work3 int64
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE id = 800001`).Scan(&work2); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT work_id FROM subjects WHERE source='tmdb' AND source_ref='tv/555'`).Scan(&work3); err != nil {
		t.Fatal(err)
	}
	if work2 != work3 || work3 == 800001 {
		t.Fatalf("设置优先级后主行应翻转为 tmdb 行: bgm→%d tmdb→%d", work2, work3)
	}
}
