# ivideo

ivideo 是一个面向内网自用场景的网盘媒体工作台。它从公开分享源发现资源，将阿里云盘、115 和夸克分享整理进统一资源库，按需转存并通过后端代理播放，同时生成 Jellyfin 可扫描的 STRM、NFO 和图片。

## 当前能力

- 多来源资源发现：本地目录、GitHub 公开仓库、Telegram 公开频道和可定时采集的 RSS / Atom 订阅源。
- 分享库管理：收藏、批量录入、目录浏览、有效性检测和定时检查。
- 三网盘适配：阿里云盘、115、夸克的授权、分享浏览、转存和播放。
- 按需缓存：播放时转存，支持 LRU、容量、闲置时间和 Jellyfin 会话感知清理。
- 媒体整理：路径语义分析、候选标题、TMDb/豆瓣匹配、内容证据复核和人工确认。
- Jellyfin 集成：初始化、媒体库创建、STRM/NFO/图片发布、扫描与图片刷新。
- 可观测性：后端结构化日志通过 WebSocket 实时显示在设置页。

## 系统组成

| 服务 | 职责 | 默认端口 |
| --- | --- | --- |
| `web` | React 前端与 nginx API 网关 | `8090` |
| `server` | Go/Gin 业务服务、代理、任务编排 | 仅容器内 `3001` |
| `mysql` | 正式业务数据库，也暴露给局域网数据库工具 | `3306` |
| `jellyfin` | 媒体库、客户端播放与转码 | `8097` |
| `openlist` | 网盘挂载和兼容直读能力 | `5244` |

核心代码按业务模块拆分，模块只依赖窄接口，由 `internal/app` 统一装配。详细说明见 [架构文档](docs/architecture.md) 和 [媒体整理流程](docs/media-pipeline.md)。

## 快速部署

要求服务器已安装 Docker 与 Docker Compose。

```bash
cp .env.example .env
mkdir -p data/server
cp server/configs/conf.example.yaml data/server/conf.yaml
```

1. 修改 `.env` 中的 MySQL 密码和 `DB_DSN`。
2. 修改 `data/server/conf.yaml` 中的 `site_url`。它必须是 Jellyfin 容器能够访问的 ivideo 地址，例如 `http://192.168.50.140:8090`。
3. 启动服务：

```bash
docker compose up -d --build
```

4. 打开 `http://<服务器IP>:8090`，在右上角设置中心完成网盘、Jellyfin、TMDb、GitHub 和 RSS 订阅配置。

运行参数、升级和排障步骤见 [部署与运维](docs/operations.md)。

## 本地开发

后端测试默认使用 SQLite，不依赖正式 MySQL：

```bash
cd server
go test ./...

DB_DRIVER=sqlite \
DB_PATH=./ivideo.dev.db \
SITE_URL=http://localhost:5173 \
go run .
```

前端：

```bash
cd web
npm install
npm run dev
```

生产构建检查：

```bash
cd server && go vet ./... && go test ./...
cd ../web && npm run build
```

## 数据与媒体目录

运行数据不会提交到 Git：

```text
data/mysql/             MySQL 数据
data/server/            后端配置及本地状态
data/media/movies/      电影 STRM、NFO、海报
data/media/tv/          剧集 STRM、NFO、图片
data/media/anime/       动漫 STRM、NFO、图片
data/media/variety/     综艺 STRM、NFO、图片
data/media/review/      待整理资源
data/jellyfin/config/   Jellyfin 数据库与配置
data/jellyfin/cache/    Jellyfin 缓存和刮削缓存
```

## 注意事项

- 当前项目定位为可信内网自用，业务 API 尚未增加独立登录鉴权，不应直接暴露到公网。
- 网盘和第三方服务令牌保存在数据库中；`.env`、`data/` 和本地配置已加入 `.gitignore`。
- STRM 发布是全量但幂等的：输入未变化时不会重写文件或触发 Jellyfin 扫库。
- 媒体匹配置信度不足时会进入“待整理”，不会强行套用可能错误的在线元数据。

## 文档

- [架构与模块边界](docs/architecture.md)
- [媒体识别、匹配和发布流程](docs/media-pipeline.md)
- [配置、部署和运维](docs/operations.md)
- [HTTP API](docs/api.md)
