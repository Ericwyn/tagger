# OGG 与 Opus 格式支持

本次实现把 Ogg 容器接入完整曲库流程。`.ogg` 和 `.opus` 文件会被扫描、索引并统一记录为 `ogg` 格式；实际编码继续通过技术属性中的 `codec` 区分，常见值为 `VORBIS` 和 `OPUS`。

## 实现范围

- 扫描、增量发现、单曲重扫和目录探测识别 `.ogg`、`.opus`，扩展名匹配不区分大小写。
- 曲库 API 和前端增加 OGG 格式筛选。
- 标签、歌词 sidecar、嵌入封面和历史恢复沿用现有安全写入流程。
- 音频 Range 接口使用 `audio/ogg`，供浏览器内播放器试听。
- 底层继续使用内嵌的 go-taglib/TagLib-WASM，无需 FFmpeg、CGO 或系统 TagLib。

## 格式边界

OGG 是容器而不是单一编码。Tagger 的 `TrackFormat` 表示容器家族，`TrackProperties.Codec` 表示编码，因此 `.ogg` 与 `.opus` 都使用 `format=ogg`。本次没有按扩展名把 Opus 建成独立格式，也没有开放 `.oga` 或 `.spx`；新增扩展名应先加入真实文件读写、封面和播放器兼容性测试。

格式扩展名映射集中在 `domain.TrackFormatFromExtension`，受支持格式判断集中在 `TrackFormat.IsSupported`。未知扩展名会被明确拒绝，不再隐式回退为 FLAC。

## 验证

- 单元测试覆盖 `.ogg`、`.opus` 的扫描映射、目录计数、API 查询、写入白名单和音频 MIME。
- 集成测试会在测试音乐目录存在对应样本时，验证 Ogg Vorbis 与 Ogg Opus 的扫描、原始标签、Range 试听和歌词 sidecar。
- go-taglib v0.14.0 自带 Ogg Vorbis 样本，依赖测试覆盖标签、多值和 Unicode 往返；本次开发中另行验证了该样本的嵌入封面写入和读取。
