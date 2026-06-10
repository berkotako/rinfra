-- Migration 000002: add engagement metadata and node canvas fields.

ALTER TABLE engagements
    ADD COLUMN codename        TEXT NOT NULL DEFAULT '',
    ADD COLUMN lead_operator   TEXT NOT NULL DEFAULT '',
    ADD COLUMN scope_exclusions JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN engagement_type TEXT NOT NULL DEFAULT '';

ALTER TABLE nodes
    ADD COLUMN name          TEXT NOT NULL DEFAULT '',
    ADD COLUMN subtype       TEXT NOT NULL DEFAULT '',
    ADD COLUMN listener      TEXT NOT NULL DEFAULT '',
    ADD COLUMN front_domain  TEXT NOT NULL DEFAULT '',
    ADD COLUMN cost_estimate NUMERIC(8,2) NOT NULL DEFAULT 0,
    ADD COLUMN x             INT NOT NULL DEFAULT 0,
    ADD COLUMN y             INT NOT NULL DEFAULT 0;
