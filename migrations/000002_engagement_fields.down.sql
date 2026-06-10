ALTER TABLE nodes
    DROP COLUMN IF EXISTS y,
    DROP COLUMN IF EXISTS x,
    DROP COLUMN IF EXISTS cost_estimate,
    DROP COLUMN IF EXISTS front_domain,
    DROP COLUMN IF EXISTS listener,
    DROP COLUMN IF EXISTS subtype,
    DROP COLUMN IF EXISTS name;

ALTER TABLE engagements
    DROP COLUMN IF EXISTS engagement_type,
    DROP COLUMN IF EXISTS scope_exclusions,
    DROP COLUMN IF EXISTS lead_operator,
    DROP COLUMN IF EXISTS codename;
