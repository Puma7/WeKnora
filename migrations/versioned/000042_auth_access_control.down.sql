-- Migration: 000042_auth_access_control (down)
DO $$ BEGIN RAISE NOTICE '[Migration 000042] Rolling back auth/access-control setup...'; END $$;

DROP INDEX IF EXISTS idx_kb_user_permissions_deleted_at;
DROP INDEX IF EXISTS idx_kb_user_permissions_tenant;
DROP INDEX IF EXISTS idx_kb_user_permissions_kb;
DROP INDEX IF EXISTS idx_kb_user_permissions_user;
DROP INDEX IF EXISTS idx_kb_user_permissions_kb_user;
DROP TABLE IF EXISTS kb_user_permissions;

DROP INDEX IF EXISTS idx_user_invitations_deleted_at;
DROP INDEX IF EXISTS idx_user_invitations_invited_by;
DROP INDEX IF EXISTS idx_user_invitations_status;
DROP INDEX IF EXISTS idx_user_invitations_tenant;
DROP INDEX IF EXISTS idx_user_invitations_email;
DROP INDEX IF EXISTS idx_user_invitations_token;
DROP TABLE IF EXISTS user_invitations;

DROP INDEX IF EXISTS idx_knowledge_bases_owner_id;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS owner_id;

DROP INDEX IF EXISTS idx_users_invited_by;
DROP INDEX IF EXISTS idx_users_role;
ALTER TABLE users
    DROP COLUMN IF EXISTS invited_by_user_id,
    DROP COLUMN IF EXISTS permissions,
    DROP COLUMN IF EXISTS role;

DO $$ BEGIN RAISE NOTICE '[Migration 000042] Rollback completed'; END $$;
