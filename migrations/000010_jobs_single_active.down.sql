-- Migration 000010 (down): drop the single-active-infra-job guard.
DROP INDEX IF EXISTS idx_jobs_one_active_infra;
