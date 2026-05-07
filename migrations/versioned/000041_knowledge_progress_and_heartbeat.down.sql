-- Migration: 000040_knowledge_progress_and_heartbeat (down)
-- Description: Drop the progress + heartbeat fields and their index.

DO $$ BEGIN RAISE NOTICE '[Migration 000040 DOWN] Dropping progress + heartbeat fields'; END $$;

DROP INDEX IF EXISTS idx_knowledges_parse_status_started;

ALTER TABLE knowledges
    DROP COLUMN IF EXISTS processing_started_at,
    DROP COLUMN IF EXISTS chunks_total,
    DROP COLUMN IF EXISTS chunks_done,
    DROP COLUMN IF EXISTS aigs_chunks_total,
    DROP COLUMN IF EXISTS aigs_chunks_done;

DO $$ BEGIN RAISE NOTICE '[Migration 000040 DOWN] columns dropped'; END $$;
