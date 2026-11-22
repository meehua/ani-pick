-- 多数据源：subjects 增加 (source, source_ref) 身份列，bgm 存量回填；
-- 非 bgm 行的公开 id 走独立序列（与 bgm 现有 id 空间隔离）；crawl_runs 标注来源。

ALTER TABLE subjects ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'bgm';
ALTER TABLE subjects ADD COLUMN IF NOT EXISTS source_ref TEXT NOT NULL DEFAULT '';

-- bgm 行回填：source_ref = 原始 Bangumi id 的文本形式（幂等，只补空缺）
UPDATE subjects SET source_ref = id::text WHERE source = 'bgm' AND source_ref = '';

-- 身份唯一索引：(source, source_ref) 一条上游记录只对应一行
CREATE UNIQUE INDEX IF NOT EXISTS subjects_source_ref_idx ON subjects (source, source_ref);

-- 非 bgm 行的公开 id 序列：1e10 起，bgm 现有 id（最大千万级）永远撞不上
CREATE SEQUENCE IF NOT EXISTS subject_ext_id_seq START WITH 10000000000;

-- 按来源统计与筛选（来源内条目量级在千级，单列索引足够）
CREATE INDEX IF NOT EXISTS subjects_source_idx ON subjects (source);

-- crawl_runs 标注来源：合并运行里每个来源一行，管理面板可展示 per-source 最近状态
ALTER TABLE crawl_runs ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'bgm';
