-- Migration: 000041_knowledge_progress_and_heartbeat
-- Description: Add per-document progress fields and a "processing heartbeat"
--              column. Together they let the new KnowledgeReconciler service
--              distinguish "still working" from "stuck" Knowledge rows after
--              an asynq retry exhaustion or a hard server crash.
--
--   processing_started_at — bumped by the worker on entry to processing
--                           and again every N chunks; reconciler treats
--                           rows older than WEKNORA_STUCK_THRESHOLD as
--                           candidates for requeue.
--   chunks_total/_done    — surfaced in the API response so the frontend
--                           can show real progress instead of an opaque
--                           "processing" badge.
--   aigs_chunks_total/_done — same, for the AIGS question-generation pass.

DO $$ BEGIN RAISE NOTICE '[Migration 000041] Adding progress + heartbeat fields to knowledges'; END $$;

ALTER TABLE knowledges
    ADD COLUMN IF NOT EXISTS processing_started_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS chunks_total          INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS chunks_done           INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS aigs_chunks_total     INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS aigs_chunks_done      INTEGER NOT NULL DEFAULT 0;

-- Reconciler scan covers a small fraction of the table (only rows still
-- in pending/processing). Index keeps the scan cheap on KBs with millions
-- of historical rows.
CREATE INDEX IF NOT EXISTS idx_knowledges_parse_status_started
    ON knowledges (parse_status, processing_started_at)
    WHERE parse_status IN ('processing', 'pending');

DO $$ BEGIN RAISE NOTICE '[Migration 000041] knowledges progress + heartbeat columns added'; END $$;
