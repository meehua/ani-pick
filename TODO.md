# 待办

> **全项目进度的唯一记录处。** `docs/`、`AGENTS.md`、README 只描述已落地的
> 事实与决策，不散记待办。用法：完成即勾选，或挪到文末「已完成」（带提交号）。
> 最后更新：2026-11-18——性能优化与管理面改进收口（工作分支 `dev`，`main` 回归发版态）。

## 一、进行中

- [ ] **生产部署性能优化成果**（dev 已验证，待合并 `main` 发版）：
  - [ ] 部署 `.env` 配置 `ANIPICK_CLIENT_IP_HEADER=CF-Connecting-IP` +
        `ANIPICK_TRUSTED_PROXY_CIDRS=<Cloudflare 网段>`（见 cloudflare.com/ips）；
  - [ ] 合并 `dev` → `main`，打 `v*` tag 触发镜像构建，VPS 拉取重启；
  - [ ] 部署后用管理页「性能」Tab 复位统计，对比前后路由耗时。

## 二、仓库级

- [ ] **数据库查询深度优化**（2026-11-18 EXPLAIN 实测 3.4 万条目/45 万关联）：
  - [ ] 排序索引补全：`rank`/`score`/`collect`/`wish`/`doing`/`date` 各排序的
        完整 ORDER BY 链（含 `id` tie-break）——实测当前全部 Seq Scan + top-N
        排序（82-125ms）；
  - [ ] 两段式组装：先按排序取 id 窗口（LIMIT/OFFSET 作用于纯索引扫描），
        再按这 24 个 id 组装 top-tags——实测深分页第 5000 页 3.6s，
        根因是 OFFSET 之前每行都在做 LATERAL 组装；
  - [ ] `TagSuggest`（43ms）与 `MetaTagSuggest`（75ms）预聚合/汇总列；
  - [ ] `Stats` 的 `subject_tags`/`tags` 计数（41-96ms）改估算或加缓存；
- [ ] （可选）前端游标分页（keyset）——两段式落地后深页已可接受，评估必要性再做；
- [ ] （可选）`collect`/`wish`/`doing` 排序观察使用率，低则删对应索引；
- [ ] （可选）SubjectPage 上游详情按 Tab 懒加载（浏览器直连上游，不耗 VPS，纯体验项）。

## 三、已完成

- **2026-11-18 性能优化与管理面改进**：gzip 压缩（动态按需 + 静态产物压缩缓存）、
  会话校验进程内缓存与 last_seen 节流、热点查询 TTL 缓存（count/meta/meta-tags/overview）、
  独立查询并行化、管理端性能监测（每路由统计 + DB/gzip 链路计时 + 性能 Tab）、
  真实 IP 头 + 可信网段校验、前端路由懒加载（638KB→471KB，gzip 走线 153KB）与
  轮询可见性暂停、移除页面管理入口、compose 收紧 PG 连接与 Go 堆上限（1GB 机适配）、
  新增 AGENTS.md 门禁与 docs/EXPERIENCE.md 踩坑史。
