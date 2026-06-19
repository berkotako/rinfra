-- Operator-managed threat-advisory feeds, collected alongside the env-configured
-- base sources. A feed is either a remote URL or an inline JSON document, both in
-- RInfra's native Advisory schema.
CREATE TABLE advisory_feeds (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL,
    url        TEXT NOT NULL DEFAULT '',
    inline     TEXT NOT NULL DEFAULT '',
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL DEFAULT ''
);
