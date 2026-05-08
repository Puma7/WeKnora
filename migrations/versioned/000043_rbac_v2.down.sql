-- Migration: 000043_rbac_v2 (down)
-- Removes the RBAC v2 tables. Existing users.role / users.permissions remain
-- intact; they continue to be honoured by the legacy hard-coded fallback in
-- internal/types/user.go EffectivePermissions().

DROP TABLE IF EXISTS kb_group_permissions;
DROP TABLE IF EXISTS user_group_members;
DROP TABLE IF EXISTS user_groups;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
