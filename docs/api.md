# HTTP API

所有业务接口前缀为 `/api/v1`，响应格式为：

```json
{"code": 0, "msg": "ok", "data": {}}
```

当前 API 用于可信内网，不包含独立用户鉴权。

## 系统与日志

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/health` | 服务、来源和缓存后端状态 |
| GET | `/logs/ws` | 后端实时日志 WebSocket |

## 资源发现与分享

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/search/resources?q=` | 聚合资源搜索 |
| GET | `/search/resources/ws` | 渐进式搜索 WebSocket |
| GET | `/search/github/sources` | GitHub 资源源列表 |
| GET | `/search/github/resources` | 已解析的 GitHub 网盘资源 |
| POST | `/search/github/resources/sync` | 同步 GitHub 资源入库 |
| GET/POST | `/shares` | 分享库列表/新增 |
| POST | `/shares/batch` | 批量新增分享 |
| PUT/DELETE | `/shares/:id` | 更新/删除分享 |
| POST | `/shares/check` | 检查全部分享 |
| POST | `/shares/:id/check` | 检查单个分享 |
| GET | `/share/browse` | 浏览分享目录 |
| POST | `/share/save` | 转存分享条目 |

## 资源、缓存与播放

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET/POST | `/resources` | 资源列表/手工新增 |
| POST | `/resources/import` | 遍历分享并导入视频 |
| GET | `/play?resource=` | 触发或查询按需转存 |
| GET | `/cache` | 缓存列表与占用 |
| POST | `/cache/evict` | 主动清理缓存项 |
| GET | `/stream?source=cache&resource=` | 缓存播放代理 |
| GET/HEAD | `/file/:name` | Jellyfin STRM 原画入口 |
| GET | `/hls/:name` | Jellyfin STRM HLS 入口 |
| GET | `/hls-seg/:name` | HLS 切片代理 |

`/videos`、`/image` 以及 `/stream?source=openlist|jellyfin` 保留为兼容直读接口，当前主界面不再提供入口。

## 媒体整理与 Jellyfin

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/strm/generate` | 幂等重建媒体发布目录 |
| GET | `/media-groups` | 作品组和待整理列表 |
| GET | `/media-groups/:id` | 作品组、成员和候选详情 |
| POST | `/media-groups/:id/candidates` | 重新搜索候选 |
| POST | `/media-groups/:id/confirm` | 人工确认候选 |
| POST | `/media-groups/:id/review` | 标记待整理 |
| GET | `/settings/metadata` | 元数据任务状态 |
| POST | `/settings/metadata/token` | 保存 TMDb token |
| POST | `/metadata/scrape` | 启动元数据整理 |
| GET | `/settings/jellyfin` | Jellyfin 初始化状态 |
| POST | `/settings/jellyfin/initialize` | 初始化 Jellyfin |
| POST | `/settings/jellyfin/refresh-images` | 刷新 Jellyfin 图片 |

## 设置与任务

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/settings/providers` | 网盘授权与诊断状态 |
| POST | `/settings/providers/check` | 检测令牌和播放能力 |
| POST | `/settings/token` | 保存 provider 令牌 |
| GET/PUT | `/settings/import` | 自动导入设置 |
| GET | `/imports/status` | 导入任务进度 |
| POST | `/imports/run` | 立即运行导入 |
| GET | `/settings/search` | 搜索引擎设置 |
| POST/DELETE | `/settings/search/github` | 保存/删除 GitHub token |
| GET/PUT | `/settings/search/rss` | RSS / Atom 订阅源和定时采集设置 |
| POST | `/settings/search/rss/run` | 立即采集 RSS / Atom 订阅源 |

扫码授权接口位于 `/auth/aliyun/*`、`/auth/aliyun/open/*`、`/auth/115/*` 和 `/auth/quark/*`。
