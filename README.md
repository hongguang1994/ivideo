# ivideo · 网盘视频平台

以 **OpenList 挂载网盘** 为片源的点播/流媒体站,并可选接入 **Jellyfin** 提供刮削后的电影/剧集(海报、简介、分类)。前端 React,后端 Go + Gin,整套用 Docker Compose 部署,适合内网服务器。

## 架构

```
                            ┌───────── Docker Compose ─────────┐
  网盘 ──► OpenList(5244) ──┤                                   │
              │            │  strm 生成器 ──► Jellyfin(8096)    │
              │            │   (外部工具)      刮削/海报/转码     │
              │            │                     │              │
  浏览器 ─► nginx ─► Go/Gin backend ──┬── OpenList 源(裸文件浏览) │
                                       └── Jellyfin 源(刮削片库)  │
                            └───────────────────────────────────┘
```

- **OpenList**:挂载网盘,提供文件列表和直链。管理员在其 Web UI(`:5244`)配置网盘。
- **Jellyfin(可选)**:独立媒体服务器,负责刮削元数据、海报、转码。媒体库指向 `./data/media`,由外部 strm 生成器把网盘内容转成 `.strm` 直链文件放入。
- **server(Go/Gin)**:聚合两路源到 `/api/videos`(`?source=openlist|jellyfin`);播放/海报统一**代理转发**(隐藏真实地址、支持 Range 进度拖动)。未配置 `JELLYFIN_API_KEY` 时自动只提供 OpenList 源。
- **web(React + nginx)**:唯一对外入口,`/` 出前端,`/api` 反代后端。首页按来源切换标签。

## 目录结构

```
ivideo/
├── docker-compose.yml
├── .env.example
├── server/          # Go + Gin 后端
│   ├── main.go
│   └── internal/{config,openlist,handlers,router.go}
└── web/             # React 前端
    ├── nginx.conf
    └── src/{pages,components,api.ts,App.tsx}
```

## 部署(内网服务器)

前置:服务器已装 Docker 与 Docker Compose。

```bash
# 1. 拷贝项目到服务器,进入目录
cd ivideo

# 2. 准备环境变量
cp .env.example .env

# 3. 先只启动 OpenList,拿初始管理员密码
docker compose up -d openlist
docker compose logs openlist | grep -i password

# 4. 浏览器打开 http://<服务器IP>:5244
#    用 admin + 上一步的密码登录,添加网盘存储(阿里云盘/OneDrive/WebDAV 等)
#    记下视频所在目录路径,例如 /aliyun/videos

# 5. 把凭据和视频目录填进 .env
#    OPENLIST_PASSWORD=你的密码
#    OPENLIST_ROOT=/aliyun/videos

# 6. 起全部服务
docker compose up -d --build

# 7. 访问站点
#    http://<服务器IP>:8080
```

## 端口

| 服务 | 端口 | 说明 |
|------|------|------|
| web | 8090 | 视频站入口(对外)。8080 被 qBittorrent 占用,改用 8090 |
| openlist | 5244 | OpenList 管理后台(配置网盘用) |
| jellyfin | 8097 | Jellyfin 后台。8096 被现有 Jellyfin 占用,改用 8097 |
| server | 3001 | 后端,仅容器内部访问 |

## 后端 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | 健康检查,返回启用的来源列表 |
| GET | `/api/videos?source=openlist&path=/` | 列出网盘目录下的子目录与视频 |
| GET | `/api/videos?source=jellyfin` | 列出 Jellyfin 片库影片(带海报/简介) |
| GET | `/api/stream?source=openlist&path=...` | 代理网盘直链播放(支持 Range) |
| GET | `/api/stream?source=jellyfin&id=...` | 代理 Jellyfin 播放流(支持 Range) |
| GET | `/api/image?source=jellyfin&id=...` | 代理 Jellyfin 海报图 |
| GET | `/api/resources` | 资源目录(收集来的分享链接) |
| POST | `/api/resources` | 新增一条资源 |
| GET | `/api/play?resource=<id>` | 触发/查询转存,就绪返回 streamUrl |
| GET | `/api/stream?source=cache&resource=<id>` | 播放已转存进自己网盘的资源(支持 Range) |

## 按需转存缓存(核心)

把「自己的网盘」当成缓存/中转,而非仓库:

```
资源目录(分享链接) ──点播──► 缓存管理器
                              ├─ 已缓存? → 给自己盘直链播放
                              └─ 未缓存 → 后台转存进自己盘(同源转存≈秒传) → 就绪后播放
                                                    │
                          定时清理:LRU + 配额上限 淘汰旧缓存 + 清回收站
```

- **一切播放都从自己网盘出**,源分享盘只被转存那一下碰,播放带宽全走自己账号。
- **同源转存**(源与缓存盘同一家网盘)是服务器内部元数据复制,近乎瞬时。
- 状态机:`uncached → transferring → ready → cleaned`;点播非阻塞,前端轮询 `/api/play` 直到 `ready`。
- **适配器**:`CacheBackend` 接口,每个网盘一套实现。
  - `fake`:本地联调用,用公开示例视频跑通整条链路(默认)。
  - `aliyun`:阿里云盘,**当前为 stub**。落地需非官方网页 API(Go 库 [tickstep/aliyunpan-api](https://github.com/tickstep/aliyunpan-api)),建议用**专用小号**隔离封号风险,`ALIYUN_REFRESH_TOKEN` 传入凭据。
- 清理由 `CACHE_MAX_BYTES`(配额上限)和 `CACHE_TTL_HOURS`(闲置时长)控制。

## 接入 Jellyfin(可选)

1. `docker compose up -d jellyfin`,打开 `http://<服务器IP>:8096` 走首启向导。
2. 用外部 strm 生成器(如 AutoFilm / alist-strm 类工具)扫描 OpenList,把 `.strm` 直链文件输出到 `./data/media`。
3. 在 Jellyfin 后台新建媒体库,目录选 `/media`,完成刮削。
4. 后台 → 控制台 → API 密钥,生成一个密钥,填入 `.env` 的 `JELLYFIN_API_KEY`。
5. `docker compose up -d server`(重建/重启后端),前端首页即出现「Jellyfin 影库」标签。

> 未填 `JELLYFIN_API_KEY` 时后端只提供网盘源,前端不显示 Jellyfin 标签,一切照常。

## 本地开发

```bash
# 后端(需先有一个可访问的 OpenList,或改 OPENLIST_BASE_URL 指向它)
cd server
OPENLIST_BASE_URL=http://<openlist地址>:5244 OPENLIST_PASSWORD=xxx go run .

# 前端(另开终端,/api 已代理到 localhost:3001)
cd web
npm install
npm run dev   # http://localhost:5173
```

## Roadmap

**进行中 / 已搭骨架**
- [x] 按需转存缓存:SQLite 数据层 + `CacheBackend` 接口 + 缓存管理器 + LRU/配额清理(fake 适配器已跑通)
- [ ] **阿里云盘适配器**(把 stub 换成真实转存,基于 tickstep/aliyunpan-api + 专用小号)
- [ ] 前端:资源目录页 + 点播轮询「转存中→就绪」交互

**后续**
- 其它缓存盘适配器(115 / 夸克 / PikPak)
- strm 生成并入后端(定时扫描 → 生成 strm,免外部工具)
- Jellyfin 剧集(Series/Season/Episode)分季分集浏览
- 用户登录 / 权限(与 Jellyfin 用户体系映射)
- 播放量、历史记录、收藏
- 评论 / 弹幕 / 搜索
```
