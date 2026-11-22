-- 概览/公开元信息/浏览计数的统计查询改走「仅索引扫描」：
-- 作品塌缩条件（work_id = 0 OR id = work_id）无法命中普通索引，也不支持
-- 部分索引以外的仅索引扫描（运行时逐行回表读 work_id）——把它放进索引谓词，
-- 引用列全部随附在索引内，统计查询不再整表扫（生产 169k 行全表扫是
-- 管理概览每轮回源数百毫秒的主因）。
CREATE INDEX IF NOT EXISTS subjects_work_type_idx
    ON subjects (type, nsfw)
    WHERE work_id = 0 OR id = work_id;

-- 过期条目统计（管理概览 + 重爬队列）：按来源 + 抓取时间做范围扫描，
-- 其余过滤列随附索引内，保持仅索引扫描；顺带覆盖 CountStaleBySource。
CREATE INDEX IF NOT EXISTS subjects_source_fetched_idx
    ON subjects (source, fetched_at)
    INCLUDE (type, locked, fail_count);

-- 按源统计覆盖作品数（DISTINCT 塌缩 id）：引用列全部在索引内，免整表扫。
CREATE INDEX IF NOT EXISTS subjects_source_work_idx
    ON subjects (source) INCLUDE (id, work_id);
