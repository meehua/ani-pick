-- AniPick 初始结构：条目索引（只存筛选所需的主信息）+ 设置 + 抓取记录 + 管理会话

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS subjects (
    id            BIGINT PRIMARY KEY,
    type          SMALLINT NOT NULL,
    name          TEXT NOT NULL,
    name_cn       TEXT NOT NULL DEFAULT '',
    date          DATE,
    platform      TEXT NOT NULL DEFAULT '',
    rank          INTEGER NOT NULL DEFAULT 0,
    score         NUMERIC(3, 1) NOT NULL DEFAULT 0,
    rating_total  INTEGER NOT NULL DEFAULT 0,
    wish          INTEGER NOT NULL DEFAULT 0,
    collect       INTEGER NOT NULL DEFAULT 0,
    doing         INTEGER NOT NULL DEFAULT 0,
    on_hold       INTEGER NOT NULL DEFAULT 0,
    dropped       INTEGER NOT NULL DEFAULT 0,
    nsfw          BOOLEAN NOT NULL DEFAULT FALSE,
    meta_tags     TEXT[] NOT NULL DEFAULT '{}',
    image         TEXT NOT NULL DEFAULT '',
    tags_total    INTEGER NOT NULL DEFAULT 0,
    locked        BOOLEAN NOT NULL DEFAULT FALSE,
    fail_count    SMALLINT NOT NULL DEFAULT 0,
    last_run_id   BIGINT,
    fetched_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS subjects_type_rank_idx ON subjects (type, rank);
CREATE INDEX IF NOT EXISTS subjects_type_score_idx ON subjects (type, score DESC);
CREATE INDEX IF NOT EXISTS subjects_type_date_idx ON subjects (type, date DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS subjects_type_rating_idx ON subjects (type, rating_total DESC);
CREATE INDEX IF NOT EXISTS subjects_rank_idx ON subjects (rank) WHERE rank > 0;
CREATE INDEX IF NOT EXISTS subjects_fetched_idx ON subjects (fetched_at);
CREATE INDEX IF NOT EXISTS subjects_meta_idx ON subjects USING GIN (meta_tags);

CREATE TABLE IF NOT EXISTS tags (
    id   BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS subject_tags (
    subject_id BIGINT NOT NULL REFERENCES subjects (id) ON DELETE CASCADE,
    tag_id     BIGINT NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    votes      INTEGER NOT NULL,
    pct        NUMERIC(5, 2) NOT NULL,
    PRIMARY KEY (subject_id, tag_id)
);

CREATE INDEX IF NOT EXISTS subject_tags_tag_idx ON subject_tags (tag_id, votes DESC);

-- 标签热度（按类型聚合），用于筛选候选与占比展示；抓取后重算
CREATE TABLE IF NOT EXISTS tag_usage (
    type          SMALLINT NOT NULL,
    tag_id        BIGINT NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    subject_count INTEGER NOT NULL DEFAULT 0,
    votes         BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (type, tag_id)
);

CREATE INDEX IF NOT EXISTS tags_name_trgm_idx ON tags USING GIN (name gin_trgm_ops);

CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS crawl_runs (
    id          BIGSERIAL PRIMARY KEY,
    mode        TEXT NOT NULL,
    trigger     TEXT NOT NULL DEFAULT 'manual',
    status      TEXT NOT NULL DEFAULT 'running',
    types       INTEGER[] NOT NULL DEFAULT '{}',
    stats       JSONB NOT NULL DEFAULT '{}',
    error       TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS crawl_runs_started_idx ON crawl_runs (started_at DESC);

CREATE TABLE IF NOT EXISTS admin_sessions (
    token_hash TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS admin_sessions_expires_idx ON admin_sessions (expires_at);
