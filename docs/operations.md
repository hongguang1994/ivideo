# 配置、部署与运维

## 配置来源

后端配置优先级为：默认值 < YAML 配置 < 环境变量。嵌套键会转换为大写下划线环境变量，例如 `openlist.base_url` 对应 `OPENLIST_BASE_URL`。

- `.env`：仅放 Docker Compose 启动所需的 MySQL 密码和 `DB_DSN`。
- `data/server/conf.yaml`：后端业务配置。
- 数据库 `provider_credentials`：网盘、Jellyfin、TMDb 和 GitHub 令牌。

不要把真实 `.env`、`conf.yaml`、数据库或 `data/` 提交到 Git。

## 首次部署

```bash
cp .env.example .env
mkdir -p data/server
cp server/configs/conf.example.yaml data/server/conf.yaml
docker compose up -d --build
```

必要配置：

- `.env` 中 `MYSQL_ROOT_PASSWORD` 与 `DB_DSN` 密码一致。
- `db.driver` 使用 `mysql`。
- `site_url` 填 Jellyfin 可访问的 ivideo 网关地址，不能在 Docker 中写 `localhost`。
- 正式使用将 `cache.backend` 设置为 `aliyun`；`fake` 只用于本地联调。

## 设置中心

推荐按以下顺序配置：

1. **网盘授权**：配置阿里扫码登录、阿里 TV/OAuth2 原画令牌、115 和夸克 Cookie。
2. **Jellyfin**：初始化管理员、API Key 和媒体库。
3. **媒体刮削**：填写 TMDb API Read Access Token。
4. **资源搜索**：填写 GitHub fine-grained token。公开仓库搜索在匿名模式下限额很低，代码搜索接口通常需要令牌。
5. **分享库与导入**：配置自动导入周期、批量大小和启用状态。

## 常用命令

```bash
# 服务状态
docker compose ps

# 后端日志
docker compose logs -f server

# 只更新后端
docker compose build server
docker compose up -d --no-deps server

# 只更新前端
docker compose build web
docker compose up -d --no-deps web

# 手动触发一次幂等 STRM 发布
curl -X POST http://127.0.0.1:8090/api/v1/strm/generate
```

## 数据库迁移

服务启动时自动执行幂等建表和迁移。MySQL 是正式数据源；SQLite 只用于本地测试。当前核心表按领域分为：

- 分享与资源：`share_sources`、`resources`。
- 媒体识别：`media_groups`、`media_group_members`、`media_candidates`、`media_aliases`、`tags`。
- 处理与发布：`media_processing_jobs`、`media_processing_steps`、`media_publications`、`media_publication_artifacts`。
- 缓存与授权：`cache_items`、`provider_credentials`、`app_settings`。

迁移代码只允许删除已经完成数据搬迁的旧表，不应在普通升级中清空业务表。

## 健康检查

```bash
curl http://127.0.0.1:8090/api/v1/health
```

设置页“网盘授权”会显示令牌有效性和播放探测速度。令牌有效但无测速数据，通常表示尚无已转存视频可供探测，不代表授权失败。

## 常见问题

### Jellyfin 没有新资源

检查：

1. 资源是否已经进入 `verified`，未确认资源只会进入 `review`。
2. `data/media` 是否同时挂载到 server 和 Jellyfin 的 `/media`。
3. 手动 STRM 发布结果是否有错误。
4. Jellyfin 媒体库路径是否为 `/media/movies`、`/media/tv`、`/media/anime`、`/media/variety`、`/media/review`。

### 海报或标题错误

在“媒体整理”中修正候选。不要只在 Jellyfin 内改图，因为下次后端发布可能根据已确认决定重新同步本地 NFO 和图片。

### 播放提示没有媒体源

依次检查资源对应 provider 的授权、分享是否有效、是否已成功转存，以及 `/api/v1/file/<资源ID>.<扩展名>` 是否能返回重定向。阿里原画令牌速度不足时可使用 HLS 或降低 `stream.original_max_mbps`。

### 分享接口返回 429

这是网盘上游限流。降低并发和检查频率，保留目录缓存，等待退避时间后重试。不要用高频循环立即重放同一分享请求。

## 备份

停止写入任务后至少备份：

- `data/mysql`
- `data/server/conf.yaml`
- `data/media`
- `data/jellyfin/config`

`data/jellyfin/cache` 可以不备份，但恢复后 Jellyfin 会重新生成缓存。

## 安全边界

项目当前没有独立的用户登录和权限系统，设计目标是可信内网。若需要远程访问，应在前置反向代理增加 HTTPS、身份认证和访问控制，且不要直接暴露 MySQL、OpenList 或后端容器端口到公网。
