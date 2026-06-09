-- RInfra initial schema (golang-migrate format).
-- Run with: migrate -path migrations -database "$DATABASE_URL" up

-- +migrate Up

CREATE TABLE engagements (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client          TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'draft',
    scope_targets   JSONB NOT NULL DEFAULT '[]'::jsonb,
    scope_notes     TEXT NOT NULL DEFAULT '',
    roe_document    TEXT NOT NULL DEFAULT '',
    roe_window_start TIMESTAMPTZ,
    roe_window_end   TIMESTAMPTZ,
    roe_constraints  JSONB NOT NULL DEFAULT '[]'::jsonb,
    auth_by          TEXT NOT NULL DEFAULT '',
    auth_document    TEXT NOT NULL DEFAULT '',
    auth_granted_at  TIMESTAMPTZ,
    auth_expires_at  TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE nodes (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    engagement_id UUID NOT NULL REFERENCES engagements(id) ON DELETE CASCADE,
    node_type     TEXT NOT NULL,         -- redirector | c2_server | payload_host
    cloud         TEXT NOT NULL,         -- aws | gcp | azure | digitalocean
    region        TEXT NOT NULL,
    size          TEXT NOT NULL,
    c2_framework  TEXT NOT NULL DEFAULT '',
    profile_name  TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'pending',
    health        TEXT NOT NULL DEFAULT 'unknown',
    public_ip     TEXT NOT NULL DEFAULT '',
    provider_ref  TEXT NOT NULL DEFAULT '',  -- cloud resource id, for reconciliation
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_nodes_engagement ON nodes(engagement_id);

CREATE TABLE edges (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    engagement_id UUID NOT NULL REFERENCES engagements(id) ON DELETE CASCADE,
    from_node_id  UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    to_node_id    UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE
);

CREATE TABLE scenario_runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    engagement_id UUID NOT NULL REFERENCES engagements(id) ON DELETE CASCADE,
    scenario_id   TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ
);
CREATE INDEX idx_runs_engagement ON scenario_runs(engagement_id);

CREATE TABLE technique_results (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id        UUID NOT NULL REFERENCES scenario_runs(id) ON DELETE CASCADE,
    attack_id     TEXT NOT NULL,           -- e.g. T1059.001
    status        TEXT NOT NULL,
    output        TEXT NOT NULL DEFAULT '',  -- sanitized summary only
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    err           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_results_run ON technique_results(run_id);

-- Append-only audit log. No UPDATE/DELETE is ever performed against this table;
-- enforce that at the application layer and, ideally, with a restrictive role.
CREATE TABLE audit_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    engagement_id UUID REFERENCES engagements(id) ON DELETE SET NULL,
    actor         TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    target        TEXT NOT NULL DEFAULT '',
    detail        TEXT NOT NULL DEFAULT '',  -- never store secrets here
    at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_engagement ON audit_events(engagement_id);
CREATE INDEX idx_audit_at ON audit_events(at);

-- +migrate Down
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS technique_results;
DROP TABLE IF EXISTS scenario_runs;
DROP TABLE IF EXISTS edges;
DROP TABLE IF EXISTS nodes;
DROP TABLE IF EXISTS engagements;
