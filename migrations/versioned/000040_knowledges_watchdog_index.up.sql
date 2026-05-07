-- ============================================================================
-- Migration 000040: Add composite index for the stuck-task watchdog scan
-- ============================================================================
-- Watchdog query (knowledge_watchdog.go FindStuckProcessing):
--   WHERE parse_status = 'processing' AND updated_at < ?
--   ORDER BY updated_at ASC LIMIT 100
--
-- The existing single-column idx_knowledges_parse_status handles the
-- equality filter, but ordering by updated_at then forces an in-memory
-- sort over every "processing" row. On instances with a large backlog
-- of historic processing rows (e.g. after a multi-tenant ingest spike)
-- that's wasted work every 5 minutes.
--
-- A composite (parse_status, updated_at) index lets Postgres satisfy
-- both the filter and the sort directly from the index. Tiny in size
-- because most rows are not 'processing' — and 'processing' is a
-- bounded working set.
-- ============================================================================

DO $$ BEGIN RAISE NOTICE '[Migration 000040] Adding watchdog composite index'; END $$;

CREATE INDEX IF NOT EXISTS idx_knowledges_parse_status_updated_at
    ON knowledges(parse_status, updated_at);
