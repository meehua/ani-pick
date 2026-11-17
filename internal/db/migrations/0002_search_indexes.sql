-- 检索用的补充索引：未上榜条目排在最后、名称模糊搜索

CREATE INDEX IF NOT EXISTS subjects_type_rank_order_idx
    ON subjects (type, ((rank = 0)), rank);

CREATE INDEX IF NOT EXISTS subjects_name_trgm_idx
    ON subjects USING GIN (name gin_trgm_ops);

CREATE INDEX IF NOT EXISTS subjects_name_cn_trgm_idx
    ON subjects USING GIN (name_cn gin_trgm_ops);
