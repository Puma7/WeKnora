-- Migration: 000043_rbac_v2
-- Description: Adds DB-driven roles, role permission matrix, user groups, and
--              group-level KB grants. Seeds the four pre-existing system roles
--              (owner/admin/member/viewer) with permission rows that exactly
--              mirror the previously hard-coded EffectivePermissions() map, so
--              every existing user sees byte-identical effective permissions
--              after the migration runs.

DO $$ BEGIN RAISE NOTICE '[Migration 000043] Starting RBAC v2 setup...'; END $$;

-- 1. Roles registry. Tenant-scoped or global (system).
--    System roles use tenant_id = 0 to keep the unique key index simple
--    (Postgres treats NULL as distinct so a partial-unique-index would have
--    been needed otherwise).
CREATE TABLE IF NOT EXISTS roles (
    id          VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id   BIGINT NOT NULL DEFAULT 0,
    key         VARCHAR(64) NOT NULL,
    label       VARCHAR(128) NOT NULL,
    description TEXT,
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at  TIMESTAMP WITH TIME ZONE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_tenant_key
    ON roles(tenant_id, key)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_roles_tenant ON roles(tenant_id);
CREATE INDEX IF NOT EXISTS idx_roles_deleted_at ON roles(deleted_at);

COMMENT ON TABLE roles IS 'System and tenant-scoped roles for RBAC v2';
COMMENT ON COLUMN roles.tenant_id IS 'Tenant that owns this role (0 = global system role)';
COMMENT ON COLUMN roles.is_system IS 'System-seeded roles are immutable through the API';

-- 2. Role permission matrix. One row per (role, permission flag).
--    Absence means "use system default for the flag" (typically false).
CREATE TABLE IF NOT EXISTS role_permissions (
    role_id        VARCHAR(36) NOT NULL,
    permission_key VARCHAR(64) NOT NULL,
    allowed        BOOLEAN NOT NULL,
    created_at     TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at     TIMESTAMP WITH TIME ZONE,
    PRIMARY KEY (role_id, permission_key)
);

CREATE INDEX IF NOT EXISTS idx_role_permissions_role ON role_permissions(role_id);

COMMENT ON TABLE role_permissions IS 'Permission flag matrix per role (one row per granted/denied flag)';

-- 3. Seed the four system roles + their permission matrix to exactly match the
--    previously hard-coded map in internal/types/user.go EffectivePermissions().
--    On a fresh install these rows are the source of truth. On an existing
--    install, they restore the legacy behavior into the DB so the resolver
--    returns identical results.
INSERT INTO roles (id, tenant_id, key, label, description, is_system)
VALUES
    ('00000000-0000-0000-0000-000000000001', 0, 'owner',  'Owner',  'Full control of the tenant', TRUE),
    ('00000000-0000-0000-0000-000000000002', 0, 'admin',  'Admin',  'Manages users, invitations, and knowledge bases', TRUE),
    ('00000000-0000-0000-0000-000000000003', 0, 'member', 'Member', 'Can chat, search, and create personal knowledge bases', TRUE),
    ('00000000-0000-0000-0000-000000000004', 0, 'viewer', 'Viewer', 'Read-only access via chat/search to permitted KBs', TRUE)
ON CONFLICT DO NOTHING;

-- Permission matrix mirroring EffectivePermissions() exactly.
INSERT INTO role_permissions (role_id, permission_key, allowed)
VALUES
    -- Owner: everything
    ('00000000-0000-0000-0000-000000000001', 'chat',          TRUE),
    ('00000000-0000-0000-0000-000000000001', 'search',        TRUE),
    ('00000000-0000-0000-0000-000000000001', 'create_kb',     TRUE),
    ('00000000-0000-0000-0000-000000000001', 'invite_users',  TRUE),
    ('00000000-0000-0000-0000-000000000001', 'manage_users',  TRUE),
    ('00000000-0000-0000-0000-000000000001', 'manage_kbs',    TRUE),
    -- Admin: everything (same as owner)
    ('00000000-0000-0000-0000-000000000002', 'chat',          TRUE),
    ('00000000-0000-0000-0000-000000000002', 'search',        TRUE),
    ('00000000-0000-0000-0000-000000000002', 'create_kb',     TRUE),
    ('00000000-0000-0000-0000-000000000002', 'invite_users',  TRUE),
    ('00000000-0000-0000-0000-000000000002', 'manage_users',  TRUE),
    ('00000000-0000-0000-0000-000000000002', 'manage_kbs',    TRUE),
    -- Member: chat / search / create_kb only
    ('00000000-0000-0000-0000-000000000003', 'chat',          TRUE),
    ('00000000-0000-0000-0000-000000000003', 'search',        TRUE),
    ('00000000-0000-0000-0000-000000000003', 'create_kb',     TRUE),
    ('00000000-0000-0000-0000-000000000003', 'invite_users',  FALSE),
    ('00000000-0000-0000-0000-000000000003', 'manage_users',  FALSE),
    ('00000000-0000-0000-0000-000000000003', 'manage_kbs',    FALSE),
    -- Viewer: chat / search only
    ('00000000-0000-0000-0000-000000000004', 'chat',          TRUE),
    ('00000000-0000-0000-0000-000000000004', 'search',        TRUE),
    ('00000000-0000-0000-0000-000000000004', 'create_kb',     FALSE),
    ('00000000-0000-0000-0000-000000000004', 'invite_users',  FALSE),
    ('00000000-0000-0000-0000-000000000004', 'manage_users',  FALSE),
    ('00000000-0000-0000-0000-000000000004', 'manage_kbs',    FALSE)
ON CONFLICT DO NOTHING;

-- 4. User groups. Tenant-scoped collections of users with optional role + per-flag overrides.
CREATE TABLE IF NOT EXISTS user_groups (
    id          VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id   BIGINT NOT NULL,
    name        VARCHAR(128) NOT NULL,
    description TEXT,
    role_id     VARCHAR(36),
    permissions JSONB,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at  TIMESTAMP WITH TIME ZONE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_groups_tenant_name
    ON user_groups(tenant_id, LOWER(name))
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_groups_tenant ON user_groups(tenant_id);
CREATE INDEX IF NOT EXISTS idx_user_groups_role ON user_groups(role_id);
CREATE INDEX IF NOT EXISTS idx_user_groups_deleted_at ON user_groups(deleted_at);

COMMENT ON TABLE user_groups IS 'Tenant-scoped user groups with optional role + per-flag overrides';

-- 5. Group memberships. Composite primary key prevents duplicate adds.
CREATE TABLE IF NOT EXISTS user_group_members (
    group_id   VARCHAR(36) NOT NULL,
    user_id    VARCHAR(36) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_group_members_user ON user_group_members(user_id);
CREATE INDEX IF NOT EXISTS idx_user_group_members_group ON user_group_members(group_id);

-- 6. Group-level KB permissions. Mirrors kb_user_permissions but keyed by group.
CREATE TABLE IF NOT EXISTS kb_group_permissions (
    id                 VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    knowledge_base_id  VARCHAR(36) NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    group_id           VARCHAR(36) NOT NULL,
    tenant_id          BIGINT NOT NULL,
    permission         VARCHAR(32) NOT NULL DEFAULT 'viewer',
    granted_by_user_id VARCHAR(36) NOT NULL,
    created_at         TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at         TIMESTAMP WITH TIME ZONE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_group_permissions_kb_group
    ON kb_group_permissions(knowledge_base_id, group_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_group_permissions_group ON kb_group_permissions(group_id);
CREATE INDEX IF NOT EXISTS idx_kb_group_permissions_kb ON kb_group_permissions(knowledge_base_id);
CREATE INDEX IF NOT EXISTS idx_kb_group_permissions_tenant ON kb_group_permissions(tenant_id);

COMMENT ON TABLE kb_group_permissions IS 'Group-level direct grants on a knowledge base (complements kb_user_permissions and kb_shares)';

DO $$ BEGIN RAISE NOTICE '[Migration 000043] RBAC v2 setup completed'; END $$;
