# Tagger

Tagger 是一个使用 Go 实现的本地音乐元数据工作台。它扫描指定音乐目录，读取 MP3、FLAC、WAV 的标签和技术参数，并通过嵌入式 React 前端浏览、检查和编辑元数据。正式构建只有一个可执行文件，不要求目标机器安装 Node.js、FFmpeg 或系统 TagLib。

## 当前实现状态

已经可用：

- React 19 + TypeScript + Vite + Tailwind CSS 4 完整交互原型。
- Go/Hertz HTTP 服务和嵌入式前端，`make build` 产出单二进制。
- 使用 go-taglib（TagLib-WASM）并发扫描 MP3、FLAC、WAV。
- 读取标题、艺术家、专辑、音轨/光盘号、年份、风格、歌词、封面数量、时长、码率、采样率、位深和声道。
- 忽略隐藏/系统目录，不跟随符号链接；单文件解析失败不会终止整次扫描。
- 曲库、曲目列表、曲目详情、搜索筛选和重新扫描 API。
- revision 冲突检查、显式字段 patch、临时副本、写后重读验证和原子替换。
- 前端标签表单已经接通真实安全写入 API。
- MusicBrainz、LRCLIB、Apple/iTunes 官方数据源策略、统一候选评分和多源失败隔离。
- 前端数据源设置、候选搜索、字段选择和 LRCLIB 歌词采用已经接通真实 Go API。
- SQLite WAL 持久化曲库索引；重启后可直接恢复最近一次扫描结果，并支持显式重新扫描和在设置页排队切换活动曲库。
- 每次成功标签写入都会记录字段 diff、写入前后标签快照和 revision，历史页已经接通真实 API。
- 历史页支持先预览、再确认恢复到某次修改前；恢复使用相同的 revision 冲突检查、原子替换与写后验证，并生成新的审计修订。
- 可读取并真实展示嵌入封面；支持 JPEG/PNG/WebP 上传、二次确认删除、10 MiB/40MP 安全校验、原子写入和封面操作审计，MP3/FLAC/WAV 均已验证。
- 曲目检查器和匹配详情展示封面像素尺寸/字节数；上传或采用远程封面前可选择保留原图，或居中裁剪缩小到 1000×1000 / 500×500。
- 候选抽屉可采用 MusicBrainz/Apple 封面；远程 URL 只保留在后端短期引用中，并经过 HTTPS、来源域名、重定向、DNS 公网地址、大小和解码校验。
- 快速扫描使用 SQLite 持久化任务队列，HTTP 立即返回任务 ID；worker 原子领取，重启恢复等待任务，任务中心展示真实进度和结果。
- Provider 搜索结果按规范化查询写入 SQLite TTL 缓存，重启后仍可命中；设置页的数据源启停状态实时持久化，并提供真实连接测试。
- 批量匹配审核和批量安全写入已接入同一 SQLite jobs 队列；每项使用候选 ID 与基准 revision，失败项独立记录并支持 partial 结果。
- 审核页的字段 checkbox 会真实映射到写入 patch；后端无匹配项不会回退到 mock 候选，而是明确显示并安全跳过。
- 批量任务的取消、失败项重试和单任务 SSE 已接入；任务事件丢失时以前端 GET 快照恢复。
- 批量审核中的候选封面可显式勾选并进入安全写入任务；封面失败会独立标记并支持只重试封面。
- 封面历史使用 SQLite 内容寻址 blob 去重，历史页支持预览并恢复标签与嵌入封面。
- 网易云与酷我已接入独立 experimental strategy，默认关闭；酷我候选会优先读取当前 JSON 同步歌词接口并回退旧 lyric key 接口；所有实验性请求通过统一限流/缓存/失败隔离进入候选流，接口不稳定时不会影响官方来源。
- 可选接入 LrcApi 聚合策略（默认关闭）；通过 `TAGGER_LRCAPI_URL`、`TAGGER_LRCAPI_COVER_URL` 和可选 `TAGGER_LRCAPI_AUTH` 指向自托管或公开兼容接口，用于补充歌词/封面，不参与文件写入。
- 前端生产构建和任务页面均读取真实 Go API；高级筛选、导出和更多批量规则仍保留在路线图中。
- 曲库多选后的批量编辑面板已可设置/追加/删除公共字段、按选中顺序生成音轨号，并展示前 20 条差异预览。
- 真实模式下批量编辑进入持久化 `batch_edit` job，逐文件记录 diff/失败项，任务重启后可恢复并按失败项重试；Mock 模式保留即时原型流程。
- 曲目 Inspector 的试听按钮已接入真实 `/api/v1/tracks/:id/audio` Range 流，支持 MP3/FLAC/WAV、ETag 缓存和断点请求；Mock 模式仍使用本地预览。
- 任务中心会轮询并展示批量编辑的逐文件状态、失败原因和字段 diff；任务快照仍是重启和 SSE 断线后的事实来源。
- Inspector 可以通过 `/api/v1/tracks/:id/raw-tags` 读取并展开 TagLib 原始 PropertyMap，保留多值和格式专有键用于诊断；Mock 模式提供规范化预览。
- Inspector 的歌词页“查找歌词”会打开 Provider 候选并默认勾选歌词资产；仍由用户确认后写入，不会因打开搜索而自动改文件。
- 同名 `.lrc` 歌词 sidecar 已支持安全读取、按需查看、revision 冲突保护、原子创建/替换/删除和 1 MiB 大小限制；音频内嵌歌词优先展示，sidecar 作为独立文件资产管理。
- 曲库试听已提升为跨页面全局播放器；切换任务、历史或设置页面时保持播放状态。
- 任务中心状态 Tab、审核接受/跳过/更换候选状态持久化、候选封面安全预览已接通真实 API；远程封面预览失败时默认保持空白。
- 内嵌元数据已扩展到注释、作曲家、指挥、作词家、版权、BPM、ISRC、MusicBrainz 和 AcoustID 标识；Inspector、候选审核和批量编辑均按字段策略处理。
- 支持可选单用户访问令牌；配置 `--auth-token` 或 `TAGGER_AUTH_TOKEN` 后 API 使用 Bearer 令牌保护，未配置时不启用鉴权。

正在实现：

- 后续继续完善批量写入过程的更细粒度事件与审计展示，以及审核 checkbox 策略的跨刷新快照。
- 网易云/酷我实验性接口的长期兼容和正式授权接入。
- Provider 密钥等敏感配置的加密存储与缓存管理界面。
- 音频标签与 sidecar 的批量编排，以及更细粒度的跨文件部分失败恢复。
- 目录导入/导出标签快照、CSV/JSON 变更预览，以及敏感 Provider 配置加密存储。

写标签会直接修改曲库中的音乐文件。首次使用前请确认音乐目录有独立备份；revision 冲突和写后验证不能代替文件系统备份。

## 快速开始

要求 Go 1.25.7+、Node.js 20+ 和 npm。构建时需要 Node.js，运行生成的二进制不需要。

```bash
make build
./dist/tagger --music-dir /path/to/music --data-dir /path/to/tagger-data
```

首次启动需要提供一次 `--music-dir`（或 `TAGGER_MUSIC_DIR`）。之后可以在设置页切换活动曲库；如果省略该参数，程序会从 `tagger.db` 恢复上次选择的目录。

默认监听 `127.0.0.1:8080`，浏览器打开 <http://127.0.0.1:8080>。

本项目开发时可直接使用现有测试曲库：

```bash
./dist/tagger --music-dir /home/ericwyn/Downloads/TestMusic --data-dir ./data
```

也可以用环境变量配置：

```bash
TAGGER_MUSIC_DIR=/path/to/music TAGGER_DATA_DIR=/path/to/tagger-data TAGGER_LISTEN=0.0.0.0:8080 ./dist/tagger
```

如果需要单用户访问保护，可以在启动时配置令牌；不配置时不会要求鉴权：

```bash
./dist/tagger --music-dir /path/to/music --auth-token 'replace-with-a-long-random-token'
# 或：TAGGER_AUTH_TOKEN='replace-with-a-long-random-token' ./dist/tagger --music-dir /path/to/music
```

配置令牌后，浏览器会在首次打开时显示令牌输入页；API 客户端使用
`Authorization: Bearer <token>`（也兼容 `X-Tagger-Token`）。这是单用户实例级 admin token，不提供账号、角色、设备或复杂会话管理；浏览器成功验证后仅通过同源 HttpOnly cookie 免去重复输入，因此封面、音频和任务 SSE 也能正常加载。健康检查和嵌入式静态资源保持公开。

`TAGGER_DATA_DIR` 默认是当前工作目录下的 `./data`，其中保存 `tagger.db`、WAL 和后续缓存。音乐文件仍是标签事实源；SQLite 是可重建的索引与标签级历史，不是音频文件备份。外部程序修改文件后需要在界面执行重新扫描。

## 开发

分别启动后端和 Vite：

```bash
make dev-backend MUSIC_DIR=/home/ericwyn/Downloads/TestMusic
make dev-frontend
```

Vite 会把 `/api`、`/healthz`、`/readyz` 代理到 `127.0.0.1:8080`。如需只查看不依赖后端的原型：

```bash
make dev-frontend-mock
```

## 测试

```bash
make test
make lint
make test-integration MUSIC_DIR=/home/ericwyn/Downloads/TestMusic
```

真实音频只作为本地集成语料，不会提交到仓库。写入测试会先把 MP3/FLAC 复制到临时目录；WAV 测试会现场生成一个短 PCM 文件。原始 `TestMusic` 文件不会被修改。

## 目录

```text
cmd/tagger/          进程入口
internal/config/     flags 与环境变量
internal/domain/     前后端统一领域模型
internal/scanner/    文件发现、并发提取和归一化
internal/tags/       标签引擎接口与 TagLib-WASM 适配器
internal/library/    线程安全的曲库查询服务
internal/store/      SQLite migration、曲库索引和修订历史
internal/providers/  抓取策略、统一评分和官方数据源客户端
internal/server/     REST API、健康检查和 SPA 静态资源
frontend/            React 工作台
web/                 嵌入式前端资源
docs/前期设计/       产品、架构、API 与路线图设计
docs/实现记录/       已完成里程碑的验证记录
docs/任务列表.md     已实现与未实现任务的持续清单
```
