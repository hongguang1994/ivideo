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
