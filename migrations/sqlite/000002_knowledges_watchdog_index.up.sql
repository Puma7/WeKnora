-- ============================================================================
-- SQLite migration: composite index for the stuck-task watchdog scan.
-- See migrations/versioned/000040_knowledges_watchdog_index.up.sql for the
-- Postgres counterpart and the rationale.
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_knowledges_parse_status_updated_at
    ON knowledges(parse_status, updated_at);
