-- 资源目录：你收集来的分享链接（不含文件本体）。
CREATE TABLE IF NOT EXISTS resources (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT    NOT NULL,
    poster     TEXT,
    overview   TEXT,
    provider   TEXT    NOT NULL,            -- 源网盘类型：aliyun / pikpak / ...
    share_url  TEXT    NOT NULL,            -- 分享链接
    share_pwd  TEXT,                        -- 提取码（可选）
    file_path  TEXT,                        -- 分享内具体文件路径（可选）
    created_at INTEGER NOT NULL
);

-- 缓存项：某个资源在“自己网盘”里的转存状态。一个资源一条。
CREATE TABLE IF NOT EXISTS cache_items (
    resource_id INTEGER PRIMARY KEY,
    backend     TEXT    NOT NULL,           -- 缓存盘适配器名
    status      TEXT    NOT NULL,           -- uncached / transferring / ready / failed / cleaned
    cache_path  TEXT,                       -- 自己网盘里的路径
    direct_url  TEXT,                       -- 可播直链（可能过期）
    size        INTEGER NOT NULL DEFAULT 0, -- 字节
    last_access INTEGER NOT NULL DEFAULT 0, -- 最后播放时间（unix 秒），用于 LRU
    error       TEXT,                       -- 最近一次失败原因
    updated_at  INTEGER NOT NULL,
    FOREIGN KEY (resource_id) REFERENCES resources (id)
);

CREATE INDEX IF NOT EXISTS idx_cache_status_access ON cache_items (status, last_access);

-- 网盘凭据：阿里存 refresh_token，115/夸克存 cookie。扫码/填写后落库，轮换自动更新。
CREATE TABLE IF NOT EXISTS credentials (
    provider   TEXT PRIMARY KEY,          -- aliyun / 115 / quark
    token      TEXT NOT NULL DEFAULT '',  -- refresh_token 或 cookie
    extra      TEXT NOT NULL DEFAULT '',  -- 预留 JSON（如 open token、drive_id 等）
    updated_at INTEGER NOT NULL
);

-- 分享库：收藏的各网盘分享链接（整份分享，区别于 resources 的单个文件）。
CREATE TABLE IF NOT EXISTS shares (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    provider        TEXT    NOT NULL,                    -- aliyun / 115 / quark / pikpak / ...
    share_url       TEXT    NOT NULL,                    -- 分享链接
    share_pwd       TEXT,                                -- 提取码（可选）
    share_id        TEXT,                                -- 从链接提取的分享 ID（可选，便于查重/调用）
    title           TEXT,                                -- 名称/标题（可选）
    remark          TEXT,                                -- 备注（可选）
    category        TEXT,                                -- 分类：电影/剧集/音乐/...（可选）
    status          TEXT    NOT NULL DEFAULT 'unknown',  -- unknown / valid / invalid
    last_checked_at INTEGER NOT NULL DEFAULT 0,          -- 上次校验有效性（unix）
    file_count      INTEGER NOT NULL DEFAULT 0,          -- 分享内条目数（浏览后缓存，0=未知）
    total_size      INTEGER NOT NULL DEFAULT 0,          -- 总大小（字节，0=未知）
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE (provider, share_url)                         -- 防重复收藏
);
