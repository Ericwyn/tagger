# 歌词 Sidecar 读写

本里程碑把同名歌词文件（例如 `track.mp3` 对应 `track.lrc`）接入扫描器和后端写入边界，作为嵌入音频标签之外的独立资产。

## 已实现

- 扫描 MP3、FLAC、WAV 时读取同目录同 basename 的 `.lrc`。
- 曲目模型增加 `lyricsSidecar` 投影：是否存在、内容 revision、字节数和修改时间；曲目列表不会携带完整歌词文件内容。
- 当音频内没有 `LYRICS` 时，扫描结果使用 `.lrc` 内容作为歌词展示值；已有嵌入歌词优先保留。
- `GET /api/v1/tracks/:id/lyrics-sidecar` 按需返回 sidecar 内容。
- `PUT`/`DELETE /api/v1/tracks/:id/lyrics-sidecar` 支持创建、原子替换和删除，并同时校验音频 revision 与 sidecar content revision。
- 写入使用同目录临时文件、`fsync`、rename 和目录同步；限制为 1 MiB，拒绝越界路径、符号链接和非普通文件。
- sidecar 写入后重新扫描曲目索引，后续读取可观察到最新文件状态。

## 一致性边界

sidecar revision 是内容 SHA-256 前 12 字节，不依赖易变的文件修改时间。音频 revision 与 sidecar revision 分开防护，避免写标签时误覆盖用户刚修改的歌词文件。

本阶段尚未把 sidecar 变更并入现有“标签级修订”历史表，也没有把音频标签和 sidecar 组合成跨文件事务；批量编排与统一审计会在后续里程碑补齐。写入失败时原子替换保证单个 sidecar 不会留下半文件，但调用方仍应保留文件系统备份。

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
