-- Migration: 000040_auth_access_control
-- Description: Adds invitation-based onboarding and granular access control:
--              * users.role / users.permissions / users.invited_by_user_id
--              * knowledge_bases.owner_id (tracks the user who created a KB)
--              * user_invitations table (magic-link invitations issued by admins)
--              * kb_user_permissions table (per-user grants on a knowledge base)

DO $$ BEGIN RAISE NOTICE '[Migration 000040] Starting auth/access-control setup...'; END $$;

-- 1. Extend users table with role + feature permissions + invitation lineage
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS role VARCHAR(32) NOT NULL DEFAULT 'member',
    ADD COLUMN IF NOT EXISTS permissions JSONB,
    ADD COLUMN IF NOT EXISTS invited_by_user_id VARCHAR(36);

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
CREATE INDEX IF NOT EXISTS idx_users_invited_by ON users(invited_by_user_id);

COMMENT ON COLUMN users.role IS 'Global role inside the tenant: owner, admin, member, viewer';
COMMENT ON COLUMN users.permissions IS 'Optional JSON map of feature flags overriding the role defaults (can_chat, can_search, can_create_kb, can_invite_users, ...)';
COMMENT ON COLUMN users.invited_by_user_id IS 'User ID of the admin who invited this account (null for self-registered or OIDC users)';

-- The first existing user in every tenant becomes the implicit owner.
-- We use the earliest created_at per tenant_id so multi-user tenants keep working.
UPDATE users u
SET role = 'owner'
WHERE u.id IN (
    SELECT DISTINCT ON (tenant_id) id
    FROM users
    WHERE tenant_id IS NOT NULL
    ORDER BY tenant_id, created_at ASC, id ASC
)
AND u.role = 'member';

-- 2. Track KB ownership at the user level (not just tenant level).
-- We deliberately do NOT backfill owner_id for legacy KBs: leaving it NULL keeps
-- tenant-wide visibility working for those rows under the resolution rule
-- "KB has zero user grants -> any tenant member can view it". New KBs created
-- after this migration get owner_id stamped by the KB service.
ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS owner_id VARCHAR(36);

CREATE INDEX IF NOT EXISTS idx_knowledge_bases_owner_id ON knowledge_bases(owner_id);

COMMENT ON COLUMN knowledge_bases.owner_id IS 'User ID of the KB creator/owner; null for legacy KBs that predate per-user ownership';

-- 3. Invitation table: admins create rows here, the public accept endpoint redeems the token.
CREATE TABLE IF NOT EXISTS user_invitations (
    id VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) NOT NULL,
    token VARCHAR(128) NOT NULL,
    tenant_id BIGINT NOT NULL,
    invited_by_user_id VARCHAR(36) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'member',
    permissions JSONB,
    knowledge_base_grants JSONB,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    accepted_at TIMESTAMP WITH TIME ZONE,
    accepted_user_id VARCHAR(36),
    revoked_at TIMESTAMP WITH TIME ZONE,
    revoked_by_user_id VARCHAR(36),
    note TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_invitations_token ON user_invitations(token) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_invitations_email ON user_invitations(LOWER(email));
CREATE INDEX IF NOT EXISTS idx_user_invitations_tenant ON user_invitations(tenant_id);
CREATE INDEX IF NOT EXISTS idx_user_invitations_status ON user_invitations(status);
CREATE INDEX IF NOT EXISTS idx_user_invitations_invited_by ON user_invitations(invited_by_user_id);
CREATE INDEX IF NOT EXISTS idx_user_invitations_deleted_at ON user_invitations(deleted_at);

COMMENT ON TABLE user_invitations IS 'Admin-issued invitations that allow a specific email to register or be auto-provisioned via magic link';
COMMENT ON COLUMN user_invitations.role IS 'Role to assign upon acceptance: owner, admin, member, viewer';
COMMENT ON COLUMN user_invitations.permissions IS 'Optional feature-flag overrides applied to the new user';
COMMENT ON COLUMN user_invitations.knowledge_base_grants IS 'Optional pre-grants for KBs: [{"knowledge_base_id":"...","permission":"viewer|editor|admin"}]';
COMMENT ON COLUMN user_invitations.status IS 'Lifecycle: pending, accepted, revoked, expired';

-- 4. Per-user KB permissions: complements existing org-level kb_shares with direct user grants.
CREATE TABLE IF NOT EXISTS kb_user_permissions (
    id VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    knowledge_base_id VARCHAR(36) NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    user_id VARCHAR(36) NOT NULL,
    tenant_id BIGINT NOT NULL,
    permission VARCHAR(32) NOT NULL DEFAULT 'viewer',
    granted_by_user_id VARCHAR(36) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_user_permissions_kb_user ON kb_user_permissions(knowledge_base_id, user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_user_permissions_user ON kb_user_permissions(user_id);
CREATE INDEX IF NOT EXISTS idx_kb_user_permissions_kb ON kb_user_permissions(knowledge_base_id);
CREATE INDEX IF NOT EXISTS idx_kb_user_permissions_tenant ON kb_user_permissions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_kb_user_permissions_deleted_at ON kb_user_permissions(deleted_at);

COMMENT ON TABLE kb_user_permissions IS 'Per-user direct grants on a knowledge base (complements org-level kb_shares)';
COMMENT ON COLUMN kb_user_permissions.permission IS 'Access level: viewer, editor, admin';

DO $$ BEGIN RAISE NOTICE '[Migration 000040] auth/access-control setup completed'; END $$;
