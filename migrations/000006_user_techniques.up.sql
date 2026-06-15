-- Operator-authored TTPs (techniques) for the TTP library, keyed by ATT&CK id.
CREATE TABLE user_techniques (
    attack_id   TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    tactic      TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    commands    JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
