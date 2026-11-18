-- 默认浏览页 count 走窄索引：count(type + nsfw) 原本只能扫堆（3.4 万行约 8MB，
-- 2c1g 超售机上是每轮缓存过期都要付的 IO/CPU），(type, nsfw) 索引约 200KB，
-- index-only scan 直接收敛到毫秒级。抓取写入侧 nsfw/type 几乎不变，维护成本可忽略。

CREATE INDEX IF NOT EXISTS subjects_type_nsfw_idx
    ON subjects (type, nsfw);
