package store

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"anipick/internal/db"
)

// 数据库集成测试（upsert 身份写入含 id 序列、source 过滤、Prune 隔离）。
//
// 本地库 ani_pick 与生产实例共享，且 ani_pick 用户无建库权限：
// 测试在共享库内创建独立 schema（search_path 指向它），迁移与数据全部落在
// 该 schema 下，与 public 里的线上数据完全隔离，结束后仅清空测试 schema。
// 设置 ANIPICK_TEST_DSN 后才会运行，例如：
//
//	ANIPICK_TEST_DSN='postgres://ani_pick:ani_pick@127.0.0.1:5432/ani_pick?sslmode=disable' go test ./internal/store/
const scratchSchema = "anipick_scratch"

func newScratchStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	base := os.Getenv("ANIPICK_TEST_DSN")
	if base == "" {
		t.Skip("未设置 ANIPICK_TEST_DSN，跳过数据库集成测试")
	}

	ctx := context.Background()
	// 建独立 schema（幂等）：迁移与数据全部落在其中，不触碰 public
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	if _, err := conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+scratchSchema); err != nil {
		conn.Close(ctx)
		t.Skipf("无法创建测试 schema（权限不足）: %v", err)
	}
	conn.Close(ctx)

	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	pool, err := pgxpool.New(ctx, base+sep+"search_path="+scratchSchema+",public")
	if err != nil {
		t.Fatalf("建连接池失败: %v", err)
	}
	t.Cleanup(pool.Close)

	// 安全护栏：current_schema 必须是测试 schema，否则立即终止（绝不在 public 上跑）
	var cur string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&cur); err != nil {
		t.Fatal(err)
	}
	if cur != scratchSchema {
		t.Fatalf("search_path 未生效（current_schema=%s），拒绝在非测试 schema 上运行", cur)
	}

	// 迁移可重复执行：连跑两次，第二次必须 no-op
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("迁移(第一次): %v", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("迁移(第二次,应幂等): %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE subjects, tags, subject_tags, tag_usage, meta_tag_usage, crawl_runs CASCADE`); err != nil {
		t.Fatalf("清空测试 schema: %v", err)
	}
	return New(pool, nil), pool
}

// TestUpsertIdentity 身份写入：bgm 行保持原 id 并回填 source_ref；
// 扩展源按 (source, source_ref) 稳定映射，id 从 1e10 序列分配。
func TestUpsertIdentity(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()

	// bgm：id 即来源主键，source_ref 回填
	ins, upd, err := st.UpsertSubjects(ctx, []Subject{{ID: 42, Type: 2, Name: "bgm 条目"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ins != 1 || upd != 0 {
		t.Fatalf("bgm 首写应为 insert, 实际 ins=%d upd=%d", ins, upd)
	}
	var src, ref string
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id, source, source_ref FROM subjects WHERE id = 42`).Scan(&id, &src, &ref); err != nil {
		t.Fatal(err)
	}
	if id != 42 || src != SourceBGM || ref != "42" {
		t.Fatalf("bgm 身份异常: id=%d source=%s ref=%s", id, src, ref)
	}

	// 扩展源首写：从扩展序列取号（>= 1e10）
	rec := Subject{Source: SourceTMDB, SourceRef: "movie/1", Type: TypeMovie, Name: "TMDB 电影", Score: 8.1}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{rec}, 0); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM subjects WHERE source = 'tmdb' AND source_ref = 'movie/1'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id < 10000000000 {
		t.Fatalf("扩展 id 应 >= 1e10, 实际 %d", id)
	}

	// 同一 (source, source_ref) 再写：id 稳定，内容更新（insert→update）
	rec2 := Subject{Source: SourceTMDB, SourceRef: "movie/1", Type: TypeMovie, Name: "TMDB 电影改", Score: 8.9, RatingTotal: 55}
	ins, upd, err = st.UpsertSubjects(ctx, []Subject{rec2}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ins != 0 || upd != 1 {
		t.Fatalf("重复写应为 update, 实际 ins=%d upd=%d", ins, upd)
	}
	var name string
	var score float64
	if err := pool.QueryRow(ctx, `SELECT id, name, score::float8 FROM subjects WHERE source = 'tmdb' AND source_ref = 'movie/1'`).Scan(&id, &name, &score); err != nil {
		t.Fatal(err)
	}
	if id < 10000000000 || name != "TMDB 电影改" || score != 8.9 {
		t.Fatalf("更新后异常: id=%d name=%q score=%v", id, name, score)
	}

	// 不同 source_ref 各自取号；同一 ref 在不同 source 下互不冲突
	other := Subject{Source: SourceTMDB, SourceRef: "movie/2", Type: TypeMovie, Name: "另一部"}
	otherSrc := Subject{Source: SourceMAL, SourceRef: "movie/1", Type: 2, Name: "mal 同 ref"}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{other, otherSrc}, 0); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subjects`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("应有 4 行（bgm1 + tmdb2 + mal1）, 实际 %d", n)
	}

	// GetSubject 回读 source
	sub, err := st.GetSubject(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Source != SourceTMDB || sub.Name != "TMDB 电影改" {
		t.Fatalf("GetSubject 回读异常: %+v", sub)
	}

	// bgm 缺 source_ref 时按 ID 补齐（convert 的默认路径）
	if _, _, err := st.UpsertSubjects(ctx, []Subject{{ID: 100, Type: 1, Name: "书籍"}}, 0); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT source_ref FROM subjects WHERE id = 100`).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	if ref != "100" {
		t.Fatalf("bgm source_ref 应回填为 id 文本, 实际 %q", ref)
	}

	// 空 SourceRef 的扩展源记录必须被拒绝（身份不完整）
	if _, _, err := st.UpsertSubjects(ctx, []Subject{{Source: SourceTMDB, Type: TypeMovie, Name: "缺 ref"}}, 0); err == nil {
		t.Fatal("扩展源缺 source_ref 应报错")
	}
}

// TestSearchSourceFilter source 入参过滤与总数缓存互不串味。
func TestSearchSourceFilter(t *testing.T) {
	st, _ := newScratchStore(t)
	ctx := context.Background()
	recs := []Subject{
		{ID: 1, Type: 2, Name: "bgm 动画"},
		{Source: SourceMAL, SourceRef: "11", Type: 2, Name: "MAL 动画"},
		{Source: SourceTMDB, SourceRef: "movie/9", Type: TypeMovie, Name: "电影"},
	}
	if _, _, err := st.UpsertSubjects(ctx, recs, 0); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		source string
		types  []int
		total  int
	}{
		{"", []int{2}, 2},         // 全来源动画
		{SourceBGM, []int{2}, 1},  // 只 bgm
		{SourceMAL, nil, 1},       // 只 mal
		{SourceTMDB, nil, 1},      // 只 tmdb
		{SourceAniList, nil, 0},   // 未抓取的来源
		{SourceTMDB, []int{2}, 0}, // tmdb 无动画
	}
	for _, c := range cases {
		res, err := st.Search(ctx, SearchQuery{Source: c.source, Types: c.types})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != c.total {
			t.Errorf("source=%q types=%v total=%d, 期望 %d", c.source, c.types, res.Total, c.total)
		}
	}
}

// TestPruneSourceIsolation bgm 全量清理不得波及同类型的扩展源行；过期重爬只看 bgm。
func TestPruneSourceIsolation(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()
	// 上一轮（run 6）遗留一条 bgm 旧行；本轮（run 7）覆盖另一条 bgm 与一条扩展源行
	if _, _, err := st.UpsertSubjects(ctx, []Subject{{ID: 2, Type: 1, Name: "bgm 未覆盖将被清理"}}, 6); err != nil {
		t.Fatal(err)
	}
	recs := []Subject{
		{ID: 1, Type: 2, Name: "bgm 本轮覆盖"},
		{Source: SourceMAL, SourceRef: "99", Type: 2, Name: "MAL 动画"}, // 同为类型 2
	}
	if _, _, err := st.UpsertSubjects(ctx, recs, 7); err != nil {
		t.Fatal(err)
	}
	pruned, err := st.Prune(ctx, 7, []int{1, 2, 3, 4, 6})
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Fatalf("应只清理未覆盖的 bgm 行 1 条, 实际 %d", pruned)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subjects WHERE source = 'mal'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("扩展源行必须存活, 实际 %d 行", n)
	}

	// StaleSubjects / CountStale 只算 bgm：把扩展源行 fetched_at 拨老
	if _, err := pool.Exec(ctx, `UPDATE subjects SET fetched_at = now() - interval '100 days' WHERE source = 'mal'`); err != nil {
		t.Fatal(err)
	}
	stale, err := st.StaleSubjects(ctx, 30, 5, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("过期重爬不应包含扩展源行: %v", stale)
	}
	cnt, err := st.CountStale(ctx, 30, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("过期统计不应包含扩展源行: %d", cnt)
	}
}

// TestRecomputeMetaTagsOnlyBGM 元标签聚合只算 bgm 原生类型（type<100）。
func TestRecomputeMetaTagsOnlyBGM(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()
	recs := []Subject{
		{ID: 5, Type: 2, Name: "bgm 动画", MetaTags: []string{"TV", "日本"}},
		{Source: SourceTMDB, SourceRef: "movie/77", Type: TypeMovie, Name: "电影"},
	}
	if _, _, err := st.UpsertSubjects(ctx, recs, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.RecomputeTagUsage(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM meta_tag_usage`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("元标签聚合应只含 bgm 行（TV/日本）, 实际 %d", n)
	}
}

// TestTagUsageCrossSource 标签热度跨源按类型聚合（扩展源标签可作筛选候选）。
func TestTagUsageCrossSource(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()
	recs := []Subject{
		{ID: 6, Type: 2, Name: "bgm 动画", Tags: []TagVote{{Name: "科幻", Votes: 10}}},
		{Source: SourceMAL, SourceRef: "12", Type: 2, Name: "MAL 动画", Tags: []TagVote{{Name: "热血", Votes: 0}}},
	}
	if _, _, err := st.UpsertSubjects(ctx, recs, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.RecomputeTagUsage(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_usage WHERE type = 2`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("类型 2 的标签热度应跨源聚合（科幻/热血）, 实际 %d", n)
	}
	res, err := st.Search(ctx, SearchQuery{Tags: []string{"热血"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Fatalf("扩展源标签应可用于筛选, total=%d", res.Total)
	}
}
