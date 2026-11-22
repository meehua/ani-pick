-- 0007: 各源原始评分与作品合成评分分离。
-- raw_score / raw_rating_total 永远保存该来源自己的原始值（10 分制归一后），
-- 不再被任何后续写覆盖；作品组主行的 score / rating_total 改为按管理员配置的
-- 各源权重加权合成的组级值（检索排序与卡片展示用，物化保查询走索引）。
-- 回填：现有 score / rating_total 就是各源自己的归一值，原样搬进 raw_*。
ALTER TABLE subjects ADD COLUMN IF NOT EXISTS raw_score real NOT NULL DEFAULT 0;
ALTER TABLE subjects ADD COLUMN IF NOT EXISTS raw_rating_total int NOT NULL DEFAULT 0;

UPDATE subjects SET raw_score = score, raw_rating_total = rating_total;
