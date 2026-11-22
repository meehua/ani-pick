-- 0010: 评分分布入库（详情页各源评分对比行展示）。
-- bgm rating.count 与 anilist scoreDistribution 归一到 1~10 分十档人数；
-- 空数组 = 该源无分档数据（mal/tmdb/itunes 官方接口不提供分档统计）。
ALTER TABLE subjects ADD COLUMN IF NOT EXISTS rating_dist INTEGER[] NOT NULL DEFAULT '{}';
