# 第三方声明

Tagger 的原创代码按仓库根目录 [`LICENSE`](LICENSE) 中的 MIT License 发布。本文件记录随项目构建或运行时使用、尤其是会被嵌入发布产物的主要第三方组件；这些组件不因 Tagger 的 MIT License 而重新授权。

## go.senan.xyz/taglib v0.14.0

- 用途：音频标签、音频属性和嵌入式封面的读写。
- 分发方式：其 `taglib.wasm` 会被嵌入 Tagger 的 Go 可执行文件。
- 许可证：GNU Lesser General Public License v2.1（LGPL-2.1）。
- 该版本内置 TagLib v2.1.1。
- 上游项目：[sentriz/go-taglib](https://github.com/sentriz/go-taglib)
- 上游许可证：[go-taglib LICENSE](https://github.com/sentriz/go-taglib/blob/master/LICENSE)

分发包含该组件的源码或二进制时，请保留上游版权和许可证声明，并按照 LGPL-2.1 提供许可证文本及该许可证要求的相应源代码或可重新链接形式。

## 其他依赖

Go 依赖列在 [`go.mod`](go.mod) 中，前端依赖列在 [`frontend/package.json`](frontend/package.json) 和 [`frontend/package-lock.json`](frontend/package-lock.json) 中。除 Tagger 自身代码外，所有依赖仍按各自上游项目的许可证授权；发布包应保留适用的上游版权、许可证和 NOTICE 文件。

## 外部内容和服务

音乐文件、歌词、封面、元数据以及 MusicBrainz、LRCLIB、Apple/iTunes、网易云、酷我、酷狗和其他数据源返回的内容，不属于本项目许可证授权的作品。使用这些内容或服务时，请遵守相应权利人的授权范围、服务条款和适用法律。
