# 待办

> **全项目进度的唯一记录处。** `docs/`、`AGENTS.md`、README 只描述已落地的
> 事实与决策，不散记待办。用法：完成即勾选，或挪到文末「已完成」（带提交号）。
> 最后更新：2026-11-22——v0.2.0 发版（多源接入与性能优化，见「已完成」）。

## 一、进行中

- [ ] **VPS 侧部署 v0.2.0**（镜像已由 CI 构建，待站主操作）：
  - [ ] 部署 `.env` 配置 `ANIPICK_CLIENT_IP_HEADER=CF-Connecting-IP` +
        `ANIPICK_TRUSTED_PROXY_CIDRS=<Cloudflare 网段>`（见 cloudflare.com/ips）；
  - [ ] VPS 拉取新镜像并重启；
  - [ ] 管理后台补 MAL Client ID（占位值会抓取失败）；
  - [ ] 跑几天后用管理页「性能」Tab 复位统计，对比前后路由耗时。

## 二、仓库级

- [ ] **游标分页（keyset）**：生产规模（约 500MB / 17 万条目）实测深分页第 5000 页
      约 225ms（E5 上更高）；现有 5s 页缓存 + 30s count 缓存兜底，规模再翻倍或
      深页访问变多时实施（`sort键+id` 游标，替代 OFFSET）；
- [ ] （可选）关键词 2 字 CJK count 约 141ms（生产规模，trgm 对 <3 字符无效）——
      30s 缓存兜底，如仍不够可对常见单字做预热；
- [ ] （可选）`collect`/`wish`/`doing` 排序观察使用率，低则删对应索引；
- [ ] （可选）SubjectPage 上游详情按 Tab 懒加载（浏览器直连上游，不耗 VPS，纯体验项）。

## 已完成

- **2026-11-22 v0.2.0 发版**（dev 提交 0d8512b..c91aa95）：多源接入与跨源合并
  （来源登记表/四源适配器/作品分组/匹配组）、真增量与空数据升级全量、内部参数
  全量设置化、管理后台自动保存与排序轨道、统计查询索引化与 meta/概览后台刷新
  （SWR）、详情只读库（简介抓取期入库，详情代理端点拆除）、迁移 0005–0009。
- **2026-11-18 生产规模实测加固**：本地库放大 5 倍（16.9 万条目 / 227 万关联，
  ≈440MB）全量 EXPLAIN 复核——TagSuggest 带类型改 trgm GIN 驱动（4 字查询 6.8ms
  且不随规模变化），抓取收尾 ANALYZE 升级 VACUUM ANALYZE（防 subject_tags 膨胀）；
  其余热路径（两段式首页 10.9ms / MetaTagSuggest 0.08ms / GetSubject 0.39ms）
  规模免疫确认。

- **2026-11-18 数据库查询深度优化**：排序索引补全（六个排序键的完整 ORDER BY 链
  含 id tie-break，删除被取代的五个旧索引）；单类型查询改直接等值（`type = ANY`
  会让规划器放弃索引序）；页查询两段式组装（LIMIT/OFFSET 作用于 id 窗口子查询，
  标签组装只对窗口内 24 行做 LATERAL）；`tags` 加全类型汇总列、`meta_tag_usage`
  预聚合表（TagSuggest/MetaTagSuggest 直查）；`Stats` 展示计数改 pg_class 估算；
  抓取后 ANALYZE 刷新统计。实测：搜索路由 avg 89ms→21.5ms、p95 143ms→43ms
  （含深分页第 5000 页，3.6s→约 40ms）；MetaTagSuggest 75ms→0.3ms。
- **2026-11-18 性能优化与管理面改进**：gzip 压缩（动态按需 + 静态产物压缩缓存）、
  会话校验进程内缓存与 last_seen 节流、热点查询 TTL 缓存（count/meta/meta-tags/overview）、
  独立查询并行化、管理端性能监测（每路由统计 + DB/gzip 链路计时 + 性能 Tab）、
  真实 IP 头 + 可信网段校验、前端路由懒加载（638KB→471KB，gzip 走线 153KB）与
  轮询可见性暂停、移除页面管理入口、compose 收紧 PG 连接与 Go 堆上限（1GB 机适配）、
  新增 AGENTS.md 门禁与 docs/EXPERIENCE.md 踩坑史。
