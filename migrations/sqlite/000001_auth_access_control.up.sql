-- SQLite migration 000001: auth/access-control
-- Mirrors migrations/versioned/000040 but uses SQLite-portable syntax.
--
-- SQLite-specific notes:
--   * No JSONB; we use TEXT and rely on application-level JSON encode/decode.
--   * No DISTINCT ON; we use a correlated subquery for the per-tenant first-user lookup.
--   * Partial unique indexes (WHERE deleted_at IS NULL) ARE supported by SQLite,
--     so we keep them where they matter.

-- 1. Extend the users table with role + permissions + invitation lineage.
ALTER TABLE users ADD COLUMN role VARCHAR(32) NOT NULL DEFAULT 'member';
ALTER TABLE users ADD COLUMN permissions TEXT;
ALTER TABLE users ADD COLUMN invited_by_user_id VARCHAR(36);

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
CREATE INDEX IF NOT EXISTS idx_users_invited_by ON users(invited_by_user_id);

-- Promote the earliest active non-deleted user of each tenant to owner so
-- existing installs keep having an effective tenant owner. A soft-deleted or
-- disabled first user is skipped to avoid handing owner role to an account
-- that can no longer log in.
UPDATE users
SET role = 'owner'
WHERE role = 'member'
  AND tenant_id IS NOT NULL
  AND deleted_at IS NULL
  AND is_active = 1
  AND id = (
    SELECT u2.id
    FROM users u2
    WHERE u2.tenant_id = users.tenant_id
      AND u2.deleted_at IS NULL
      AND u2.is_active = 1
    ORDER BY u2.created_at ASC, u2.id ASC
    LIMIT 1
  );

-- 2. Track KB ownership at the user level. Legacy KBs keep owner_id NULL
--    so tenant-wide visibility stays intact for them.
ALTER TABLE knowledge_bases ADD COLUMN owner_id VARCHAR(36);
CREATE INDEX IF NOT EXISTS idx_knowledge_bases_owner_id ON knowledge_bases(owner_id);

-- 3. user_invitations: admin-issued tokens for invitation-based onboarding.
CREATE TABLE IF NOT EXISTS user_invitations (
    id VARCHAR(36) PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    token VARCHAR(128) NOT NULL,
    tenant_id INTEGER NOT NULL,
    invited_by_user_id VARCHAR(36) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'member',
    permissions TEXT,
    knowledge_base_grants TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    expires_at DATETIME NOT NULL,
    accepted_at DATETIME,
    accepted_user_id VARCHAR(36),
    revoked_at DATETIME,
    revoked_by_user_id VARCHAR(36),
    note TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_invitations_token ON user_invitations(token) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_invitations_email ON user_invitations(email COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_user_invitations_tenant ON user_invitations(tenant_id);
CREATE INDEX IF NOT EXISTS idx_user_invitations_status ON user_invitations(status);
CREATE INDEX IF NOT EXISTS idx_user_invitations_invited_by ON user_invitations(invited_by_user_id);
CREATE INDEX IF NOT EXISTS idx_user_invitations_deleted_at ON user_invitations(deleted_at);

-- 4. kb_user_permissions: per-user direct grants on a knowledge base.
CREATE TABLE IF NOT EXISTS kb_user_permissions (
    id VARCHAR(36) PRIMARY KEY,
    knowledge_base_id VARCHAR(36) NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    user_id VARCHAR(36) NOT NULL,
    tenant_id INTEGER NOT NULL,
    permission VARCHAR(32) NOT NULL DEFAULT 'viewer',
    granted_by_user_id VARCHAR(36) NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_user_permissions_kb_user ON kb_user_permissions(knowledge_base_id, user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_user_permissions_user ON kb_user_permissions(user_id);
CREATE INDEX IF NOT EXISTS idx_kb_user_permissions_kb ON kb_user_permissions(knowledge_base_id);
CREATE INDEX IF NOT EXISTS idx_kb_user_permissions_tenant ON kb_user_permissions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_kb_user_permissions_deleted_at ON kb_user_permissions(deleted_at);
