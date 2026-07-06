-- Migration 000010: enforce at most one active infrastructure job per engagement.
--
-- Two concurrent deploy/teardown requests could both pass the application-level
-- "no active job" check (a TOCTOU race) and start provisioning the same
-- engagement twice. This partial unique index makes the guard atomic at the DB:
-- at most one deploy/teardown job in a non-terminal (pending|running) state may
-- exist per engagement. Scenario-run jobs and terminal (done|failed) jobs are
-- unaffected, so history is unbounded as before.
CREATE UNIQUE INDEX idx_jobs_one_active_infra
    ON jobs (engagement_id)
    WHERE kind IN ('deploy','teardown') AND status IN ('pending','running');
