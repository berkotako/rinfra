-- Operator-authored emulation scenarios (distinct from the code-shipped catalog).
CREATE TABLE user_scenarios (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT NOT NULL,
    adversary_profile TEXT NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    techniques        JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_user_scenarios_created ON user_scenarios(created_at DESC);
