-- SQLite migration 000003 (down). Removes RBAC v2 tables.

DROP TABLE IF EXISTS kb_group_permissions;
DROP TABLE IF EXISTS user_group_members;
DROP TABLE IF EXISTS user_groups;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
