# ivideo · 网盘视频平台

基于 **OpenList 挂载网盘** 作为片源的点播/流媒体站。前端 React,后端 Go + Gin,整套用 Docker Compose 部署,适合内网服务器。

## 架构

```
浏览器 ──► nginx(web 网关) ──► /      React 静态站
                            └► /api  反向代理到 Go 后端
                                      │
                                 Go + Gin(server)
                                      │  调 OpenList REST API
                                 OpenList(:5244)──► 阿里云盘 / OneDrive / WebDAV / 百度网盘 …
```

- **OpenList**:挂载网盘,提供文件列表和直链。管理员在其 Web UI(`:5244`)配置网盘。
- **server(Go/Gin)**:把配置目录里的视频整理成 `/api/videos`;播放时取网盘直链并**代理转发**(隐藏真实链接、支持 Range 进度拖动)。
- **web(React + nginx)**:唯一对外入口,`/` 出前端,`/api` 反代后端。

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
| web | 8080 | 视频站入口(对外) |
| openlist | 5244 | OpenList 管理后台(配置网盘用) |
| server | 3001 | 后端,仅容器内部访问 |

## 后端 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | 健康检查 |
| GET | `/api/videos?path=/` | 列出目录下的子目录与视频 |
| GET | `/api/stream?path=...` | 代理网盘直链播放(支持 Range) |

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

## Roadmap(尚未实现)

- 用户登录 / 权限
- 视频封面、时长、简介(读取 OpenList 缩略图 / 元数据)
- 播放量、历史记录、收藏(引入 SQLite)
- 评论 / 弹幕
- 搜索
- 上传(经 OpenList API 写回网盘)
- 转码 / 自动生成 HLS
```
