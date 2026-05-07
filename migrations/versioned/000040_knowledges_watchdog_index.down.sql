DO $$ BEGIN RAISE NOTICE '[Migration 000040] Dropping watchdog composite index'; END $$;

DROP INDEX IF EXISTS idx_knowledges_parse_status_updated_at;
