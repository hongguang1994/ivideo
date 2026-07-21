-- MySQL 建表 DDL(与 schema.sql 的 SQLite 版一一对应)。
-- 差异:AUTO_INCREMENT、VARCHAR 主键、索引写进建表、utf8mb4、TEXT/VARCHAR 默认值。

-- 资源目录:你收集来的分享链接(不含文件本体)。
CREATE TABLE IF NOT EXISTS resources (
    id         BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    title      VARCHAR(512) NOT NULL,
    poster     VARCHAR(1024)    NULL,
    overview   TEXT             NULL,
    provider   VARCHAR(32)  NOT NULL,          -- 源网盘类型:aliyun / pikpak / ...
    share_url  VARCHAR(1024) NOT NULL,         -- 分享链接
    share_pwd  VARCHAR(64)      NULL,          -- 提取码(可选)
    file_path  VARCHAR(1024)    NULL,          -- 分享内具体文件路径(可选)
    created_at BIGINT       NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 缓存项:某个资源在"自己网盘"里的转存状态。一个资源一条。
CREATE TABLE IF NOT EXISTS cache_items (
    resource_id BIGINT       NOT NULL PRIMARY KEY,
    backend     VARCHAR(32)  NOT NULL,          -- 缓存盘适配器名
    status      VARCHAR(32)  NOT NULL,          -- uncached / transferring / ready / failed / cleaned
    cache_path  VARCHAR(1024)    NULL,          -- 自己网盘里的路径
    direct_url  TEXT             NULL,          -- 可播直链(可能过期)
    size        BIGINT       NOT NULL DEFAULT 0,-- 字节
    last_access BIGINT       NOT NULL DEFAULT 0,-- 最后播放时间(unix 秒),用于 LRU
    error       VARCHAR(1024) NOT NULL DEFAULT '', -- 最近一次失败原因
    updated_at  BIGINT       NOT NULL,
    INDEX idx_cache_status_access (status, last_access)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 网盘凭据:阿里存 refresh_token,115/夸克存 cookie。扫码/填写后落库,轮换自动更新。
CREATE TABLE IF NOT EXISTS credentials (
    provider   VARCHAR(32)   NOT NULL PRIMARY KEY, -- aliyun / 115 / quark
    token      VARCHAR(4096) NOT NULL DEFAULT '',  -- refresh_token 或 cookie(开放接口 JWT 较长)
    extra      VARCHAR(2048) NOT NULL DEFAULT '',  -- 预留 JSON(如 open token、drive_id 等)
    updated_at BIGINT        NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 分享库:收藏的各网盘分享链接(整份分享,区别于 resources 的单个文件)。
CREATE TABLE IF NOT EXISTS shares (
    id              BIGINT        NOT NULL AUTO_INCREMENT PRIMARY KEY,
    provider        VARCHAR(32)   NOT NULL,                    -- aliyun / 115 / quark / pikpak / ...
    share_url       VARCHAR(1024) NOT NULL,                    -- 分享链接
    share_pwd       VARCHAR(64)       NULL,                    -- 提取码(可选)
    share_id        VARCHAR(128)      NULL,                    -- 从链接提取的分享 ID(可选)
    title           VARCHAR(512)      NULL,                    -- 名称/标题(可选)
    remark          VARCHAR(1024)     NULL,                    -- 备注(可选)
    category        VARCHAR(64)       NULL,                    -- 分类(可选)
    status          VARCHAR(16)   NOT NULL DEFAULT 'unknown',  -- unknown / valid / invalid
    last_checked_at BIGINT        NOT NULL DEFAULT 0,          -- 上次校验有效性(unix)
    file_count      INT           NOT NULL DEFAULT 0,          -- 分享内条目数(浏览后缓存,0=未知)
    total_size      BIGINT        NOT NULL DEFAULT 0,          -- 总大小(字节,0=未知)
    created_at      BIGINT        NOT NULL,
    updated_at      BIGINT        NOT NULL,
    UNIQUE KEY uniq_provider_url (provider, share_url(200))    -- 防重复收藏
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
