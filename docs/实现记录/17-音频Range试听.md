# 音频 Range 试听

验证日期：2026-08-20

## 后端

新增 `GET /api/v1/tracks/:id/audio`：

- 只接受已索引曲目 ID，通过 `filewrite.Writer.OpenRead` 复用曲库根目录、相对路径和 symlink 安全校验。
- 返回 MP3 `audio/mpeg`、FLAC `audio/flac`、WAV `audio/wav`。
- 支持单个 HTTP byte range，返回 `206`、`Content-Range`、`Accept-Ranges` 和准确的 `Content-Length`；非法范围返回 `416`。
- 以曲目 revision 返回 ETag，`If-None-Match` 命中时返回 `304`，避免重复传输。
- 使用有限 body stream 并在发送完成后关闭文件，不把整首音乐读入内存。

## 前端

Inspector 的播放按钮在真实模式下创建带 revision query 的 `<audio>` 资源，继续复用现有波形视觉和播放状态；Mock 模式保留无需后端的交互预览。

## 验证

- Hertz server 测试覆盖 206 range、Content-Type、ETag、304 和 416。
- `TestAudioAPIWithCopiedTestMusic` 使用 TestMusic 的 MP3、FLAC 临时副本验证真实 TagLib 扫描后的音频流；原始语料不被修改。

## 边界

当前不做转码；浏览器不支持的编码由前端显示播放失败，原始文件仍可下载/由外部播放器播放。多范围请求、波形预计算和播放列表属于后续增强。
