# API 与数据存储设计

## 1. API 原则

- 同源 REST JSON，前缀 `/api/v1`。
- URL 中使用稳定 ID，不传服务器绝对路径。
- 列表使用 cursor pagination。
- 修改使用显式 patch 和 `If-Match`/`base_revision`。
- 长任务立即返回 job，不让 HTTP 请求等待整批完成。
- 任务实时更新用 SSE，事实源仍然是可重取的 GET 快照。
- 错误使用稳定 code，不要求前端解析中文 message。
- OpenAPI 为契约；TypeScript DTO 从契约生成或在 CI 中检查一致性。

## 2. 通用响应

成功：

```json
{
  "data": {},
  "meta": {
    "request_id": "req_01..."
  }
}
```

失败：

```json
{
  "error": {
    "code": "revision_conflict",
    "message": "文件已被其他操作修改",
    "details": {
      "file_id": "fil_01...",
      "current_revision": "rev_01..."
    },
    "request_id": "req_01..."
  }
}
```

主要 HTTP/code：

| HTTP | code | 场景 |
|---:|---|---|
| 400 | `invalid_request` | DTO 或字段规则错误 |
| 401 | `unauthenticated` | 未登录 |
| 403 | `forbidden` / `library_read_only` | 无权限或曲库只读 |
| 404 | `file_not_found` / `provider_not_found` | 资源不存在 |
| 409 | `revision_conflict` / `job_state_conflict` | 乐观并发冲突 |
| 413 | `payload_too_large` / `artwork_too_large` | 超限 |
| 422 | `unsupported_tag` / `unwritable_format` | 语义有效但当前格式不能执行 |
| 429 | `rate_limited` | 本地或 provider 限流 |
| 500 | `write_verification_failed` | 写后验证失败，原文件未替换 |
| 503 | `provider_unavailable` / `not_ready` | 依赖暂不可用 |

## 3. 曲库 API

### 3.1 管理曲库

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/libraries` | 曲库及统计 |
| POST | `/libraries` | 新增根目录，仅管理员 |
| GET | `/libraries/:id` | 详情、权限和扫描状态 |
| PATCH | `/libraries/:id` | 名称、忽略、只读、监听策略 |
| DELETE | `/libraries/:id` | 删除索引配置，不删除音乐文件 |
| POST | `/libraries/:id/probe` | 检查路径、权限、格式样本 |
| POST | `/libraries/:id/scans` | 创建 scan job |

新增：

```json
{
  "name": "NAS Music",
  "root_path": "/music",
  "write_enabled": false,
  "follow_symlinks": false,
  "ignore_patterns": ["@eaDir/", ".Trash-*/"]
}
```

`root_path` 只在管理员创建/修改曲库时接受。普通文件 API 永不接受绝对路径。

### 3.2 目录

`GET /libraries/:id/entries?parent_id=...&cursor=...&limit=200&sort=name`

返回目录和文件的轻量投影：

```json
{
  "data": {
    "entries": [
      {
        "kind": "file",
        "id": "fil_01...",
        "name": "01 - Track.flac",
        "relative_path": "Artist/Album/01 - Track.flac",
        "size_bytes": 31000000,
        "updated_at": "2026-08-20T08:00:00Z",
        "parse_state": "ok",
        "writable": true
      }
    ],
    "next_cursor": "..."
  }
}
```

普通用户是否返回 `relative_path` 可配置；绝不返回 root 拼接后的绝对路径。

## 4. 曲目与文件 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/tracks` | 搜索/筛选曲目 |
| GET | `/tracks/:fileId` | 标签、属性、图片描述、revision |
| PATCH | `/tracks/:fileId/tags` | 创建单文件写入 job 或同步短写 |
| GET | `/tracks/:fileId/raw-tags` | 原始统一键 |
| GET | `/tracks/:fileId/artworks` | 图片描述 |
| GET | `/tracks/:fileId/artworks/:index` | 图片/缩略图 |
| POST | `/tracks/:fileId/artworks` | 上传/替换图片 |
| DELETE | `/tracks/:fileId/artworks/:index` | 删除指定图片 |
| GET | `/tracks/:fileId/audio` | Range 试听 |
| GET | `/tracks/:fileId/history` | 文件修订 |
| POST | `/tracks/:fileId/reparse` | 重读并刷新索引 |

搜索示例：

`GET /tracks?library_id=lib_01&folder_id=dir_01&q=周杰伦&missing=artwork&format=flac&sort=album,disc,track&cursor=...`

### 4.1 读取详情

响应 header：

`ETag: "rev_01..."`

```json
{
  "data": {
    "file": {
      "id": "fil_01...",
      "name": "05. 开不了口.flac",
      "relative_path": "周杰伦/范特西/05. 开不了口.flac",
      "format": "flac",
      "size_bytes": 39124567,
      "writable": true
    },
    "revision": "rev_01...",
    "tags": {
      "title": "开不了口",
      "artists": [{"name": "周杰伦"}],
      "album": "范特西",
      "album_artists": [{"name": "周杰伦"}],
      "track": {"number": 5, "total": 10},
      "disc": {"number": 1, "total": 1},
      "release_date": {"raw": "2001-09-14", "year": 2001},
      "genres": ["流行"]
    },
    "properties": {
      "container": "flac",
      "codec": "flac",
      "duration_ms": 237000,
      "sample_rate_hz": 44100,
      "bit_depth": 16,
      "channels": 2
    },
    "artworks": [
      {
        "index": 0,
        "kind": "Front Cover",
        "mime_type": "image/jpeg",
        "width": 1200,
        "height": 1200,
        "size_bytes": 240000,
        "content_hash": "..."
      }
    ]
  }
}
```

### 4.2 修改标签

`PATCH /tracks/:fileId/tags`

header：

`If-Match: "rev_01..."`

```json
{
  "base_revision": "rev_01...",
  "patch": {
    "title": {"op": "set", "value": "开不了口"},
    "artists": {
      "op": "set",
      "value": [{"name": "周杰伦"}]
    },
    "genres": {"op": "merge", "value": ["Mandopop"]},
    "comment": {"op": "keep"}
  },
  "provenance": {
    "kind": "manual"
  },
  "dry_run": false
}
```

返回 `202 Accepted` 和 job，或对经过验证的小写入返回 `200`。为了前后端一致和历史清晰，建议即使单文件也创建 job，只是通常会快速完成。

`dry_run=true` 返回规范化后的字段 diff、warning 和 format capability，不写文件。

## 5. 匹配与 provider API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/providers` | 描述器、能力和健康 |
| GET | `/providers/:id/config` | 脱敏配置 |
| PUT | `/providers/:id/config` | 保存配置 |
| POST | `/providers/:id/test` | 测试连接 |
| POST | `/matches/tracks/search` | 多源搜索 |
| POST | `/matches/albums/search` | 专辑搜索 |
| POST | `/matches/preview` | 候选字段 diff |
| POST | `/matches/apply` | 应用确认后的候选 |
| POST | `/matches/batch` | 创建批量分析 job |

搜索请求：

```json
{
  "file_id": "fil_01...",
  "query": {
    "title": "开不了口",
    "artists": ["周杰伦"],
    "album": "范特西",
    "duration_ms": 237000
  },
  "provider_ids": ["musicbrainz", "apple_music", "netease"],
  "limit_per_provider": 10,
  "force_refresh": false
}
```

响应区分 provider 结果：

```json
{
  "data": {
    "candidates": [],
    "providers": {
      "musicbrainz": {"status": "ok", "count": 3, "latency_ms": 820},
      "apple_music": {"status": "misconfigured", "count": 0},
      "netease": {"status": "timeout", "retryable": true}
    }
  }
}
```

候选详情、歌词和大封面按需加载，避免一次搜索触发所有昂贵请求。

### 5.1 应用候选

```json
{
  "file_id": "fil_01...",
  "base_revision": "rev_01...",
  "candidate_id": "cand_01...",
  "field_policy": "selected_fields",
  "fields": {
    "title": true,
    "artists": true,
    "album": true,
    "track": true,
    "lyrics": false,
    "artwork": false
  },
  "lyrics_candidate_id": null,
  "artwork_candidate_id": null
}
```

服务端从 provider cache 重新解析 candidate；不能信任前端自行提交的远程 URL 和来源字段。

## 6. 批量与任务 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/jobs` | 任务列表 |
| GET | `/jobs/:id` | 快照和汇总 |
| GET | `/jobs/:id/items` | item 分页 |
| POST | `/jobs/:id/cancel` | 协作取消 |
| POST | `/jobs/:id/retry` | 重试失败/冲突 item |
| POST | `/jobs/:id/confirm` | 审核后进入 apply |
| GET | `/jobs/events` | 当前用户任务 SSE |
| GET | `/jobs/:id/events` | 单任务 SSE |

### 6.1 创建批量匹配

`POST /matches/batch`

```json
{
  "selection": {
    "file_ids": ["fil_01...", "fil_02..."]
  },
  "providers": ["musicbrainz", "lrclib", "netease"],
  "query_policy": "tags_then_filename",
  "apply_policy": "missing_only",
  "auto_accept_threshold": 0.92
}
```

创建后只进入 analyze。完成后 job 状态为 `review_required`。

### 6.2 确认

```json
{
  "accepted_items": [
    {
      "job_item_id": "jit_01...",
      "candidate_id": "cand_01...",
      "fields": ["title", "artists", "album", "track"]
    }
  ],
  "skip_unlisted": true,
  "confirmation_token": "..."
}
```

`confirmation_token` 由最新预览快照生成，防止用户确认后候选/选择集已经变化。

### 6.3 SSE

事件：

```text
event: job.progress
id: 184
data: {"job_id":"job_01","state":"running","processed":41,"total":100}

event: job.item
id: 185
data: {"job_id":"job_01","item_id":"jit_41","state":"succeeded"}
```

支持 `Last-Event-ID`。内存事件已丢失或服务重启时，返回 `job.snapshot`，客户端用 GET 快照重建。

## 7. 历史 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/revisions` | 全局筛选 |
| GET | `/revisions/:id` | 完整 diff/provenance |
| POST | `/revisions/:id/restore-preview` | 相对当前文件生成恢复 diff |
| POST | `/revisions/:id/restore` | 创建恢复 job |

恢复也要求当前 revision；旧修订不是“直接覆盖按钮”。

## 8. 数据表

### 8.1 `libraries`

| 字段 | 说明 |
|---|---|
| id | ULID/UUID |
| name | 展示名 |
| root_path | 仅后端使用的绝对路径 |
| root_key | 规范化路径唯一键 |
| write_enabled | 是否允许写 |
| follow_symlinks | 默认 false |
| ignore_patterns_json | 忽略规则 |
| scan_policy_json | watcher/周期策略 |
| last_scan_* | 时间与状态 |
| created_at/updated_at | 时间 |

### 8.2 `directories`

- id、library_id、parent_id、name、relative_path、path_key。
- direct_file_count、descendant_file_count、mtime_ns、scan_state。
- unique(library_id, path_key)。

### 8.3 `files`

- id、library_id、directory_id。
- name、relative_path、path_key、extension、media_type。
- size_bytes、mtime_ns、platform_file_id/inode（可空）。
- writable、missing、parse_state、parse_error_code。
- tag_revision、tag_hash、scanned_at。
- unique(library_id, path_key)。

### 8.4 `tracks`

以 file_id 为主键的一对一当前快照：

- title、album。
- artists_json、album_artists_json、genres_json。
- track_number/total、disc_number/total。
- recording/release/original date raw。
- lyrics_state、artwork_count。
- normalized_json、raw_tags_json、external_ids_json。
- search_text 或 FTS 关联。

SQLite JSON 保存灵活/多值字段，常用排序过滤字段单独成列。MVP 可先使用索引列 + LIKE；确认构建支持后再加入 FTS5。

MVP 中 album/artist 页面是对 tracks 的查询投影，不先维护可独立编辑的 album/artist 事实表。专辑批量匹配以用户选择集或目录为边界。后续若专辑视图和统计成为性能瓶颈，再增加物化表，避免第一版同时维护两套标签事实。

### 8.5 `audio_properties`

- file_id、container、codec、duration_ms、bitrate_kbps、sample_rate_hz、bit_depth、channels。
- parser_id、parser_version。

### 8.6 `artworks`

- file_id、picture_index、kind、description、mime、width、height、size_bytes、content_hash。
- 不保存当前音乐文件内的完整图片 BLOB，只保存描述和 hash。

### 8.7 `provider_configs`

- provider_id、enabled、config_json、secret_ciphertext、schema_version。
- health_state、last_checked_at、last_error_code。

### 8.8 `provider_cache`

- provider_id、cache_key、schema_version。
- response_json、result_kind、expires_at、created_at。
- 负缓存通过 result_kind 区分 not_found 与 error；error 默认不缓存。

### 8.9 `jobs`

- id、type、state、phase。
- payload_json、summary_json。
- total/processed/succeeded/skipped/failed/conflicted。
- priority、attempt、max_attempts。
- worker_id、lease_until、heartbeat_at、retry_at。
- cancel_requested_at、created_at、started_at、finished_at。
- error_code、error_message。

索引至少覆盖 `state + retry_at + priority + created_at`。

### 8.10 `job_items`

- id、job_id、file_id、state、attempt。
- base_revision、query_json。
- candidates_json 或 candidate cache refs。
- selection_json、patch_json。
- score、score_details_json。
- error_code、error_message。
- started_at、finished_at。

unique(job_id, file_id) 保证一次批量任务不重复处理同一文件。

### 8.11 `revisions`

- id、file_id、parent_revision_id。
- base_file_revision、result_file_revision。
- before_tags_json、after_tags_json、diff_json。
- before/after artwork manifests 和 sidecar manifests。
- provenance_json、job_id、user_id。
- state、error_stage、created_at、committed_at。

### 8.12 `blob_refs`

用于历史图片/sidecar 去重：

- content_hash、relative_blob_path、size_bytes、ref_count、created_at。
- 文件落在 data dir，不把大 blob 堆进 SQLite。

### 8.13 `sidecars`

- id、file_id、kind（当前为 lyrics_lrc）、relative_path、size_bytes、mtime_ns、content_hash。
- missing、writable、last_scanned_at。
- unique(file_id, kind, relative_path)。
- sidecar 仍是磁盘事实源；表中只保存当前索引和修订关联。

## 9. 数据一致性

- SQLite 当前快照只是索引，音乐文件是标签事实源。
- 每次成功写入：先替换文件，再短事务更新 current snapshot 和 committed revision。
- 文件替换成功但 DB 更新失败：启动/quick scan 根据 pending revision 和磁盘快照修复。
- DB 显示成功但文件替换不可能发生；revision 只有在替换后才 committed。
- 外部编辑后，扫描覆盖当前快照并创建 `external_change` 修订摘要。
- 删除 library 只删除索引和无引用缓存，不删除音乐文件。

## 10. 数据库迁移与备份

- migration SQL 嵌入二进制，启动时自动向前迁移。
- 迁移前创建 SQLite 在线备份或安全 copy，保留有限版本。
- schema migration 和音乐标签 migration 是两件事；升级不能自动重写整个曲库。
- 提供 `tagger doctor`/诊断 API 检查 integrity、孤儿 blob、过期 lease、残留临时文件。
- 数据目录备份应包括 SQLite、blob history、实例 key（若需要恢复 secret）和配置说明。

## 11. API 验收基线

- 任何普通文件请求都不接受绝对路径。
- 列表 limit 有硬上限，排序字段白名单。
- GET 详情和 PATCH 的 revision 契约经过并发测试。
- dry-run 与真实执行使用同一个 patch 规范化逻辑。
- SSE 断线重连不会重复应用任务。
- provider 远程 URL 不能由浏览器伪造后交给 apply。
- secret 字段所有 GET 均脱敏。
- 删除 library 不会调用任何文件删除。
- 任务与 job item 的状态转换有状态机测试。
- SQLite 崩溃恢复、lease 恢复和文件成功/DB 失败场景有集成测试。
