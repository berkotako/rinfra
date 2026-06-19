-- Three-part defender outcome (SRA-style) per executed technique:
-- none | alerted | detected | blocked. "passed" = alerted/detected/blocked.
ALTER TABLE technique_results
    ADD COLUMN detection TEXT NOT NULL DEFAULT 'none';
