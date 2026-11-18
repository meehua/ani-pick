-- 查询性能：排序索引补全（完整 ORDER BY 链含 id tie-break）+ 标签/元标签预聚合
-- 2026-11-18 EXPLAIN 实测（3.4 万条目 / 45 万关联）：
--   排序页查询全部 Seq Scan + top-N 排序（82-125ms）——旧索引缺 tie-break 列，
--   ORDER BY (rank=0) ASC, rank ASC, id ASC 这类三键链不命中两列索引；
--   深分页第 5000 页 3.6s——OFFSET 前每行都做 LATERAL 标签组装（应用层两段式解决）；
--   TagSuggest 43ms / MetaTagSuggest 75ms / Stats 的 subject_tags 计数 41-96ms。

-- 1) 各排序键的完整索引（type 过滤 + 排序键 + id tie-break），取代旧的部分链索引
CREATE INDEX IF NOT EXISTS subjects_type_rank_full_idx
    ON subjects (type, ((rank = 0)), rank, id);
CREATE INDEX IF NOT EXISTS subjects_type_score_full_idx
    ON subjects (type, score DESC, rating_total DESC, id ASC);
CREATE INDEX IF NOT EXISTS subjects_type_rating_full_idx
    ON subjects (type, rating_total DESC, id ASC);
CREATE INDEX IF NOT EXISTS subjects_type_date_full_idx
    ON subjects (type, date DESC NULLS LAST, id ASC);
CREATE INDEX IF NOT EXISTS subjects_type_collect_idx
    ON subjects (type, collect DESC, id ASC);
CREATE INDEX IF NOT EXISTS subjects_type_wish_idx
    ON subjects (type, wish DESC, id ASC);
CREATE INDEX IF NOT EXISTS subjects_type_doing_idx
    ON subjects (type, doing DESC, id ASC);

DROP INDEX IF EXISTS subjects_type_rank_idx;        -- 0001，被 rank_full 取代
DROP INDEX IF EXISTS subjects_type_rank_order_idx;  -- 0002，被 rank_full 取代
DROP INDEX IF EXISTS subjects_type_score_idx;       -- 0001，被 score_full 取代
DROP INDEX IF EXISTS subjects_type_rating_idx;      -- 0001，被 rating_full 取代
DROP INDEX IF EXISTS subjects_type_date_idx;        -- 0001，被 date_full 取代

-- 2) tags 全类型汇总列：TagSuggest 无类型过滤时直查本表（原为 tag_usage 全量聚合）
ALTER TABLE tags ADD COLUMN subject_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tags ADD COLUMN votes BIGINT NOT NULL DEFAULT 0;
UPDATE tags t SET subject_count = u.c, votes = u.v
    FROM (SELECT tag_id, sum(subject_count)::int AS c, sum(votes)::bigint AS v
          FROM tag_usage GROUP BY tag_id) u
    WHERE t.id = u.tag_id;

-- 3) 元标签按类型预聚合：MetaTagSuggest 从扫 subjects unnest 改为查本表
CREATE TABLE IF NOT EXISTS meta_tag_usage (
    type  SMALLINT NOT NULL,
    name  TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (type, name)
);
INSERT INTO meta_tag_usage (type, name, count)
    SELECT s.type, m, count(*)::int FROM subjects s, unnest(s.meta_tags) AS m GROUP BY s.type, m;
