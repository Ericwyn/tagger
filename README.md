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
- 前端生产构建读取真实 Go API；任务和历史页面暂时继续使用明确标注的原型数据。

正在实现：

- SQLite 修订历史和恢复。
- SQLite 持久化扫描索引和任务。
- 网易云、酷我实验性 provider 的实际适配、缓存与配置健康检查。

写标签会直接修改曲库中的音乐文件。首次使用前请确认音乐目录有独立备份；revision 冲突和写后验证不能代替文件系统备份。

## 快速开始

要求 Go 1.25+、Node.js 20+ 和 npm。构建时需要 Node.js，运行生成的二进制不需要。

```bash
make build
./dist/tagger --music-dir /path/to/music
```

默认监听 `127.0.0.1:8080`，浏览器打开 <http://127.0.0.1:8080>。

本项目开发时可直接使用现有测试曲库：

```bash
./dist/tagger --music-dir /home/ericwyn/Downloads/TestMusic
```

也可以用环境变量配置：

```bash
TAGGER_MUSIC_DIR=/path/to/music TAGGER_LISTEN=0.0.0.0:8080 ./dist/tagger
```

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
internal/providers/  抓取策略、统一评分和官方数据源客户端
internal/server/     REST API、健康检查和 SPA 静态资源
frontend/            React 工作台
web/                 嵌入式前端资源
docs/前期设计/       产品、架构、API 与路线图设计
```
