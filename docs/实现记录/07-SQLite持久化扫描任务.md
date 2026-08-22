# SQLite 持久化扫描任务

验证日期：2026-08-20

## 任务生命周期

快速扫描不再占用一个长时间 HTTP 请求。`POST /api/v1/libraries/:id/scans` 写入 SQLite 后立即返回 `202 Accepted` 和 job ID，状态依次为：

`waiting → running → succeeded / failed`

worker 使用单条 `UPDATE … WHERE id=(SELECT … LIMIT 1) RETURNING` 原子领取最早等待任务；即使以后增加多个 worker，也不会重复领取同一 job。任务进度、成功/失败数、详情、错误和各阶段时间均持久化。

启动时，数据库中遗留的 `running` 任务会恢复为 `waiting`。正常关闭期间若扫描收到 context cancel，Manager 同样把任务退回等待状态，而不是错误标记成永久失败。

当前首先接入 `scan` handler；`match`、`write` 已保留统一 kind 和 handler 注册边界，后续批量操作直接复用。

## API 与前端

- `POST /api/v1/libraries/:id/scans`：创建扫描任务。
- `GET /api/v1/jobs`：按创建时间倒序返回任务列表。
- `GET /api/v1/jobs/:id`：查询单任务状态。
- 曲库页创建任务后每 350ms 查询详情，终态后刷新曲库并显示索引数量。
- 任务中心在真实模式下每秒刷新 SQLite 快照，动态展示等待、执行、完成、部分完成和失败状态；Mock 模式仍用于独立前端开发。

## 验证

- migration v2 创建 jobs 表和状态/时间索引。
- Store 测试验证 FIFO 领取、running 恢复、列表与时间字段。
- Manager 测试验证 handler 进度持久化和成功终态。
- Server 测试验证 202 入队、异步完成、列表/详情 API 和真实曲库 rescan。
- 前端 API 测试验证扫描入队、任务列表、URL encoding 和详情查询。
- 前端任务页复用应用导航测试，类型检查覆盖新增 `failed` 状态。

进程级验收直接使用完整 `TestMusic`：入队请求立即返回 HTTP 202 和 `waiting` job，随后 worker 完成 24/24 首扫描并记录 `succeeded`。浏览器任务中心显示 `DURABLE QUEUE / SQLITE`、100% 进度、24 个成功和真实最新事件，控制台无错误。关闭服务并使用同一数据目录重启后，`GET /jobs` 仍返回同一 job ID 和完整完成状态。

## 当前边界

- 当前单进程只启动一个 worker，避免扫描同时竞争同一磁盘；schema 和领取语义支持后续配置并发度。
- 页面使用短轮询；SSE、取消、失败项重试与事件表在批量 match/write 阶段补齐。
- 尚未做相同曲库扫描任务去重，连续点击会按顺序执行多个任务。
