-- SQLite migration 000002: rollback auth/access-control.
-- SQLite doesn't support DROP COLUMN before 3.35; we accept the cost of leaving
-- the columns in place on rollback. Tables are dropped cleanly.

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
DROP INDEX IF EXISTS idx_users_invited_by;
DROP INDEX IF EXISTS idx_users_role;
