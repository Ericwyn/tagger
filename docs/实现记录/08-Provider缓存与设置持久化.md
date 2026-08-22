# Provider 缓存与设置持久化

验证日期：2026-08-20

## SQLite 状态

Migration v3 新增：

- `provider_cache`：cache key、provider ID、原始候选 JSON、过期时间和更新时间。
- `provider_settings`：provider ID、启用状态、预留配置 JSON 和更新时间。

搜索缓存保存后端原始候选，包括只在服务端使用的封面 URL；公开 API 仍只返回候选 ID 和 `hasArtwork`。缓存命中后会重新生成评分视图和短期封面引用，因此服务重启后仍能安全应用缓存候选封面。

MusicBrainz 默认缓存 7 天，Apple/LRCLIB 默认 24 小时。启动时清理过期记录，读取时也用 `expires_at` 强制过滤，不能返回陈旧结果。缓存读取、反序列化或写入失败不会让整个多源查询失败，而是回退到对应策略。

## Provider 设置

- `PATCH /api/v1/providers/:id` 持久化启停状态。
- `POST /api/v1/providers/:id/test` 对单个已启用来源执行真实小查询，并返回延迟、候选数和缓存命中状态。
- Registry descriptor 叠加 SQLite 设置；停用后健康状态统一为 `disabled`，重新启用恢复策略自身健康状态。
- 尚未实现的网易云/酷我实验性占位器不能被伪装成已启用，返回 `provider_unavailable`。
- 设置页开关立即写入 SQLite，连接测试不再使用计时器模拟。

## 测试覆盖

- Store：TTL 命中、时间推进后过期、清理和启用状态读写。
- Registry：相同搜索只调用策略一次，第二次标记 `cached`；关闭/重开 SQLite 和 Registry 后仍命中；停用状态跨重启保留；缓存候选恢复封面引用。
- Server：停用、禁用来源连接测试、重新启用和真实策略测试响应。
- Frontend：Provider PATCH/test 路径、JSON body、缓存连接提示和稳定响应类型。

真实 Apple 验收：首次 `心安之地 / 许嵩 / 安泊猜想` 查询耗时 475ms；第二次使用额外空格/大小写等价查询耗时 0ms 且返回 `cached=true`。浏览器设置页将 Apple 停用后立即显示“未启用”，控制台无错误。关闭服务并用同一数据目录重启后 Apple 仍为 disabled；重新启用后同一搜索继续 0ms 命中重启前缓存，证明设置与候选缓存均跨进程保留。

## 当前边界

- 当前只持久化启用状态；Apple Music Developer Token、代理和实验性来源凭据需要加密 secret store 后再开放 UI。
- 尚未提供按 provider 清除缓存的管理按钮；过期清理由启动流程执行。
- 当前 key 按 provider、完整查询和 limit 生成；后续批量匹配阶段会加入 singleflight，合并同一时刻的并发冷缓存请求。
