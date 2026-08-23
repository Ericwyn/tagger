# Provider 封面磁盘缓存与配置恢复

## 本次实现

- Provider 配置页的“恢复默认配置”使用独立的紧凑危险按钮，不再继承会撑满容器的全宽 `danger-quiet` 样式；确认后调用 `POST /api/v1/providers/:id/reset`。
- Registry 的 reset 流程先调用策略的 `ResetConfig`，再清除 SQLite 中保存的覆盖配置和配置错误，保留启用/停用状态。服务端返回重新生成的字段描述，前端同步表单值，因此恢复默认会立即生效而不需要重启。
- 候选来源弹窗的选项高度提升为 70px，并保留列表容器自己的纵向滚动，长来源信息不会溢出鼠标滚动区域。

## 封面缓存策略

`internal/artwork.Cache` 位于 Provider 下载器和具体策略之间，所有服务端预览入口及批量写入候选封面共用同一实例：

1. 原始 HTTPS URL 只保留 scheme、host 和 path，去除所有 query 参数与 fragment。
2. 对规范化 URL 做 SHA-256，文件保存为 `<data-dir>/artwork-cache/<hash>.img`，临时文件写入完成后原子 rename。
3. 缓存命中仍会重新执行图片字节校验、格式解析、尺寸和像素上限检查；损坏文件按 miss 删除。
4. 默认 TTL 为 3 小时。启动时清理过期文件，运行期间每 90 分钟执行一次清理；使用文件修改时间作为跨平台的创建/刷新时间。
5. 同一规范化 URL 的并发冷请求通过 `singleflight` 合并。缓存目录不可写时只跳过缓存，不影响本次远程图片返回。

## 验证

- `internal/artwork/cache_test.go` 覆盖 URL 规范化、查询参数去重、磁盘命中、过期删除、临时文件清理。
- `go test ./...` 通过；前端 Vitest 106 个测试和 TypeScript lint 通过。
