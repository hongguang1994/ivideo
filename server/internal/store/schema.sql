-- 网盘来源：一个分享链接只保存一次；收藏只是一个标记，资源可继续引用它。
CREATE TABLE IF NOT EXISTS share_sources (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    provider        TEXT    NOT NULL,
    share_url       TEXT    NOT NULL,
    share_pwd       TEXT,
    share_id        TEXT,
    source_key      TEXT    NOT NULL UNIQUE,
    is_bookmarked   INTEGER NOT NULL DEFAULT 0,
    title           TEXT,
    remark          TEXT,
    category        TEXT,
    status          TEXT    NOT NULL DEFAULT 'unknown',
    last_checked_at INTEGER NOT NULL DEFAULT 0,
    file_count      INTEGER NOT NULL DEFAULT 0,
    total_size      INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sources_bookmarked_created ON share_sources (is_bookmarked, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sources_provider_share_id ON share_sources (provider, share_id);

-- 规范化资源目录：链接实体、发现证据、健康历史与真实文件树分离。
CREATE TABLE IF NOT EXISTS discovery_sources (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type  TEXT NOT NULL,
    source_key   TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL DEFAULT 1,
    config_json  TEXT NOT NULL DEFAULT '{}',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (source_type, source_key)
);

CREATE TABLE IF NOT EXISTS shares (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    provider         TEXT NOT NULL,
    share_url        TEXT NOT NULL,
    share_pwd        TEXT NOT NULL DEFAULT '',
    share_id         TEXT NOT NULL DEFAULT '',
    canonical_key    TEXT NOT NULL UNIQUE,
    legacy_source_id INTEGER UNIQUE,
    status           TEXT NOT NULL DEFAULT 'unknown',
    first_seen_at    INTEGER NOT NULL,
    last_seen_at     INTEGER NOT NULL,
    FOREIGN KEY (legacy_source_id) REFERENCES share_sources (id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_shares_provider_share_id ON shares (provider, share_id);
CREATE INDEX IF NOT EXISTS idx_shares_status_seen ON shares (status, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS share_observations (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    share_id            INTEGER NOT NULL,
    discovery_source_id INTEGER NOT NULL,
    source_ref          TEXT NOT NULL DEFAULT '',
    source_ref_key      TEXT NOT NULL DEFAULT '',
    title               TEXT NOT NULL DEFAULT '',
    category            TEXT NOT NULL DEFAULT '',
    file_name           TEXT NOT NULL DEFAULT '',
    metadata_json       TEXT NOT NULL DEFAULT '{}',
    active              INTEGER NOT NULL DEFAULT 1,
    first_seen_at       INTEGER NOT NULL,
    last_seen_at        INTEGER NOT NULL,
    UNIQUE (share_id, discovery_source_id, source_ref_key),
    FOREIGN KEY (share_id) REFERENCES shares (id) ON DELETE CASCADE,
    FOREIGN KEY (discovery_source_id) REFERENCES discovery_sources (id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_share_observations_source_active ON share_observations (discovery_source_id, active);
CREATE INDEX IF NOT EXISTS idx_share_observations_share_active ON share_observations (share_id, active);

CREATE TABLE IF NOT EXISTS share_health_checks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    share_id     INTEGER NOT NULL,
    status       TEXT NOT NULL,
    entry_count  INTEGER NOT NULL DEFAULT 0,
    total_size   INTEGER NOT NULL DEFAULT 0,
    message      TEXT NOT NULL DEFAULT '',
    checked_at   INTEGER NOT NULL,
    FOREIGN KEY (share_id) REFERENCES shares (id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_share_health_checks_share_checked ON share_health_checks (share_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS share_items (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    share_id     INTEGER NOT NULL,
    path         TEXT NOT NULL,
    path_key     TEXT NOT NULL,
    parent_path  TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL,
    item_type    TEXT NOT NULL,
    size         INTEGER NOT NULL DEFAULT 0,
    extension    TEXT NOT NULL DEFAULT '',
    first_seen_at INTEGER NOT NULL,
    last_seen_at  INTEGER NOT NULL,
    UNIQUE (share_id, path_key),
    FOREIGN KEY (share_id) REFERENCES shares (id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_share_items_share_parent ON share_items (share_id, parent_path);
CREATE INDEX IF NOT EXISTS idx_share_items_name ON share_items (name);

-- GitHub 采集源：仓库、文件 SHA 与解析出的分享链接分层保存。
-- 只保存文本清单和结构化结果，绝不下载网盘视频文件。
CREATE TABLE IF NOT EXISTS github_repositories (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    repository        TEXT    NOT NULL UNIQUE,
    branch            TEXT    NOT NULL DEFAULT 'main',
    parser            TEXT    NOT NULL DEFAULT 'markdown-table',
    enabled           INTEGER NOT NULL DEFAULT 1,
    last_commit_sha   TEXT    NOT NULL DEFAULT '',
    last_collected_at INTEGER NOT NULL DEFAULT 0,
    last_error        TEXT    NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS github_repository_files (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    repository_id     INTEGER NOT NULL,
    path              TEXT    NOT NULL,
    path_key          TEXT    NOT NULL,
    blob_sha          TEXT    NOT NULL,
    size              INTEGER NOT NULL DEFAULT 0,
    active            INTEGER NOT NULL DEFAULT 1,
    last_collected_at INTEGER NOT NULL DEFAULT 0,
    last_error        TEXT    NOT NULL DEFAULT '',
    UNIQUE (repository_id, path_key),
    FOREIGN KEY (repository_id) REFERENCES github_repositories (id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_github_files_repository_active ON github_repository_files (repository_id, active);

CREATE TABLE IF NOT EXISTS github_share_observations (
    repository_file_id INTEGER NOT NULL,
    source_id          INTEGER NOT NULL,
    title              TEXT    NOT NULL DEFAULT '',
    resource_type      TEXT    NOT NULL DEFAULT '',
    file_name          TEXT    NOT NULL DEFAULT '',
    updated_at_text    TEXT    NOT NULL DEFAULT '',
    active             INTEGER NOT NULL DEFAULT 1,
    observed_at        INTEGER NOT NULL,
    PRIMARY KEY (repository_file_id, source_id),
    FOREIGN KEY (repository_file_id) REFERENCES github_repository_files (id) ON DELETE CASCADE,
    FOREIGN KEY (source_id) REFERENCES share_sources (id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_github_observations_source_active ON github_share_observations (source_id, active);

-- 资源目录：分享来源中的具体文件。
CREATE TABLE IF NOT EXISTS resources (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id  INTEGER NOT NULL,
    title      TEXT    NOT NULL,
    poster     TEXT,
    overview   TEXT,
    file_path  TEXT,                        -- 分享内具体文件路径（可选）
    resource_key TEXT NOT NULL UNIQUE,      -- source + 文件路径的稳定去重键
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (source_id) REFERENCES share_sources (id) ON DELETE RESTRICT
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
    fail_count  INTEGER NOT NULL DEFAULT 0, -- 连续失败次数，用于退避
    next_retry_at INTEGER NOT NULL DEFAULT 0, -- 下次可自动重试时间(unix 秒)
    updated_at  INTEGER NOT NULL,
    FOREIGN KEY (resource_id) REFERENCES resources (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cache_status_access ON cache_items (status, last_access);
CREATE INDEX IF NOT EXISTS idx_resources_source_path ON resources (source_id, file_path);
CREATE INDEX IF NOT EXISTS idx_resources_created_at ON resources (created_at DESC);

-- 网盘凭据：阿里存 refresh_token，115/夸克存 cookie。扫码/填写后落库，轮换自动更新。
CREATE TABLE IF NOT EXISTS provider_credentials (
    provider   TEXT PRIMARY KEY,          -- aliyun / 115 / quark
    token      TEXT NOT NULL DEFAULT '',  -- refresh_token 或 cookie
    extra      TEXT NOT NULL DEFAULT '',  -- 预留 JSON（如 open token、drive_id 等）
    updated_at INTEGER NOT NULL
);

-- 应用设置：保存可由前端调整的调度与运行参数。
CREATE TABLE IF NOT EXISTS app_settings (
    setting_key   TEXT PRIMARY KEY,
    setting_value TEXT NOT NULL DEFAULT '',
    updated_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS media_content_analysis (
    resource_id INTEGER PRIMARY KEY,
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    video_codec TEXT NOT NULL DEFAULT '',
    audio_languages TEXT NOT NULL DEFAULT '',
    frame_signature TEXT NOT NULL DEFAULT '',
    ocr_text TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'ready',
    error TEXT NOT NULL DEFAULT '',
    analyzed_at INTEGER NOT NULL,
    FOREIGN KEY(resource_id) REFERENCES resources(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_content_signature ON media_content_analysis(frame_signature);

CREATE TABLE IF NOT EXISTS media_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_key TEXT NOT NULL UNIQUE,
    raw_title TEXT NOT NULL DEFAULT '',
    normalized_title TEXT NOT NULL DEFAULT '',
    media_kind TEXT NOT NULL DEFAULT 'episode',
    suggested_library TEXT NOT NULL DEFAULT 'review',
    year INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'grouped',
    decision_source TEXT NOT NULL DEFAULT 'auto',
    selected_source TEXT NOT NULL DEFAULT '',
    selected_id TEXT NOT NULL DEFAULT '',
    canonical_title TEXT NOT NULL DEFAULT '',
    confidence INTEGER NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_media_groups_status_updated ON media_groups(status, updated_at);

CREATE TABLE IF NOT EXISTS media_group_members (
    group_id INTEGER NOT NULL,
    resource_id INTEGER NOT NULL UNIQUE,
    season INTEGER NOT NULL DEFAULT 0,
    episode INTEGER NOT NULL DEFAULT 0,
    confidence INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (group_id, resource_id),
    FOREIGN KEY(group_id) REFERENCES media_groups(id) ON DELETE CASCADE,
    FOREIGN KEY(resource_id) REFERENCES resources(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS media_candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL,
    source TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    original_title TEXT NOT NULL DEFAULT '',
    year INTEGER NOT NULL DEFAULT 0,
    media_kind TEXT NOT NULL DEFAULT '',
    library TEXT NOT NULL DEFAULT 'review',
    score INTEGER NOT NULL DEFAULT 0,
    evidence_json TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL,
    UNIQUE(group_id, source, provider_id),
    FOREIGN KEY(group_id) REFERENCES media_groups(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_media_candidates_group_score ON media_candidates(group_id, score DESC);

CREATE TABLE IF NOT EXISTS media_publications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL,
    version INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'published',
    output_path TEXT NOT NULL DEFAULT '',
    metadata_hash TEXT NOT NULL DEFAULT '',
    published_at INTEGER NOT NULL,
    UNIQUE(group_id, version),
    FOREIGN KEY(group_id) REFERENCES media_groups(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_media_publications_status ON media_publications(status, published_at);

CREATE TABLE IF NOT EXISTS media_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alias TEXT NOT NULL,
    normalized_alias TEXT NOT NULL,
    canonical_title TEXT NOT NULL,
    media_kind TEXT NOT NULL,
    source TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    year INTEGER NOT NULL DEFAULT 0,
    confidence INTEGER NOT NULL DEFAULT 0,
    hit_count INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(normalized_alias, media_kind, year, source, provider_id)
);
CREATE INDEX IF NOT EXISTS idx_media_alias_lookup ON media_aliases(normalized_alias, media_kind, year, confidence DESC);

-- 路径语义分析快照：保留完整证据和分析器版本，便于规则升级后重新分析。
CREATE TABLE IF NOT EXISTS media_path_analyses (
    resource_id INTEGER PRIMARY KEY,
    analyzer_version TEXT NOT NULL,
    path_hash TEXT NOT NULL,
    analysis_json TEXT NOT NULL,
    analyzed_at INTEGER NOT NULL,
    FOREIGN KEY(resource_id) REFERENCES resources(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_path_analyses_version ON media_path_analyses(analyzer_version, analyzed_at);

-- 路径阶段的片名候选；与元数据服务返回的 media_candidates 分开保存。
CREATE TABLE IF NOT EXISTS media_title_candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    resource_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    normalized_title TEXT NOT NULL,
    year INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL,
    weight INTEGER NOT NULL DEFAULT 0,
    auto_eligible INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    UNIQUE(resource_id, normalized_title, source),
    FOREIGN KEY(resource_id) REFERENCES resources(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_title_candidates_resource_weight ON media_title_candidates(resource_id, weight DESC);

-- 各复核器产生的历史证据，不覆盖旧结果。
CREATE TABLE IF NOT EXISTS media_verifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER,
    resource_id INTEGER NOT NULL,
    verifier TEXT NOT NULL,
    verifier_version TEXT NOT NULL,
    status TEXT NOT NULL,
    score_delta INTEGER NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '',
    evidence_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    FOREIGN KEY(group_id) REFERENCES media_groups(id) ON DELETE CASCADE,
    FOREIGN KEY(resource_id) REFERENCES resources(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_verifications_group_created ON media_verifications(group_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_verifications_resource_created ON media_verifications(resource_id, created_at DESC);

-- 媒体整理任务及阶段，用于进度、失败重试和前端展示。
CREATE TABLE IF NOT EXISTS media_processing_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL DEFAULT 'scrape',
    status TEXT NOT NULL DEFAULT 'queued',
    total_count INTEGER NOT NULL DEFAULT 0,
    processed_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    started_at INTEGER NOT NULL DEFAULT 0,
    finished_at INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_processing_jobs_status_created ON media_processing_jobs(status, created_at DESC);

CREATE TABLE IF NOT EXISTS media_processing_steps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id INTEGER NOT NULL,
    group_id INTEGER,
    resource_id INTEGER,
    stage TEXT NOT NULL,
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    started_at INTEGER NOT NULL DEFAULT 0,
    finished_at INTEGER NOT NULL DEFAULT 0,
    UNIQUE(job_id, stage, group_id, resource_id),
    FOREIGN KEY(job_id) REFERENCES media_processing_jobs(id) ON DELETE CASCADE,
    FOREIGN KEY(group_id) REFERENCES media_groups(id) ON DELETE CASCADE,
    FOREIGN KEY(resource_id) REFERENCES resources(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_processing_steps_job_status ON media_processing_steps(job_id, status, stage);

-- 发布制品逐项记录，支持判断 NFO、图片、STRM 或 Jellyfin 同步具体失败在哪一步。
CREATE TABLE IF NOT EXISTS media_publication_artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL,
    artifact_type TEXT NOT NULL,
    path TEXT NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'ready',
    error TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL,
    UNIQUE(group_id, artifact_type, path),
    FOREIGN KEY(group_id) REFERENCES media_groups(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_publication_artifacts_group_status ON media_publication_artifacts(group_id, status);

-- 独立标签字典及作品标签关系，记录自动生成或人工设置的来源。
CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS media_group_tags (
    group_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    source TEXT NOT NULL,
    confidence INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    PRIMARY KEY(group_id, tag_id, source),
    FOREIGN KEY(group_id) REFERENCES media_groups(id) ON DELETE CASCADE,
    FOREIGN KEY(tag_id) REFERENCES tags(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_media_group_tags_tag ON media_group_tags(tag_id, group_id);
