# 歌词 Sidecar 兼容边界

> 当前产品主线专注于音频文件内嵌元数据。本文保留历史实现说明：已有同名 `.lrc` 仍可被后端读取并显示，但前端不再提供写入或删除 `.lrc` 的入口；后续如要彻底移除后端兼容 API，需要单独迁移存量文件。

本里程碑把同名歌词文件（例如 `track.mp3` 对应 `track.lrc`）接入扫描器和后端写入边界，作为嵌入音频标签之外的独立资产。

## 已实现

- 扫描 MP3、FLAC、WAV 时读取同目录同 basename 的 `.lrc`。
- 曲目模型增加 `lyricsSidecar` 投影：是否存在、内容 revision、字节数和修改时间；曲目列表不会携带完整歌词文件内容。
- 当音频内没有 `LYRICS` 时，扫描结果使用 `.lrc` 内容作为歌词展示值；已有嵌入歌词优先保留。
- `GET /api/v1/tracks/:id/lyrics-sidecar` 按需返回 sidecar 内容。
- `PUT`/`DELETE /api/v1/tracks/:id/lyrics-sidecar` 支持创建、原子替换和删除，并同时校验音频 revision 与 sidecar content revision。
- 写入使用同目录临时文件、`fsync`、rename 和目录同步；限制为 1 MiB，拒绝越界路径、符号链接和非普通文件。
- sidecar 写入后重新扫描曲目索引，后续读取可观察到最新文件状态。
- sidecar 写入/删除会进入 SQLite 修订历史，保存前后元数据和歌词内容快照；历史恢复预览和执行也会恢复 `.lrc` 文件。

## 一致性边界

sidecar revision 是内容 SHA-256 前 12 字节，不依赖易变的文件修改时间。音频 revision 与 sidecar revision 分开防护，避免写标签时误覆盖用户刚修改的歌词文件。

sidecar 已进入现有修订历史表，但音频标签与 sidecar 仍是两个顺序写入操作，不构成跨文件系统事务；批量编排会在后续里程碑补齐。写入失败时原子替换保证单个 sidecar 不会留下半文件，但调用方仍应保留文件系统备份。

## 验证

```bash
GOTOOLCHAIN=go1.26.7 GOCACHE=/tmp/tagger-go-cache go test ./...
GOTOOLCHAIN=go1.26.7 GOCACHE=/tmp/tagger-go-cache go test -race ./internal/... ./cmd/tagger
GOTOOLCHAIN=go1.26.7 GOCACHE=/tmp/tagger-go-cache make lint
TAGGER_TEST_MUSIC_DIR=/home/ericwyn/Downloads/TestMusic \
  GOTOOLCHAIN=go1.26.7 GOCACHE=/tmp/tagger-go-cache \
  make test-integration MUSIC_DIR=/home/ericwyn/Downloads/TestMusic
```

集成测试只操作临时复制的 TestMusic 文件，不会修改原始曲库。
