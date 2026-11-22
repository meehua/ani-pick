-- 作品分组：subjects 增加 work_id（同一作品的行归为一组，work_id = 组内主行的 id）、
-- ext_key（跨源确定性键：jikan/anilist 的 mal_id）与 alt_titles（匹配用候选标题集）。
-- 行数据保持各源原样，查询塌缩到主行（id = work_id）。

ALTER TABLE subjects ADD COLUMN IF NOT EXISTS work_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE subjects ADD COLUMN IF NOT EXISTS ext_key TEXT NOT NULL DEFAULT '';
ALTER TABLE subjects ADD COLUMN IF NOT EXISTS alt_titles TEXT[] NOT NULL DEFAULT '{}';

-- 存量回填：每行先自成作品；jikan 的 source_ref 就是 mal_id，直接落 ext_key
UPDATE subjects SET work_id = id WHERE work_id = 0;
UPDATE subjects SET ext_key = source_ref WHERE source = 'jikan' AND ext_key = '';

-- 组内成员定位（重算主行/组内来源清单走 work_id 等值）
CREATE INDEX IF NOT EXISTS subjects_work_idx ON subjects (work_id);
-- 跨源确定性键匹配（部分索引：只存有键的行）
CREATE INDEX IF NOT EXISTS subjects_ext_key_idx ON subjects (ext_key) WHERE ext_key <> '';
