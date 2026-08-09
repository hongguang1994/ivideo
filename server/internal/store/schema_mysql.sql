-- MySQL 建表 DDL(与 schema.sql 的 SQLite 版一一对应)。
-- 差异:AUTO_INCREMENT、VARCHAR 主键、索引写进建表、utf8mb4、TEXT/VARCHAR 默认值。

-- 应用设置:保存可由前端调整的调度与运行参数。
CREATE TABLE IF NOT EXISTS app_settings (
    setting_key   VARCHAR(64) NOT NULL PRIMARY KEY,
    setting_value TEXT NOT NULL,
    updated_at    BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 规范化资源目录：链接实体、发现证据、健康历史与文件树分开保存。
CREATE TABLE IF NOT EXISTS discovery_sources (
    id           BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    source_type  VARCHAR(32) NOT NULL,
    source_key   VARCHAR(255) NOT NULL,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    enabled      TINYINT(1) NOT NULL DEFAULT 1,
    config_json  TEXT NOT NULL,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL,
    UNIQUE KEY uniq_discovery_source (source_type, source_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS shares (
    id               BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    provider         VARCHAR(32) NOT NULL,
    share_url        VARCHAR(1024) NOT NULL,
    share_pwd        VARCHAR(64) NOT NULL DEFAULT '',
    share_id         VARCHAR(128) NOT NULL DEFAULT '',
    canonical_key    CHAR(64) NOT NULL,
    is_bookmarked   TINYINT(1) NOT NULL DEFAULT 0,
    title           VARCHAR(512) NOT NULL DEFAULT '',
    remark          VARCHAR(1024) NOT NULL DEFAULT '',
    category        VARCHAR(255) NOT NULL DEFAULT '',
    status           VARCHAR(16) NOT NULL DEFAULT 'unknown',
    last_checked_at  BIGINT NOT NULL DEFAULT 0,
    file_count       INT NOT NULL DEFAULT 0,
    total_size       BIGINT NOT NULL DEFAULT 0,
    first_seen_at    BIGINT NOT NULL,
    last_seen_at     BIGINT NOT NULL,
    created_at       BIGINT NOT NULL,
    updated_at       BIGINT NOT NULL,
    UNIQUE KEY uniq_shares_key (canonical_key),
    INDEX idx_shares_provider_share_id (provider, share_id),
    INDEX idx_shares_bookmarked_created (is_bookmarked, created_at DESC),
    INDEX idx_shares_status_seen (status, last_seen_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS share_observations (
    id                  BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    share_id            BIGINT NOT NULL,
    discovery_source_id BIGINT NOT NULL,
    source_ref          VARCHAR(1024) NOT NULL DEFAULT '',
    source_ref_key      CHAR(64) NOT NULL,
    title               VARCHAR(512) NOT NULL DEFAULT '',
    category            VARCHAR(255) NOT NULL DEFAULT '',
    file_name           VARCHAR(1024) NOT NULL DEFAULT '',
    metadata_json       TEXT NOT NULL,
    active              TINYINT(1) NOT NULL DEFAULT 1,
    first_seen_at       BIGINT NOT NULL,
    last_seen_at        BIGINT NOT NULL,
    UNIQUE KEY uniq_share_observation (share_id, discovery_source_id, source_ref_key),
    INDEX idx_share_observations_source_active (discovery_source_id, active),
    INDEX idx_share_observations_share_active (share_id, active),
    CONSTRAINT fk_share_observations_share FOREIGN KEY (share_id) REFERENCES shares(id) ON DELETE CASCADE,
    CONSTRAINT fk_share_observations_source FOREIGN KEY (discovery_source_id) REFERENCES discovery_sources(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS share_health_checks (
    id          BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    share_id    BIGINT NOT NULL,
    status      VARCHAR(16) NOT NULL,
    entry_count INT NOT NULL DEFAULT 0,
    total_size  BIGINT NOT NULL DEFAULT 0,
    message     VARCHAR(1024) NOT NULL DEFAULT '',
    checked_at  BIGINT NOT NULL,
    INDEX idx_share_health_checks_share_checked (share_id, checked_at),
    CONSTRAINT fk_share_health_checks_share FOREIGN KEY (share_id) REFERENCES shares(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS share_items (
    id            BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    share_id      BIGINT NOT NULL,
    path          VARCHAR(2048) NOT NULL,
    path_key      CHAR(64) NOT NULL,
    parent_path   VARCHAR(1024) NOT NULL DEFAULT '',
    name          VARCHAR(1024) NOT NULL,
    item_type     VARCHAR(16) NOT NULL,
    size          BIGINT NOT NULL DEFAULT 0,
    extension     VARCHAR(32) NOT NULL DEFAULT '',
    first_seen_at BIGINT NOT NULL,
    last_seen_at  BIGINT NOT NULL,
    UNIQUE KEY uniq_share_item (share_id, path_key),
    INDEX idx_share_items_share_parent (share_id, parent_path(200)),
    INDEX idx_share_items_name (name(200)),
    CONSTRAINT fk_share_items_share FOREIGN KEY (share_id) REFERENCES shares(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- GitHub 采集源：仅记录仓库索引、文本文件 SHA 与分享链接来源，不存视频文件。
CREATE TABLE IF NOT EXISTS github_repositories (
    id                BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    repository        VARCHAR(255) NOT NULL,
    branch            VARCHAR(128) NOT NULL DEFAULT 'main',
    parser            VARCHAR(32) NOT NULL DEFAULT 'markdown-table',
    enabled           TINYINT(1) NOT NULL DEFAULT 1,
    last_commit_sha   CHAR(64) NOT NULL DEFAULT '',
    last_collected_at BIGINT NOT NULL DEFAULT 0,
    last_error        VARCHAR(1024) NOT NULL DEFAULT '',
    created_at        BIGINT NOT NULL,
    updated_at        BIGINT NOT NULL,
    UNIQUE KEY uniq_github_repository (repository)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS github_repository_files (
    id                BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    repository_id     BIGINT NOT NULL,
    path              VARCHAR(1024) NOT NULL,
    path_key          CHAR(64) NOT NULL,
    blob_sha          CHAR(64) NOT NULL,
    size              BIGINT NOT NULL DEFAULT 0,
    active            TINYINT(1) NOT NULL DEFAULT 1,
    last_collected_at BIGINT NOT NULL DEFAULT 0,
    last_error        VARCHAR(1024) NOT NULL DEFAULT '',
    UNIQUE KEY uniq_github_repository_file (repository_id, path_key),
    INDEX idx_github_files_repository_active (repository_id, active),
    CONSTRAINT fk_github_files_repository FOREIGN KEY (repository_id) REFERENCES github_repositories (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 资源目录:分享来源中的具体文件。
CREATE TABLE IF NOT EXISTS resources (
    id         BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    share_id   BIGINT       NOT NULL,
    title      VARCHAR(512) NOT NULL,
    poster     VARCHAR(1024)    NULL,
    overview   TEXT             NULL,
    file_path  VARCHAR(1024)    NULL,          -- 分享内具体文件路径(可选)
    resource_key CHAR(64)    NOT NULL,          -- source + 文件路径的稳定去重键
    created_at BIGINT       NOT NULL,
    updated_at BIGINT       NOT NULL,
    UNIQUE KEY uniq_resources_key (resource_key),
    INDEX idx_resources_share_path (share_id, file_path(200)),
    INDEX idx_resources_created_at (created_at DESC),
    CONSTRAINT fk_resources_share
        FOREIGN KEY (share_id) REFERENCES shares (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_content_analysis (
    resource_id     BIGINT NOT NULL PRIMARY KEY,
    duration_seconds INT NOT NULL DEFAULT 0,
    width           INT NOT NULL DEFAULT 0,
    height          INT NOT NULL DEFAULT 0,
    video_codec     VARCHAR(32) NOT NULL DEFAULT '',
    audio_languages VARCHAR(255) NOT NULL DEFAULT '',
    frame_signature VARCHAR(255) NOT NULL DEFAULT '',
    ocr_text        TEXT NOT NULL,
    status          VARCHAR(16) NOT NULL DEFAULT 'ready',
    error           VARCHAR(1024) NOT NULL DEFAULT '',
    analyzed_at     BIGINT NOT NULL,
    CONSTRAINT fk_content_analysis_resource FOREIGN KEY (resource_id) REFERENCES resources (id) ON DELETE CASCADE,
    INDEX idx_content_signature (frame_signature)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 作品整理：先把属于同一作品/同一季的资源组成组，再进行候选识别和发布。
CREATE TABLE IF NOT EXISTS media_groups (
    id                BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    group_key         CHAR(64) NOT NULL,
    raw_title         VARCHAR(512) NOT NULL DEFAULT '',
    normalized_title  VARCHAR(512) NOT NULL DEFAULT '',
    media_kind        VARCHAR(16) NOT NULL DEFAULT 'episode',
    suggested_library VARCHAR(16) NOT NULL DEFAULT 'review',
    year              INT NOT NULL DEFAULT 0,
    status            VARCHAR(16) NOT NULL DEFAULT 'grouped',
    decision_source   VARCHAR(16) NOT NULL DEFAULT 'auto',
    selected_source   VARCHAR(16) NOT NULL DEFAULT '',
    selected_id       VARCHAR(64) NOT NULL DEFAULT '',
    canonical_title   VARCHAR(512) NOT NULL DEFAULT '',
    confidence        INT NOT NULL DEFAULT 0,
    reason            VARCHAR(2048) NOT NULL DEFAULT '',
    created_at        BIGINT NOT NULL,
    updated_at        BIGINT NOT NULL,
    UNIQUE KEY uniq_media_groups_key (group_key),
    INDEX idx_media_groups_status_updated (status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_group_members (
    group_id    BIGINT NOT NULL,
    resource_id BIGINT NOT NULL,
    season      INT NOT NULL DEFAULT 0,
    episode     INT NOT NULL DEFAULT 0,
    confidence  INT NOT NULL DEFAULT 0,
    PRIMARY KEY (group_id, resource_id),
    UNIQUE KEY uniq_media_group_resource (resource_id),
    CONSTRAINT fk_group_members_group FOREIGN KEY (group_id) REFERENCES media_groups (id) ON DELETE CASCADE,
    CONSTRAINT fk_group_members_resource FOREIGN KEY (resource_id) REFERENCES resources (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_candidates (
    id             BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    group_id       BIGINT NOT NULL,
    source         VARCHAR(16) NOT NULL,
    provider_id    VARCHAR(64) NOT NULL,
    title          VARCHAR(512) NOT NULL DEFAULT '',
    original_title VARCHAR(512) NOT NULL DEFAULT '',
    year           INT NOT NULL DEFAULT 0,
    media_kind     VARCHAR(16) NOT NULL DEFAULT '',
    library        VARCHAR(16) NOT NULL DEFAULT 'review',
    score          INT NOT NULL DEFAULT 0,
    evidence_json  TEXT NOT NULL,
    created_at     BIGINT NOT NULL,
    UNIQUE KEY uniq_media_candidate (group_id, source, provider_id),
    INDEX idx_media_candidates_group_score (group_id, score DESC),
    CONSTRAINT fk_media_candidates_group FOREIGN KEY (group_id) REFERENCES media_groups (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_publications (
    id            BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    group_id      BIGINT NOT NULL,
    version       INT NOT NULL,
    status        VARCHAR(16) NOT NULL DEFAULT 'published',
    output_path   VARCHAR(1024) NOT NULL DEFAULT '',
    metadata_hash CHAR(64) NOT NULL DEFAULT '',
    published_at  BIGINT NOT NULL,
    UNIQUE KEY uniq_media_publication_version (group_id, version),
    INDEX idx_media_publications_status (status, published_at),
    CONSTRAINT fk_media_publications_group FOREIGN KEY (group_id) REFERENCES media_groups (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_aliases (
    id               BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    alias            VARCHAR(512) NOT NULL,
    normalized_alias VARCHAR(512) NOT NULL,
    canonical_title  VARCHAR(512) NOT NULL,
    media_kind       VARCHAR(16) NOT NULL,
    source           VARCHAR(16) NOT NULL,
    provider_id      VARCHAR(64) NOT NULL,
    year             INT NOT NULL DEFAULT 0,
    confidence       INT NOT NULL DEFAULT 0,
    hit_count        INT NOT NULL DEFAULT 1,
    created_at       BIGINT NOT NULL,
    updated_at       BIGINT NOT NULL,
    UNIQUE KEY uniq_media_alias (normalized_alias, media_kind, year, source, provider_id),
    INDEX idx_media_alias_lookup (normalized_alias, media_kind, year, confidence)
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
    fail_count  INT          NOT NULL DEFAULT 0,    -- 连续失败次数，用于退避
    next_retry_at BIGINT     NOT NULL DEFAULT 0,    -- 下次可自动重试时间(unix 秒)
    updated_at  BIGINT       NOT NULL,
    INDEX idx_cache_status_access (status, last_access),
    CONSTRAINT fk_cache_items_resource
        FOREIGN KEY (resource_id) REFERENCES resources (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 网盘凭据:阿里存 refresh_token,115/夸克存 cookie。扫码/填写后落库,轮换自动更新。
CREATE TABLE IF NOT EXISTS provider_credentials (
    provider   VARCHAR(32)   NOT NULL PRIMARY KEY, -- aliyun / 115 / quark
    token      VARCHAR(4096) NOT NULL DEFAULT '',  -- refresh_token 或 cookie(开放接口 JWT 较长)
    extra      VARCHAR(2048) NOT NULL DEFAULT '',  -- 预留 JSON(如 open token、drive_id 等)
    updated_at BIGINT        NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_path_analyses (
    resource_id BIGINT NOT NULL PRIMARY KEY,
    analyzer_version VARCHAR(32) NOT NULL,
    path_hash CHAR(64) NOT NULL,
    analysis_json MEDIUMTEXT NOT NULL,
    analyzed_at BIGINT NOT NULL,
    INDEX idx_path_analyses_version (analyzer_version, analyzed_at),
    CONSTRAINT fk_path_analyses_resource FOREIGN KEY (resource_id) REFERENCES resources (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_title_candidates (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    resource_id BIGINT NOT NULL,
    title VARCHAR(512) NOT NULL,
    normalized_title VARCHAR(512) NOT NULL,
    year INT NOT NULL DEFAULT 0,
    source VARCHAR(64) NOT NULL,
    weight INT NOT NULL DEFAULT 0,
    auto_eligible TINYINT(1) NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    UNIQUE KEY uniq_title_candidate (resource_id, normalized_title, source),
    INDEX idx_title_candidates_resource_weight (resource_id, weight DESC),
    CONSTRAINT fk_title_candidates_resource FOREIGN KEY (resource_id) REFERENCES resources (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_verifications (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    group_id BIGINT NULL,
    resource_id BIGINT NOT NULL,
    verifier VARCHAR(32) NOT NULL,
    verifier_version VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL,
    score_delta INT NOT NULL DEFAULT 0,
    reason VARCHAR(2048) NOT NULL DEFAULT '',
    evidence_json MEDIUMTEXT NOT NULL,
    created_at BIGINT NOT NULL,
    INDEX idx_verifications_group_created (group_id, created_at DESC),
    INDEX idx_verifications_resource_created (resource_id, created_at DESC),
    CONSTRAINT fk_verifications_group FOREIGN KEY (group_id) REFERENCES media_groups (id) ON DELETE CASCADE,
    CONSTRAINT fk_verifications_resource FOREIGN KEY (resource_id) REFERENCES resources (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_processing_jobs (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    kind VARCHAR(32) NOT NULL DEFAULT 'scrape',
    status VARCHAR(16) NOT NULL DEFAULT 'queued',
    total_count INT NOT NULL DEFAULT 0,
    processed_count INT NOT NULL DEFAULT 0,
    failed_count INT NOT NULL DEFAULT 0,
    error VARCHAR(2048) NOT NULL DEFAULT '',
    started_at BIGINT NOT NULL DEFAULT 0,
    finished_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    INDEX idx_processing_jobs_status_created (status, created_at DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_processing_steps (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    job_id BIGINT NOT NULL,
    group_id BIGINT NULL,
    resource_id BIGINT NULL,
    stage VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL,
    error VARCHAR(2048) NOT NULL DEFAULT '',
    started_at BIGINT NOT NULL DEFAULT 0,
    finished_at BIGINT NOT NULL DEFAULT 0,
    UNIQUE KEY uniq_processing_step (job_id, stage, group_id, resource_id),
    INDEX idx_processing_steps_job_status (job_id, status, stage),
    CONSTRAINT fk_processing_steps_job FOREIGN KEY (job_id) REFERENCES media_processing_jobs (id) ON DELETE CASCADE,
    CONSTRAINT fk_processing_steps_group FOREIGN KEY (group_id) REFERENCES media_groups (id) ON DELETE CASCADE,
    CONSTRAINT fk_processing_steps_resource FOREIGN KEY (resource_id) REFERENCES resources (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_publication_artifacts (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    group_id BIGINT NOT NULL,
    artifact_type VARCHAR(32) NOT NULL,
    path VARCHAR(1024) NOT NULL,
    content_hash CHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'ready',
    error VARCHAR(2048) NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL,
    UNIQUE KEY uniq_publication_artifact (group_id, artifact_type, path(300)),
    INDEX idx_publication_artifacts_group_status (group_id, status),
    CONSTRAINT fk_publication_artifacts_group FOREIGN KEY (group_id) REFERENCES media_groups (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS tags (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    normalized_name VARCHAR(255) NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE KEY uniq_tags_normalized_name (normalized_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_group_tags (
    group_id BIGINT NOT NULL,
    tag_id BIGINT NOT NULL,
    source VARCHAR(32) NOT NULL,
    confidence INT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (group_id, tag_id, source),
    INDEX idx_media_group_tags_tag (tag_id, group_id),
    CONSTRAINT fk_media_group_tags_group FOREIGN KEY (group_id) REFERENCES media_groups (id) ON DELETE CASCADE,
    CONSTRAINT fk_media_group_tags_tag FOREIGN KEY (tag_id) REFERENCES tags (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
