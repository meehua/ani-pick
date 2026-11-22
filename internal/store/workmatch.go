package store

// 作品分组匹配器：同一作品的各源行归入一组（work_id = 组内主行 id）。
//
// 三级匹配（upsert 路径与重新配对作品共用）：
//  1. (source, source_ref) 已存在 → 原组（由 resolveIDs 处理，不进本文件）；
//  2. 跨源确定性键 ext_key（mal/anilist 的 MAL id）相等 → 同作品；
//  3. 启发式：同类（kind）、年份 ±1、标题归一化后完全相等。
//
// 拿不准就拒绝：多个候选打平 → 源优先级归类；同级仍平且恰有一个作品的首播
// 日期贴近入站（同一次放播，容差可调）→ 并入；否则不合并，自成作品并记日志。
// 纯函数（NormalizeTitle / KindClassOf / pickWork）与 DB 查询薄壳（matchWork /
// recomputeGroupPrimary / mergeGroups / RematchWorks）分离，单测覆盖两半。

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"anipick/internal/registry"
)

// pgxTx 是匹配器可接受的句柄（Pool 事务或直连池）。
type pgxTx = pgx.Tx

// srcRank 是组内主行优先级的运行期快照（source → 越小越优先，同级取 id 最小）。
// 默认取登记表声明；管理设置的「数据源」区块经 SetSourcePriorities 立刷覆盖。
// 不变量：work_id 恒等于组内主行 id。
var srcRank atomic.Value // map[string]int

func init() {
	SetSourcePriorities(registry.Priorities())
}

// SetSourcePriorities 更新主行优先级快照（server 保存设置后调用）；
// 缺失或非法的来源回落登记表默认，未登记来源恒垫底。
func SetSourcePriorities(p map[string]int) {
	def := registry.Priorities()
	out := make(map[string]int, len(def))
	for id, d := range def {
		out[id] = d
	}
	for id, v := range p {
		if registry.Exists(id) && v > 0 {
			out[id] = v
		}
	}
	srcRank.Store(out)
}

// sourceRankOf 查当前快照里的来源优先级；未登记来源垫底（不可能成为主行）。
func sourceRankOf(source string) int {
	v, _ := srcRank.Load().(map[string]int)
	if r, ok := v[source]; ok {
		return r
	}
	return 1 << 30
}

// NormalizeTitle 归一化标题用于启发式比对：NFKC 折叠全角/半角，小写，
// 去掉全部空白与标点/符号，只留字母与数字（含各文字系统的字母）。
func NormalizeTitle(s string) string {
	if s == "" {
		return ""
	}
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TitleSetOf 汇总一行的候选标题集合（归一化、去空、去重）：name/name_cn 加 alt_titles。
func TitleSetOf(name, nameCN string, altTitles []string) []string {
	raw := make([]string, 0, 2+len(altTitles))
	raw = append(raw, name, nameCN)
	raw = append(raw, altTitles...)
	seen := map[string]struct{}{}
	set := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		n := NormalizeTitle(t)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		set = append(set, n)
	}
	return set
}

// KindClassOf 把 (source, type) 归入站点级分类（双层词表的外层）；成员划分由
// 来源登记表声明（各源自己声明哪些类型编码属于哪个 kind）。是否参与启发式合并
// 由 registry.Mergeable 决定（书籍/音乐/游戏没有跨源同物，不参与）。
func KindClassOf(source string, typ int) string {
	return string(registry.KindOf(source, typ))
}

// 启发式候选查询按它圈定可比对的行——业务 SQL 里不再出现类型码字面量。
// matchRecord 是匹配器视角的入站/存量记录。
type matchRecord struct {
	Source string
	Ref    string
	Name   string // 展示用原名（日志样本）
	ExtKey string
	Type   int
	Date   string // YYYY-MM-DD；空 = 无日期（启发式需要年份，跳过）
	Titles []string
}

// matchCandidate 是启发式的候选行（同 kind、年份窗口内、异源）。
type matchCandidate struct {
	ID     int64
	Source string
	WorkID int64
	Titles []string
	Date   string // YYYY-MM-DD（候选查询按年份窗口取，必有日期）
}

// pickWork 纯函数：标题归一化后完全相等的候选里取唯一 work_id。
// 0 个候选 → (0, false) 自成作品；多个不同 work_id 打平 → 先按源优先级归类，
// 同级仍打平且恰有一个作品的首播日期贴近入站（同一次放播，容差可调）→ 并入，
// 否则 → (0, true) 拒绝合并。
func pickWork(in matchRecord, candidates []matchCandidate) (workID int64, ambiguous bool, reason string) {
	inSet := map[string]struct{}{}
	for _, n := range TitleSetOf("", "", in.Titles) {
		inSet[n] = struct{}{}
	}
	if len(inSet) == 0 {
		return 0, false, ""
	}
	uniq := map[int64]struct{}{}
	var hit int64
	var hitRows int
	for _, c := range candidates {
		matched := false
		for _, n := range c.Titles {
			if _, ok := inSet[n]; ok {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		hitRows++
		hit = c.WorkID
		uniq[c.WorkID] = struct{}{}
	}
	switch len(uniq) {
	case 0:
		return 0, false, ""
	case 1:
		return hit, false, ""
	default:
		// 多个作品打平：按源优先级归类（组内主行优先序，越小越优先）——
		// 最高优先级唯一则并入该作品；最高级仍打平（同级多作品）宁缺毋滥拒绝。
		pri := map[int64]int{}
		for _, c := range candidates {
			if _, ok := uniq[c.WorkID]; !ok {
				continue
			}
			if p, ok := pri[c.WorkID]; !ok || sourceRankOf(c.Source) < p {
				pri[c.WorkID] = sourceRankOf(c.Source)
			}
		}
		bestPri := -1
		bestWork := int64(0)
		bestCount := 0
		for w, p := range pri {
			if bestPri == -1 || p < bestPri {
				bestPri, bestWork, bestCount = p, w, 1
			} else if p == bestPri {
				bestCount++
			}
		}
		if bestCount == 1 {
			return bestWork, false, fmt.Sprintf("「%s」在 %d 个作品上打平，按源优先级归类", in.Name, len(uniq))
		}
		// 同级打平（如相邻季标题剥标点后相同）：用精确放送日期决胜。两源都提供
		// 首播日期且候选查询按年份窗口取行（候选必有日期）——恰有一个打平作品
		// 的首播日期与入站贴近（同一次放播，容差可调，推荐 14 天）→ 并入；日期
		// 差一个月以上不构成同放播证据（同名同年的两部作品），宁缺毋滥拒绝。
		type dated struct {
			work  int64
			delta int
		}
		var dateds []dated
		for _, c := range candidates {
			if _, ok := uniq[c.WorkID]; !ok || pri[c.WorkID] != bestPri || c.Date == "" || in.Date == "" {
				continue
			}
			d, err := dayDiff(in.Date, c.Date)
			if err != nil {
				continue
			}
			found := false
			for i := range dateds {
				if dateds[i].work == c.WorkID {
					if d < dateds[i].delta {
						dateds[i].delta = d
					}
					found = true
					break
				}
			}
			if !found {
				dateds = append(dateds, dated{c.WorkID, d})
			}
		}
		if len(dateds) == bestCount {
			var close int64
			closeCount := 0
			for _, it := range dateds {
				if it.delta <= matchDateTiebreakDays {
					closeCount++
					close = it.work
				}
			}
			if closeCount == 1 {
				return close, false, fmt.Sprintf("「%s」在 %d 个同级作品上打平，按放送日期贴近归类", in.Name, len(uniq))
			}
		}
		return 0, true, fmt.Sprintf("「%s」在 %d 个同级作品上打平（命中 %d 行）",
			in.Name, len(uniq), hitRows)
	}
}

// dayDiff 两个 YYYY-MM-DD 的天数差（绝对值）；解析失败返回错误。
func dayDiff(a, b string) (int, error) {
	da, err := time.Parse("2006-01-02", a)
	if err != nil {
		return 0, err
	}
	db, err := time.Parse("2006-01-02", b)
	if err != nil {
		return 0, err
	}
	d := int(da.Sub(db).Hours() / 24)
	if d < 0 {
		d = -d
	}
	return d, nil
}

// matchWork 为一条「新」入站记录决定归属组：先确定性键，再标题启发式。
// ② 命中后再跑一次 ③ 补充（排除已同意的组）：确定性键 + 启发式双重同意时，
// 更高优先级的组（如 bgm）一并并入；③ 补充歧义则只保守保留 ② 组——
// 那些组等待对应来源侧重抓取时经 upsert 路径加入。
// 返回 0 表示自成作品；返回的 ambiguous 只在纯启发式路径出现（② 命中时恒 false）。
func matchWork(ctx context.Context, tx pgxTx, in matchRecord) (workID int64, ambiguous bool, err error) {
	// ② 跨源确定性键：mal_id 相等即同作品。多组同时命中 = 分组模型落地前的
	// 历史遗留（如 anilist 与 mal 各自成组），确定性键允许直接并成一组。
	target := int64(0)
	if in.ExtKey != "" {
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT work_id FROM subjects
			WHERE ext_key = $1 AND ext_key <> '' AND source <> $2 AND work_id > 0`,
			in.ExtKey, in.Source)
		if err != nil {
			return 0, false, err
		}
		var hits []int64
		for rows.Next() {
			var w int64
			if err := rows.Scan(&w); err != nil {
				rows.Close()
				return 0, false, err
			}
			hits = append(hits, w)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, false, err
		}
		switch len(hits) {
		case 0:
			// 落到启发式
		case 1:
			target = hits[0]
		default:
			target, err = mergeGroups(ctx, tx, hits)
			if err != nil {
				return 0, false, err
			}
		}
	}

	// ③ 启发式：同类 + 年份窗口 + 标题归一化完全相等。候选按 kind+年份窗口取，
	// 内存比对；同源行绝不互并（同源同名同年可能是不同作品，宁缺毋滥）。
	// 成员划分与年份窗口都来自来源登记表；不可跨源比对的分类（书籍/音乐/游戏）
	// 不进候选查询，直接自成作品。
	year := 0
	if len(in.Date) >= 4 {
		year, _ = strconv.Atoi(in.Date[:4])
	}
	// 候选集合由匹配组决定（多对多，管理设置可调）：锚点不在任何组 → 无候选
	// 自成作品（书籍/音乐/游戏默认不在组内）。
	if year <= 0 {
		return target, false, nil
	}
	candidates, err := candidatesFor(ctx, tx, in, year)
	if err != nil {
		return 0, false, err
	}
	if target > 0 {
		// ② 已定组：③ 作为补充——排除已同意的组后，若仍恰好命中唯一的其他组
		//（如 bgm），确定性 + 启发式双重同意，并入；歧义只保留 ② 组。
		agreed := map[int64]struct{}{target: {}}
		filtered := make([]matchCandidate, 0, len(candidates))
		for _, c := range candidates {
			if _, ok := agreed[c.WorkID]; ok {
				continue
			}
			filtered = append(filtered, c)
		}
		w, amb, reason := pickWork(in, filtered)
		if amb {
			slog.Warn("作品匹配补充歧义，保守保留确定性键组", "source", in.Source, "ref", in.Ref, "reason", reason)
			return target, false, nil
		}
		if w > 0 && w != target {
			merged, err := mergeGroups(ctx, tx, []int64{target, w})
			if err != nil {
				return 0, false, err
			}
			return merged, false, nil
		}
		return target, false, nil
	}
	workID, ambiguous, reason := pickWork(in, candidates)
	if ambiguous {
		slog.Warn("作品匹配歧义，放弃合并", "source", in.Source, "ref", in.Ref, "reason", reason)
	}
	return workID, ambiguous, nil
}

// matchYearWindow / rematchBatch / matchDateTiebreakDays / matchGroups 是合并
// 判定的运行期参数（原为登记表常量与函数内常量，现由管理设置立刷：
// SetMatchTunables/SetMatchTypeGroups）。
// 年份容差改小更严（0 = 必须同年），改大更容易误并，默认 1；
// 日期贴近容差改小更严，默认 14 天（同一次放播的经验宽度）。
var (
	matchYearWindow       = registry.MatchYearWindow
	rematchBatch          = 100
	matchDateTiebreakDays = registry.MatchDateTiebreakDays
	matchGroups           = registry.MatchGroupsDefault()
)

// SetMatchTunables 立刷合并判定参数（保存设置后由快照链路调用）。
func SetMatchTunables(yearWindow, batch, dateDays int) {
	if yearWindow >= 0 && yearWindow <= 5 {
		matchYearWindow = yearWindow
	}
	if batch > 0 {
		rematchBatch = batch
	}
	if dateDays > 0 && dateDays <= 60 {
		matchDateTiebreakDays = dateDays
	}
}

// SetMatchTypeGroups 立刷多对多类型匹配组（保存设置后由快照链路调用；
// nil/空回落登记表推荐默认）。
func SetMatchTypeGroups(groups [][]registry.TypeRef) {
	if len(groups) > 0 {
		matchGroups = groups
	} else {
		matchGroups = registry.MatchGroupsDefault()
	}
}

// pairablesOf 汇总锚点 (来源, 类型) 所属全部匹配组的成员（排除锚点自己的来源
// ——同源不互并；跨组去重）。多对多语义：同一类型可属于多个组，候选取并集。
// 返回空 = 锚点不在任何组里，自成作品。
func pairablesOf(source string, typ int) ([]string, []int) {
	var srcs []string
	var codes []int
	seen := map[string]struct{}{}
	for _, g := range matchGroups {
		anchorIn := false
		for _, m := range g {
			if m.Source == source && m.Type == typ {
				anchorIn = true
				break
			}
		}
		if !anchorIn {
			continue
		}
		for _, m := range g {
			if m.Source == source {
				continue
			}
			key := m.Source + ":" + strconv.Itoa(m.Type)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			srcs = append(srcs, m.Source)
			codes = append(codes, m.Type)
		}
	}
	return srcs, codes
}

// candidatesFor 取启发式候选行：匹配组声明的可配对 (来源, 类型)、年份窗口、异源。
func candidatesFor(ctx context.Context, tx pgxTx, in matchRecord, year int) ([]matchCandidate, error) {
	srcs, codes := pairablesOf(in.Source, in.Type)
	w := matchYearWindow
	rows, err := tx.Query(ctx, `
		SELECT id, source, work_id, name, name_cn, alt_titles, COALESCE(to_char(date, 'YYYY-MM-DD'), '')
		FROM subjects
		WHERE source <> $1
			AND date >= make_date($2, 1, 1) AND date <= make_date($3, 12, 31)
			AND (source, type) IN (SELECT * FROM unnest($4::text[], $5::int[]))
		ORDER BY id
		LIMIT 5000`,
		in.Source, year-w, year+w, srcs, codes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []matchCandidate
	for rows.Next() {
		var c matchCandidate
		var name, nameCN string
		var alt []string
		if err := rows.Scan(&c.ID, &c.Source, &c.WorkID, &name, &nameCN, &alt, &c.Date); err != nil {
			return nil, err
		}
		c.Titles = TitleSetOf(name, nameCN, alt)
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// recomputeGroupPrimary 重算一组的主行（源优先级高者，同级取 id 小），
// 整组 work_id 单条 UPDATE 重算；返回新的主行 id。
func recomputeGroupPrimary(ctx context.Context, tx pgxTx, workID int64) (int64, error) {
	rows, err := tx.Query(ctx, `SELECT id, source FROM subjects WHERE work_id = $1`, workID)
	if err != nil {
		return workID, err
	}
	type member struct {
		id     int64
		source string
	}
	var members []member
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.id, &m.source); err != nil {
			rows.Close()
			return workID, err
		}
		members = append(members, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return workID, err
	}
	if len(members) == 0 {
		return workID, nil
	}
	primary := members[0]
	for _, m := range members[1:] {
		if sourceRankOf(m.source) < sourceRankOf(primary.source) ||
			(sourceRankOf(m.source) == sourceRankOf(primary.source) && m.id < primary.id) {
			primary = m
		}
	}
	if primary.id == workID {
		return workID, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE subjects SET work_id = $1 WHERE work_id = $2`, primary.id, workID); err != nil {
		return workID, err
	}
	return primary.id, nil
}

// mergeGroups 把若干组合并为一组，返回合并后的主行 id。
// 不变量：work_id = 组内主行 id，因此各组的来源看 id = work_id 那一行。
func mergeGroups(ctx context.Context, tx pgxTx, workIDs []int64) (int64, error) {
	if len(workIDs) == 0 {
		return 0, fmt.Errorf("mergeGroups: 空组列表")
	}
	target := workIDs[0]
	targetRank := sourceRankOf("")
	for _, w := range workIDs {
		var src string
		if err := tx.QueryRow(ctx, `SELECT source FROM subjects WHERE id = $1`, w).Scan(&src); err != nil {
			return 0, err
		}
		rank := sourceRankOf(src)
		if rank < targetRank || (rank == targetRank && w < target) {
			target = w
			targetRank = rank
		}
	}
	others := make([]int64, 0, len(workIDs))
	for _, w := range workIDs {
		if w != target {
			others = append(others, w)
		}
	}
	if len(others) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE subjects SET work_id = $1 WHERE work_id = ANY($2)`, target, others); err != nil {
			return 0, err
		}
	}
	return recomputeGroupPrimary(ctx, tx, target)
}

// RematchStats 是「重新配对作品」的结果汇总（管理面板展示/验收报告用）。
type RematchStats struct {
	Scanned     int      `json:"scanned"`     // 扫过的扩展源行
	AlreadyIn   int      `json:"already_in"`  // 已在多源组中，保持不动
	Merged      int      `json:"merged"`      // 本轮并入既有作品
	Independent int      `json:"independent"` // 无候选，保持独立
	Ambiguous   int      `json:"ambiguous"`   // 候选打平，拒绝合并
	GroupMoves  int      `json:"group_moves"` // 主行重排次数
	Samples     []string `json:"samples,omitempty"`
}

// RematchWorks 对全部扩展源行按匹配器重新归组（管理端「重新配对作品」）。
// 只重新评估仍自成作品的行：已并入多源组的行保持不动（结果稳定、可重复执行）。
func (s *Store) RematchWorks(ctx context.Context) (RematchStats, error) {
	var stats RematchStats

	rows, err := s.Pool.Query(ctx, `
		SELECT id, source, source_ref, ext_key, type, name, name_cn, alt_titles,
			COALESCE(to_char(date, 'YYYY-MM-DD'), '')
		FROM subjects WHERE source <> $1 ORDER BY id`, SourceBGM)
	if err != nil {
		return stats, err
	}
	type extRow struct {
		id     int64
		workID int64
		rec    matchRecord
	}
	var pending []extRow
	for rows.Next() {
		var r extRow
		var name, nameCN string
		var alt []string
		if err := rows.Scan(&r.id, &r.rec.Source, &r.rec.Ref, &r.rec.ExtKey, &r.rec.Type,
			&name, &nameCN, &alt, &r.rec.Date); err != nil {
			rows.Close()
			return stats, err
		}
		r.rec.Name = name
		r.rec.Titles = TitleSetOf(name, nameCN, alt)
		pending = append(pending, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return stats, err
	}
	// work_id 批量补查（rows 循环里少一列扫描）
	ids := make([]int64, 0, len(pending))
	for i := range pending {
		ids = append(ids, pending[i].id)
	}
	work, err := s.workIDsOf(ctx, ids)
	if err != nil {
		return stats, err
	}
	for i := range pending {
		pending[i].workID = work[pending[i].id]
	}

	// 已在多源组的行直接跳过（一次聚合查询拿全量分组事实，免逐行查询）
	multi, err := s.multiSourceWorks(ctx)
	if err != nil {
		return stats, err
	}

	batchSize := rematchBatch
	var tx pgx.Tx
	rollback := func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
			tx = nil
		}
	}
	defer rollback()

	for i, r := range pending {
		if i%batchSize == 0 {
			rollback()
			tx, err = s.Pool.Begin(ctx)
			if err != nil {
				return stats, err
			}
		}
		stats.Scanned++
		if multi[r.workID] {
			stats.AlreadyIn++
		} else {
			workID, ambiguous, merr := matchWork(ctx, tx, r.rec)
			if merr != nil {
				return stats, merr
			}
			switch {
			case ambiguous:
				stats.Ambiguous++
				if len(stats.Samples) < 10 {
					stats.Samples = append(stats.Samples,
						fmt.Sprintf("%s %s《%s》", sourceLabelOf(r.rec.Source), r.rec.Ref, r.rec.Name))
				}
			case workID > 0 && workID != r.workID:
				// 并入既有组（matchWork 先到的行可能已被并入同一组）
				if _, err := tx.Exec(ctx, `UPDATE subjects SET work_id = $1 WHERE id = $2`, workID, r.id); err != nil {
					return stats, err
				}
				newPrimary, perr := recomputeGroupPrimary(ctx, tx, workID)
				if perr != nil {
					return stats, perr
				}
				if newPrimary != workID {
					stats.GroupMoves++
				}
				stats.Merged++
			default:
				stats.Independent++ // 无候选，或匹配回了本行自己的组
			}
		}
		if (i+1)%batchSize == 0 || i == len(pending)-1 {
			if err := tx.Commit(ctx); err != nil {
				tx = nil
				return stats, err
			}
			tx = nil
		}
	}

	// 主行优先级设置可能已调整：对所有多源组统一重算一次主行，
	// 顺序没变的组是空操作（recomputeGroupPrimary 原地返回）；翻组的计 GroupMoves。
	if len(multi) > 0 {
		tx, err = s.Pool.Begin(ctx)
		if err != nil {
			return stats, err
		}
		for w := range multi {
			newPrimary, perr := recomputeGroupPrimary(ctx, tx, w)
			if perr != nil {
				return stats, perr
			}
			if newPrimary != w {
				stats.GroupMoves++
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

// workIDsOf 批量补查各行的 work_id（id → work_id）。
func (s *Store) workIDsOf(ctx context.Context, ids []int64) (map[int64]int64, error) {
	out, err := s.Pool.Query(ctx, `SELECT id, work_id FROM subjects WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer out.Close()
	work := map[int64]int64{}
	for out.Next() {
		var id, w int64
		if err := out.Scan(&id, &w); err != nil {
			return nil, err
		}
		work[id] = w
	}
	return work, out.Err()
}

// multiSourceWorks 返回「含多个来源」的作品集合（work_id → true）。
func (s *Store) multiSourceWorks(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT work_id, count(DISTINCT source) FROM subjects GROUP BY work_id HAVING count(DISTINCT source) > 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var w int64
		var n int
		if err := rows.Scan(&w, &n); err != nil {
			return nil, err
		}
		out[w] = true
	}
	return out, rows.Err()
}

// GroupSource 是作品组内一个来源行的公开信息（详情页多源评分对比行）。
// Score/RatingTotal 是该来源自己的原始评分；Name/Platform/Date/Image 供页面上
// 区分同组的多行（如 AniList 的剧场版与 TV 各一行）。
type GroupSource struct {
	ID          int64   `json:"id"`
	Source      string  `json:"source"`
	Score       float64 `json:"score"`
	RatingTotal int     `json:"rating_total"`
	// Dist 是该行的评分分布（1~10 分十档人数；空 = 该源无分档数据）。
	Dist      []int32 `json:"dist,omitempty"`
	SourceRef string  `json:"-"`
	Name      string  `json:"name,omitempty"`
	NameCN    string  `json:"name_cn,omitempty"`
	Platform  string  `json:"platform,omitempty"`
	Date      string  `json:"date,omitempty"`
	Image     string  `json:"image,omitempty"`
	Summary   string  `json:"summary,omitempty"`
}

// GroupSources 返回一个条目所在作品组的全部来源行（多源评分对比/外链用）。
// 评分取各源自己的原始值（raw_*），与主行上的作品合成值口径分开；
// 按 sourceRank 排序（与资料优先序一致），一次查询取齐禁 N+1。
func (s *Store) GroupSources(ctx context.Context, id int64) ([]GroupSource, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT g.id, g.source, g.raw_score::float8, g.raw_rating_total, g.rating_dist, g.source_ref,
			g.name, g.name_cn, g.platform,
			COALESCE(to_char(g.date, 'YYYY-MM-DD'), ''), g.image, g.summary
		FROM subjects g
		WHERE g.work_id = COALESCE((SELECT NULLIF(work_id, 0) FROM subjects WHERE id = $1), $1)
		ORDER BY g.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupSource
	for rows.Next() {
		var g GroupSource
		if err := rows.Scan(&g.ID, &g.Source, &g.Score, &g.RatingTotal, &g.Dist, &g.SourceRef,
			&g.Name, &g.NameCN, &g.Platform, &g.Date, &g.Image, &g.Summary); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	GroupSourceSort(out)
	return out, nil
}

// GroupSourceSort 按主行优先级排序（登记表/设置声明的序，同级 id 小优先）。
func GroupSourceSort(gs []GroupSource) {
	for i := 1; i < len(gs); i++ {
		for j := i; j > 0; j-- {
			a, b := gs[j-1], gs[j]
			ra, rb := sourceRankOf(a.Source), sourceRankOf(b.Source)
			if ra > rb || (ra == rb && a.ID > b.ID) {
				gs[j-1], gs[j] = b, a
			} else {
				break
			}
		}
	}
}

func sourceLabelOf(source string) string {
	return registry.Label(source)
}

// ExtRefsMissingBackfill 返回某来源里缺回填的行（无确定性键或缺候选标题集）。
func (s *Store) ExtRefsMissingBackfill(ctx context.Context, source string) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT source_ref FROM subjects
		WHERE source = $1 AND (ext_key = '' OR alt_titles = '{}')
		ORDER BY id`, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// BackfillRecord 是一次存量回填的落库载荷（确定性键 + 候选标题集）。
type BackfillRecord struct {
	ExtKey    string
	AltTitles []string
}

// backfillAltSep 是回填载荷里标题集的连接分隔符（控制字符，正文不会出现）。
const backfillAltSep = "\x1f"

// SetBackfill 批量回写存量行的确定性键与候选标题集（来源上游一次取齐后落库）。
// 返回实际更新的行数；引用在上游查不到的行不出现在载荷里，保持原样。
func (s *Store) SetBackfill(ctx context.Context, source string, recs map[string]BackfillRecord) (int64, error) {
	if len(recs) == 0 {
		return 0, nil
	}
	refs := make([]string, 0, len(recs))
	keys := make([]string, 0, len(recs))
	alts := make([]string, 0, len(recs))
	for ref, rec := range recs {
		refs = append(refs, ref)
		keys = append(keys, rec.ExtKey)
		alts = append(alts, strings.Join(rec.AltTitles, backfillAltSep))
	}
	tag, err := s.Pool.Exec(ctx, `
		UPDATE subjects s SET ext_key = v.key,
			alt_titles = CASE WHEN v.alt = '' THEN '{}'::text[] ELSE string_to_array(v.alt, E'\x1f') END
		FROM unnest($2::text[], $3::text[], $4::text[]) AS v(ref, key, alt)
		WHERE s.source = $1 AND s.source_ref = v.ref`, source, refs, keys, alts)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
