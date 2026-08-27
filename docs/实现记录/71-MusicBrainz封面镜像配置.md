# MusicBrainz 封面镜像配置

## 背景

MusicBrainz 搜索只提供 recording、release 和 artist 等结构化数据。Tagger 根据 release MBID 请求 `coverartarchive.org/release/{mbid}/front-500`，Cover Art Archive 再把图片请求重定向到 `archive.org/download/{item}/{file}`。部分网络环境可以查询 MusicBrainz 和 Cover Art Archive，但无法连接 Internet Archive，导致候选存在封面却探测超时。

## 实现

- MusicBrainz 运行时配置增加 `archiveDownloadBaseUrl`，默认 `https://archive.org`，设置页通过现有动态 schema 自动展示并持久化。
- 下载器只改写 `archive.org` 下的 `/download/` 地址，保留 item、文件名和查询参数；动态 `*.archive.org` 存储节点不会被当成固定镜像。
- 镜像地址只允许 HTTPS URL，不接受用户信息、查询参数或片段；可以包含 `/https/archive.org` 一类路径代理前缀，下载器会在其后拼接原始 `/download/{item}/{file}`。配置域名加入当前 MusicBrainz 下载请求的精确 allowlist，直连时仍执行公网 IP 检查。
- Provider Registry 统一提供配置感知的封面下载入口，数据源测试、候选预览、批量写入和封面缓存共享同一行为。
- 恢复默认配置会同时恢复官方 MusicBrainz API、User-Agent、限流间隔和 Internet Archive 下载基址。

镜像需要兼容以下路径形式并直接返回图片内容：

```text
https://mirror.example/download/mbid-{release-mbid}/mbid-{release-mbid}-{image-id}_thumb500.jpg
```

带路径的通用代理可以配置为：

```text
https://vercel-proxy.example/https/archive.org
```

对应请求会变成 `https://vercel-proxy.example/https/archive.org/download/{item}/{file}`；代理返回的同域相对重定向仍会经过 allowlist 校验后继续跟随。

如果镜像重新重定向回 `archive.org`，请求仍可能受到本地网络限制。

## 验证

- Go 测试覆盖 CAA 307 重定向改写、路径代理拼接、同域相对重定向、镜像图片下载与校验、非法基址拒绝、动态 Archive 节点不改写、配置默认值/更新/重置，以及 Registry 配置传递。
- React 测试覆盖设置面板展示和保存 `Internet Archive 下载基址`。
