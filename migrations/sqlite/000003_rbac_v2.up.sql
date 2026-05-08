-- SQLite migration 000003: RBAC v2 (roles + role permission matrix + groups)
-- Mirrors migrations/versioned/000043 but uses SQLite-portable syntax:
--   * No JSONB; permissions JSON map stored as TEXT
--   * No COMMENT ON; documentation lives at the application layer
--   * Hard-coded UUIDs for system roles so the seed is deterministic across installs

-- 1. Roles registry. Tenant-scoped or global (system). Tenant 0 = global.
CREATE TABLE IF NOT EXISTS roles (
    id          VARCHAR(36) PRIMARY KEY,
    tenant_id   INTEGER NOT NULL DEFAULT 0,
    key         VARCHAR(64) NOT NULL,
    label       VARCHAR(128) NOT NULL,
    description TEXT,
    is_system   INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at  DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_tenant_key
    ON roles(tenant_id, key)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_roles_tenant ON roles(tenant_id);
CREATE INDEX IF NOT EXISTS idx_roles_deleted_at ON roles(deleted_at);

-- 2. Role permission matrix.
CREATE TABLE IF NOT EXISTS role_permissions (
    role_id        VARCHAR(36) NOT NULL,
    permission_key VARCHAR(64) NOT NULL,
    allowed        INTEGER NOT NULL,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at     DATETIME,
    PRIMARY KEY (role_id, permission_key)
);

CREATE INDEX IF NOT EXISTS idx_role_permissions_role ON role_permissions(role_id);

-- 3. Seed system roles. INSERT OR IGNORE so re-running the migration is a no-op.
INSERT OR IGNORE INTO roles (id, tenant_id, key, label, description, is_system) VALUES
    ('00000000-0000-0000-0000-000000000001', 0, 'owner',  'Owner',  'Full control of the tenant', 1),
    ('00000000-0000-0000-0000-000000000002', 0, 'admin',  'Admin',  'Manages users, invitations, and knowledge bases', 1),
    ('00000000-0000-0000-0000-000000000003', 0, 'member', 'Member', 'Can chat, search, and create personal knowledge bases', 1),
    ('00000000-0000-0000-0000-000000000004', 0, 'viewer', 'Viewer', 'Read-only access via chat/search to permitted KBs', 1);

INSERT OR IGNORE INTO role_permissions (role_id, permission_key, allowed) VALUES
    ('00000000-0000-0000-0000-000000000001', 'chat', 1),
    ('00000000-0000-0000-0000-000000000001', 'search', 1),
    ('00000000-0000-0000-0000-000000000001', 'create_kb', 1),
    ('00000000-0000-0000-0000-000000000001', 'invite_users', 1),
    ('00000000-0000-0000-0000-000000000001', 'manage_users', 1),
    ('00000000-0000-0000-0000-000000000001', 'manage_kbs', 1),
    ('00000000-0000-0000-0000-000000000002', 'chat', 1),
    ('00000000-0000-0000-0000-000000000002', 'search', 1),
    ('00000000-0000-0000-0000-000000000002', 'create_kb', 1),
    ('00000000-0000-0000-0000-000000000002', 'invite_users', 1),
    ('00000000-0000-0000-0000-000000000002', 'manage_users', 1),
    ('00000000-0000-0000-0000-000000000002', 'manage_kbs', 1),
    ('00000000-0000-0000-0000-000000000003', 'chat', 1),
    ('00000000-0000-0000-0000-000000000003', 'search', 1),
    ('00000000-0000-0000-0000-000000000003', 'create_kb', 1),
    ('00000000-0000-0000-0000-000000000003', 'invite_users', 0),
    ('00000000-0000-0000-0000-000000000003', 'manage_users', 0),
    ('00000000-0000-0000-0000-000000000003', 'manage_kbs', 0),
    ('00000000-0000-0000-0000-000000000004', 'chat', 1),
    ('00000000-0000-0000-0000-000000000004', 'search', 1),
    ('00000000-0000-0000-0000-000000000004', 'create_kb', 0),
    ('00000000-0000-0000-0000-000000000004', 'invite_users', 0),
    ('00000000-0000-0000-0000-000000000004', 'manage_users', 0),
    ('00000000-0000-0000-0000-000000000004', 'manage_kbs', 0);

-- 4. User groups.
CREATE TABLE IF NOT EXISTS user_groups (
    id          VARCHAR(36) PRIMARY KEY,
    tenant_id   INTEGER NOT NULL,
    name        VARCHAR(128) NOT NULL,
    description TEXT,
    role_id     VARCHAR(36),
    permissions TEXT,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at  DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_groups_tenant_name
    ON user_groups(tenant_id, name COLLATE NOCASE)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_groups_tenant ON user_groups(tenant_id);
CREATE INDEX IF NOT EXISTS idx_user_groups_role ON user_groups(role_id);
CREATE INDEX IF NOT EXISTS idx_user_groups_deleted_at ON user_groups(deleted_at);

-- 5. Group memberships.
CREATE TABLE IF NOT EXISTS user_group_members (
    group_id   VARCHAR(36) NOT NULL,
    user_id    VARCHAR(36) NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_group_members_user ON user_group_members(user_id);
CREATE INDEX IF NOT EXISTS idx_user_group_members_group ON user_group_members(group_id);

-- 6. Group-level KB grants.
CREATE TABLE IF NOT EXISTS kb_group_permissions (
    id                 VARCHAR(36) PRIMARY KEY,
    knowledge_base_id  VARCHAR(36) NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    group_id           VARCHAR(36) NOT NULL,
    tenant_id          INTEGER NOT NULL,
    permission         VARCHAR(32) NOT NULL DEFAULT 'viewer',
    granted_by_user_id VARCHAR(36) NOT NULL,
    created_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at         DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_group_permissions_kb_group
    ON kb_group_permissions(knowledge_base_id, group_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_group_permissions_group ON kb_group_permissions(group_id);
CREATE INDEX IF NOT EXISTS idx_kb_group_permissions_kb ON kb_group_permissions(knowledge_base_id);
CREATE INDEX IF NOT EXISTS idx_kb_group_permissions_tenant ON kb_group_permissions(tenant_id);
