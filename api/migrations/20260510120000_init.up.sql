CREATE TABLE pull_requests (
    github_id      BIGINT PRIMARY KEY,
    node_id        TEXT NOT NULL UNIQUE,
    repo           TEXT NOT NULL,
    pr_number      INT  NOT NULL,
    title          TEXT NOT NULL,
    author         TEXT NOT NULL,
    status         TEXT NOT NULL,
    draft          BOOLEAN NOT NULL DEFAULT false,
    additions      INT NOT NULL DEFAULT 0,
    deletions      INT NOT NULL DEFAULT 0,
    comments_count INT NOT NULL DEFAULT 0,
    head_sha       TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    closed_at      TIMESTAMPTZ,
    merged_at      TIMESTAMPTZ,
    merged_by      TEXT,
    synced_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    ingest_version INTEGER NOT NULL DEFAULT 0,
    UNIQUE (repo, pr_number)
);

CREATE INDEX pull_requests_status_updated ON pull_requests (status, updated_at DESC);

CREATE TABLE pull_request_review_requests (
    pr_github_id BIGINT NOT NULL REFERENCES pull_requests(github_id) ON DELETE CASCADE,
    reviewer     TEXT   NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (pr_github_id, reviewer)
);

CREATE TABLE pull_request_reviews (
    github_review_id BIGINT PRIMARY KEY,
    pr_github_id     BIGINT NOT NULL REFERENCES pull_requests(github_id) ON DELETE CASCADE,
    reviewer         TEXT NOT NULL,
    verdict          TEXT NOT NULL,
    submitted_at     TIMESTAMPTZ NOT NULL
);

CREATE INDEX pull_request_reviews_lookup
    ON pull_request_reviews (pr_github_id, reviewer, submitted_at DESC);

CREATE TABLE pull_request_check_runs (
    github_check_id BIGINT PRIMARY KEY,
    repo            TEXT NOT NULL,
    head_sha        TEXT NOT NULL,
    check_name      TEXT NOT NULL,
    run_status      TEXT NOT NULL,
    conclusion      TEXT,
    completed_at    TIMESTAMPTZ
);

CREATE INDEX pull_request_check_runs_head ON pull_request_check_runs (repo, head_sha);

CREATE TABLE pull_request_labels (
    pr_github_id BIGINT NOT NULL REFERENCES pull_requests(github_id) ON DELETE CASCADE,
    label        TEXT   NOT NULL,
    PRIMARY KEY (pr_github_id, label)
);

CREATE INDEX pull_request_labels_label ON pull_request_labels (label);

CREATE TABLE pull_request_views (
    user_login              TEXT        NOT NULL,
    pr_github_id            BIGINT      NOT NULL REFERENCES pull_requests(github_id) ON DELETE CASCADE,
    viewed_at               TIMESTAMPTZ NOT NULL,
    comments_count_at_view  INTEGER     NOT NULL,
    head_sha_at_view        TEXT        NOT NULL,
    PRIMARY KEY (user_login, pr_github_id)
);

CREATE INDEX pull_request_views_user_idx ON pull_request_views (user_login);

CREATE TABLE repo_sync_cursors (
    repo            TEXT PRIMARY KEY,
    last_synced_at  TIMESTAMPTZ
);
