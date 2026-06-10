-- Migration 000003: credentials, teamservers, jobs, and audit immutability.

-- Envelope-encrypted credentials (AES-256-GCM, data key wrapped by master key).
-- One row per provider per engagement. C2 license keys use provider = 'c2:<framework>'.
CREATE TABLE engagement_credentials (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    engagement_id UUID NOT NULL REFERENCES engagements(id) ON DELETE CASCADE,
    provider      TEXT NOT NULL,
    ciphertext    BYTEA NOT NULL,
    nonce         BYTEA NOT NULL,
    key_id        TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ,
    UNIQUE (engagement_id, provider)
);
CREATE INDEX idx_creds_engagement ON engagement_credentials(engagement_id);

-- Deployed C2 teamservers, associated with a node.
CREATE TABLE teamservers (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id                    UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    framework                  TEXT NOT NULL,
    host                       TEXT NOT NULL DEFAULT '',
    port                       INT NOT NULL DEFAULT 0,
    status                     TEXT NOT NULL DEFAULT 'pending',
    connection_info_ciphertext BYTEA,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_teamservers_node ON teamservers(node_id);

-- Durable background-job records. Survives restarts; reconciled on boot.
CREATE TABLE jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    engagement_id UUID NOT NULL REFERENCES engagements(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,   -- deploy | teardown | scenario_run
    status        TEXT NOT NULL DEFAULT 'pending',
    detail        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    err           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_jobs_engagement ON jobs(engagement_id);
CREATE INDEX idx_jobs_status ON jobs(status);

-- Immutability trigger: no UPDATE or DELETE on audit_events ever.
CREATE OR REPLACE FUNCTION audit_events_immutable()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_events rows are immutable: UPDATE and DELETE are not permitted';
END;
$$;

CREATE TRIGGER audit_events_immutable
BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();
