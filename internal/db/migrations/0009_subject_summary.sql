-- 详情只读库：条目简介随抓取入库（各源抓取载荷本就携带简介，
-- 此前入库时丢弃、详情页访问时再由服务端代理去上游现取——走歪了）。
-- 访问路径从此只读本站索引数据；上游外链由登记表 URL 模板按 source_ref 拼出。
ALTER TABLE subjects ADD COLUMN IF NOT EXISTS summary TEXT NOT NULL DEFAULT '';
