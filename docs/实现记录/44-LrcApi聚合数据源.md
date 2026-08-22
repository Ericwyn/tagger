# LrcApi 聚合数据源

## 调研与边界

参考 [HisAtri/LrcApi](https://github.com/HisAtri/LrcApi) README 和公开实现：它提供 `/jsonapi` 歌词 JSON 接口及 `/cover` 封面接口，可部署在本地，也有公开实例。其 README 同时提示公共聚合接口可能较慢且结果不完全准确，因此 Tagger 将其作为默认关闭的实验性策略，不把它当作唯一来源，也不复制其 GPL-3.0 源码。

## 实现

- 新增 `internal/providers/lrcapi`，兼容数组、`data`/`results` 包装和单对象响应。
- 支持 `title`、`artist`、`album` 查询，统一清理歌词 BOM/换行，映射同步歌词、艺术家、专辑、时长和封面候选。
- 支持 `Authorization` 鉴权；默认地址可通过 `TAGGER_LRCAPI_URL`、`TAGGER_LRCAPI_COVER_URL` 和 `TAGGER_LRCAPI_AUTH` 覆盖。
- LrcApi 封面 URL 仍经过现有 provider allowlist、HTTPS/DNS/大小/MIME/解码校验；策略失败只记录该来源错误。

## 验证

- 自定义 RoundTripper 测试覆盖查询参数、鉴权、数组/包装响应、LRC 清理、时长解析和封面 URL。
- provider 全量测试与 `TestMusic` 集成测试通过。
