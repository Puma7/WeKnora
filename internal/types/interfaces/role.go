package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// RoleService is the tenant-facing API for managing roles + their permission matrix.
//
// Visibility rules baked into all methods:
//   - Listing returns system roles (TenantID = 0) plus tenant-scoped custom roles
//   - Mutations on system roles are rejected at the service layer (system roles
//     are seeded by migration and represent the contract; if a tenant wants to
//     diverge they create a custom role with the same key as override)
//   - All callers must hold "manage_users" — handlers enforce this before
//     reaching the service
type RoleService interface {
	// ListRoles returns all roles visible to the supplied tenant (system + own).
	// Each role is hydrated with its permission map.
	ListRoles(ctx context.Context, tenantID uint64) ([]*types.Role, error)
	// GetRole returns one role by ID, hydrating its permission map. Tenant
	// scoping is enforced so a tenant cannot read another tenant's custom roles.
	GetRole(ctx context.Context, tenantID uint64, roleID string) (*types.Role, error)
	// CreateRole inserts a tenant-scoped custom role. System roles are not
	// creatable through this path (the seed migration is the source of truth).
	CreateRole(ctx context.Context, actor *types.User, role *types.Role) (*types.Role, error)
	// UpdateRole renames / re-describes a role. System roles are immutable.
	UpdateRole(ctx context.Context, actor *types.User, role *types.Role) (*types.Role, error)
	// DeleteRole soft-deletes a tenant role. Refused with reason="role_in_use"
	// when any user or group currently references it.
	DeleteRole(ctx context.Context, actor *types.User, roleID string) error
	// SetRolePermission upserts a single matrix cell. allowed=nil clears the
	// row, falling the role back to the system default for that flag.
	SetRolePermission(ctx context.Context, actor *types.User, roleID, permissionKey string, allowed *bool) error
}

// RoleRepository is the storage layer for roles + role_permissions.
type RoleRepository interface {
	CreateRole(ctx context.Context, role *types.Role) error
	UpdateRole(ctx context.Context, role *types.Role) error
	DeleteRole(ctx context.Context, roleID string) error
	GetRoleByID(ctx context.Context, roleID string) (*types.Role, error)
	// GetRoleByKey looks up a role visible to the supplied tenant.
	// Resolution: tenant-scoped row first (TenantID match), then global system
	// row (TenantID = 0). Returns nil, nil when no match.
	GetRoleByKey(ctx context.Context, tenantID uint64, key string) (*types.Role, error)
	ListRolesForTenant(ctx context.Context, tenantID uint64) ([]*types.Role, error)
	// CountUsersWithRole returns the number of users currently using this role
	// key in the tenant. Used to refuse deletion of an in-use role.
	CountUsersWithRole(ctx context.Context, tenantID uint64, roleKey string) (int64, error)
	// CountGroupsWithRole returns the number of groups currently assigned this
	// role. Used to refuse deletion.
	CountGroupsWithRole(ctx context.Context, roleID string) (int64, error)

	// LoadPermissions hydrates the Permissions map on each role from the
	// role_permissions table. Used by ListRoles + GetRole to avoid N queries.
	LoadPermissions(ctx context.Context, roles []*types.Role) error
	// GetPermissionsForRole returns the matrix row for one role.
	GetPermissionsForRole(ctx context.Context, roleID string) (map[string]bool, error)
	// SetPermission upserts one (role, key) pair.
	SetPermission(ctx context.Context, roleID, key string, allowed bool) error
	// ClearPermission deletes one (role, key) pair so the system default applies.
	ClearPermission(ctx context.Context, roleID, key string) error
}

// GroupService is the tenant-facing API for user groups.
type GroupService interface {
	ListGroups(ctx context.Context, tenantID uint64) ([]*types.UserGroup, error)
	GetGroup(ctx context.Context, tenantID uint64, groupID string) (*types.UserGroup, error)
	CreateGroup(ctx context.Context, actor *types.User, g *types.UserGroup) (*types.UserGroup, error)
	UpdateGroup(ctx context.Context, actor *types.User, g *types.UserGroup) (*types.UserGroup, error)
	DeleteGroup(ctx context.Context, actor *types.User, groupID string) error
	AddMember(ctx context.Context, actor *types.User, groupID, userID string) error
	RemoveMember(ctx context.Context, actor *types.User, groupID, userID string) error
	ListMembers(ctx context.Context, tenantID uint64, groupID string) ([]*types.User, error)
	// SetGroupPermission upserts a single per-flag override on the group.
	// Pass allowed=nil to clear (revert to the group role / user role default).
	SetGroupPermission(ctx context.Context, actor *types.User, groupID, permissionKey string, allowed *bool) error
}

// GroupRepository is the storage layer for groups + memberships + KB grants.
type GroupRepository interface {
	CreateGroup(ctx context.Context, g *types.UserGroup) error
	UpdateGroup(ctx context.Context, g *types.UserGroup) error
	DeleteGroup(ctx context.Context, groupID string) error
	GetGroupByID(ctx context.Context, groupID string) (*types.UserGroup, error)
	ListGroupsForTenant(ctx context.Context, tenantID uint64) ([]*types.UserGroup, error)
	// ListGroupsForUser returns all groups the user belongs to. Used by the
	// resolver's group-resolution step.
	ListGroupsForUser(ctx context.Context, userID string) ([]*types.UserGroup, error)

	AddMember(ctx context.Context, groupID, userID string) error
	RemoveMember(ctx context.Context, groupID, userID string) error
	ListMembers(ctx context.Context, groupID string) ([]string, error)
	CountMembers(ctx context.Context, groupID string) (int, error)

	// CreateKBGrant / RemoveKBGrant manage group-level KB permissions.
	CreateKBGrant(ctx context.Context, g *types.GroupKBPermission) error
	RemoveKBGrant(ctx context.Context, grantID string) error
	ListKBGrantsForKB(ctx context.Context, kbID string) ([]*types.GroupKBPermission, error)
	// ListKBGrantsForUser returns the union of KB grants from every group the
	// user is in. Used to merge into ResolvePermission.
	ListKBGrantsForUser(ctx context.Context, userID string) ([]*types.GroupKBPermission, error)
}

// PermissionResolverService is the central authority for "can user U do flag F".
//
// It runs the 4-step resolution chain (user override → group override → user
// role → system default) and is called from:
//   - the auth middleware (to populate user.EffectiveCache for every request)
//   - the kb permission service (to merge group KB grants with direct grants)
//
// All methods take the user as parameter rather than reading it from context
// so the resolver can be called for a target user during impersonation /
// admin operations.
type PermissionResolverService interface {
	// Resolve returns the effective permission map for the user. Cheap to call
	// (everything is cached in-process for the duration of the request); always
	// returns a fully-populated map with every catalog flag set to a definite
	// bool, never a nil pointer.
	Resolve(ctx context.Context, user *types.User) (*types.UserPermissions, error)
	// PopulateCache is a convenience wrapper used by the auth middleware: it
	// calls Resolve and stamps the result onto user.EffectiveCache. Errors are
	// returned but the caller may choose to log-and-continue, in which case the
	// user falls back to the legacy hard-coded resolution path.
	PopulateCache(ctx context.Context, user *types.User) error
	// PermissionCatalog returns the registered feature-flag catalog so the
	// admin UI can render the matrix without hard-coding the list on the FE.
	PermissionCatalog() []types.PermissionCatalogEntry
}
