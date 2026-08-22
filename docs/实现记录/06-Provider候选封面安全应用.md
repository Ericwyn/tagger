# Provider 候选封面安全应用

验证日期：2026-08-20

## 交互与信任边界

候选搜索结果只向前端暴露 `hasArtwork`，不暴露远程 URL。Registry 在内存中保存候选 ID 到 provider/URL 的映射，30 分钟后失效。前端应用封面时只提交：

- 当前曲目 ID。
- 刚刚搜索得到的候选 ID。
- 当前曲目 revision / `If-Match`。

后端不接受用户提交任意 URL。候选过期后返回 `candidate_artwork_expired`，要求重新搜索。

候选引用现在写入 SQLite `provider_artwork_refs` 并带 30 分钟 TTL；服务重启后可以恢复未过期的 candidate ID 到 Provider URL 映射，但最终下载仍会重新执行完整安全校验。

`POST /api/v1/matches/tracks/:id/artwork` 完成下载、图片校验和安全嵌入写入；成功后生成“采用数据源封面”修订，来源由后端 Registry 反查，客户端不能伪造。

候选标签和封面是两个各自原子的文件写入：先写标签并推进 revision，再用新 revision 写封面。若标签已成功但远程封面失败，前端明确提示“标签已写入，但候选封面应用失败”，不会误报整次操作未发生。

## SSRF 与下载防护

- 只允许 HTTPS，不接受 URL userinfo。
- MusicBrainz 只允许 Cover Art Archive / Internet Archive 官方域名。
- Apple 只允许 `mzstatic.com` 及其子域。
- 每次重定向重新验证 provider 域名，最多 5 次。
- 自定义 transport 禁用环境代理，解析 DNS 后拒绝 private、loopback、link-local、unspecified 等非公网地址。
- 连接时使用已经验证的解析结果，避免验证后再次解析造成 DNS rebinding。
- 总请求超时 15 秒、连接/TLS 超时 5 秒、响应上限 10 MiB、禁用压缩。
- 下载后仍执行 MIME 一致性、JPEG/PNG/WebP 真实解码和 40MP 上限校验。

## 测试覆盖

- Provider Registry 保存短期候选封面引用，不进入公开 JSON。
- URL allowlist 拒绝 HTTP、伪造后缀、未知 provider 和本地地址。
- IPv4/IPv6 private、loopback、link-local 判定。
- 使用自定义 `RoundTripper` 确定性验证请求头、响应图片解码和尺寸，不监听测试端口。
- 真实 MP3 API 集成测试使用注入下载器验证候选 ID → provider 来源 → 原子封面写入 → 历史审计 → SQLite 重开。
- 前端测试验证只有用户显式勾选封面时才提交 artwork 选项，以及请求路径、body、`If-Match` 和曲目 revision 推进。

真实网络验收使用 `心安之地 / 许嵩 / 安泊猜想` 查询 Apple：491ms 返回 2 个候选，首个候选得分 1.00 且有封面。客户端只提交 `cand-apple-...` ID；后端从受控 `mzstatic.com` URL 下载并验证 600×600 JPEG，将临时 MP3 中原 1200×1200 / 507717 bytes 封面替换为 129906 bytes 图片。API 重读 SHA-256 `e2d09408…` 与写入结果一致，历史来源为 `Apple / iTunes`，原始 `TestMusic` 文件 SHA-256 保持不变。

## 当前边界

- 候选引用仍是进程内 30 分钟缓存，服务重启后需重新搜索；通用 provider 响应缓存将在 SQLite cache 阶段实现。
- Provider 下载的原始图片不做重新编码，避免无提示改变质量；只做安全验证。
- 标签与封面不是跨文件事务，部分成功通过明确通知和各自历史记录暴露。
