package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ErrRoleSeedMissing signals that the migration that seeds system role rows
// has not run successfully against the current DB. The middleware translates
// this to "skip cache population" so the legacy hard-coded fallback in
// User.EffectivePermissions() takes over rather than locking everyone out.
var ErrRoleSeedMissing = errors.New("system role rows not seeded — run migration 000043_rbac_v2")

// permissionResolverService implements interfaces.PermissionResolverService.
//
// Resolution order (highest precedence wins):
//  1. user-explicit override (users.permissions JSON map)
//  2. group override (any group the user belongs to with an override on the flag,
//     OR a group with a base role that grants the flag)
//  3. user's own role (DB-backed, falling back to system role of the same key)
//  4. catalog default (currently always false — no flag is on by default)
//
// The resolver is stateless (no in-memory cache). Permission lookups happen on
// every authenticated request — they are cheap (one query for groups, one for
// role permissions, both indexed) and a stale cache here would be much worse
// UX than the few extra ms.
type permissionResolverService struct {
	roleRepo  interfaces.RoleRepository
	groupRepo interfaces.GroupRepository
}

// NewPermissionResolverService constructs the resolver. groupRepo may be nil
// in tests / minimal installs that don't enable groups; resolver step 2 is then
// skipped silently.
func NewPermissionResolverService(
	roleRepo interfaces.RoleRepository,
	groupRepo interfaces.GroupRepository,
) interfaces.PermissionResolverService {
	return &permissionResolverService{roleRepo: roleRepo, groupRepo: groupRepo}
}

// Resolve runs the 4-step chain and returns a fully-populated permission map.
// "Fully-populated" means every catalog flag is set to a definite bool — no
// nil pointers — so callers can read the result without nil-checks. This is
// the format the auth middleware caches on user.EffectiveCache, and it's also
// the shape returned to the frontend in /auth/me.
func (s *permissionResolverService) Resolve(ctx context.Context, user *types.User) (*types.UserPermissions, error) {
	if user == nil {
		// Safety: an empty (denied) result is preferable to nil so callers can
		// keep their "if !user.Can(flag)" pattern without nil panics.
		return emptyPermissions(), nil
	}

	// Start with system defaults (everything false). Each resolution step may
	// flip flags upward (allow) or downward (deny).
	out := emptyPermissions()

	// Step 3: user's role base. We compute this first so it forms the floor
	// that group/user overrides can lift or push down.
	rolePerms, err := s.permissionsForRoleKey(ctx, user.TenantID, string(user.Role))
	if err != nil {
		return nil, err
	}
	// Safety: if the user's role is one of the four reserved system keys but
	// the lookup found fewer permission rows than the catalog defines, the
	// seed migration probably ran partially (transient DB error mid-migration
	// that wasn't rolled back atomically by the SQLite migrate driver, manual
	// schema edit, etc.). Returning ErrRoleSeedMissing causes the middleware
	// to skip cache population, falling back to the legacy hard-coded map in
	// User.EffectivePermissions(). Better than caching a partial result which
	// would silently lock owners out of their own admin features.
	// GEÄNDERT: was len==0; now compares against catalog length so a partial
	// seed (e.g. 3/6 rows) also triggers the safety fallback.
	if types.IsSystemRoleKey(string(user.Role)) && len(rolePerms) < len(types.DefaultPermissionCatalog) {
		return nil, ErrRoleSeedMissing
	}
	applyMapToPermissions(out, rolePerms)

	// Step 2: group resolution. We OR group-allows on top of role base, but
	// group-deny overrides role-allow (last-decision-wins inside groups too:
	// stable order via repo's ORDER BY id, so the same group set always
	// resolves identically regardless of which one was edited last).
	if s.groupRepo != nil {
		groups, err := s.groupRepo.ListGroupsForUser(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		// GEÄNDERT: batch-load all group-role permission maps in one DB
		// roundtrip instead of one per group. With 10 groups that's 1 query
		// instead of 10. Empty input returns empty map cleanly.
		groupRoleIDs := make([]string, 0, len(groups))
		for _, g := range groups {
			if g.RoleID != nil && *g.RoleID != "" {
				groupRoleIDs = append(groupRoleIDs, *g.RoleID)
			}
		}
		var groupRolePermsByID map[string]map[string]bool
		if len(groupRoleIDs) > 0 {
			groupRolePermsByID, err = s.roleRepo.GetPermissionsForRoles(ctx, groupRoleIDs)
			if err != nil {
				return nil, err
			}
		}
		for _, g := range groups {
			// 2a: group's role grants (read from the batch result)
			if g.RoleID != nil && *g.RoleID != "" {
				if rolePerms, ok := groupRolePermsByID[*g.RoleID]; ok {
					applyMapToPermissions(out, rolePerms)
				}
			}
			// 2b: group's per-flag overrides (highest precedence within group)
			if g.Permissions != nil {
				var overrides types.UserPermissions
				if uerr := g.Permissions.Unmarshal(&overrides); uerr != nil {
					// GEÄNDERT: was silently swallowed. Corrupt JSON in a
					// group permissions row used to evaporate the override
					// without trace; now an operator gets a log line they
					// can grep for. We deliberately keep going (override
					// not applied = same observable effect) so a single bad
					// row doesn't lock everyone out of the tenant.
					logger.Warnf(ctx, "permission resolver: failed to unmarshal group %s permissions: %v", g.ID, uerr)
				} else {
					applyOverrides(out, &overrides)
				}
			}
		}
	}

	// Step 1: user-explicit override (highest precedence overall).
	if user.Permissions != nil {
		var overrides types.UserPermissions
		if uerr := user.Permissions.Unmarshal(&overrides); uerr != nil {
			// GEÄNDERT: same logging fix as the group overrides path above.
			logger.Warnf(ctx, "permission resolver: failed to unmarshal user %s permissions: %v", user.ID, uerr)
		} else {
			applyOverrides(out, &overrides)
		}
	}

	return out, nil
}

// PopulateCache is the per-request convenience wrapper used by middleware.
func (s *permissionResolverService) PopulateCache(ctx context.Context, user *types.User) error {
	if user == nil {
		return nil
	}
	resolved, err := s.Resolve(ctx, user)
	if err != nil {
		return err
	}
	user.EffectiveCache = resolved
	return nil
}

// PermissionCatalog exposes the catalog so the admin UI can render the matrix
// without hard-coding the flag list on the frontend.
func (s *permissionResolverService) PermissionCatalog() []types.PermissionCatalogEntry {
	return types.DefaultPermissionCatalog
}

// permissionsForRoleKey looks up the role by (tenant, key) and returns its
// matrix row. Falls through to system role if no tenant-scoped row exists.
// Returns an empty (all-false) map when no role can be found — defense against
// a renamed/deleted role: the user is denied everything role-driven but their
// per-user / per-group overrides still apply on top.
func (s *permissionResolverService) permissionsForRoleKey(ctx context.Context, tenantID uint64, key string) (map[string]bool, error) {
	if key == "" {
		return map[string]bool{}, nil
	}
	role, err := s.roleRepo.GetRoleByKey(ctx, tenantID, key)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return map[string]bool{}, nil
	}
	return s.roleRepo.GetPermissionsForRole(ctx, role.ID)
}

// GEÄNDERT: removed unused permissionsForRoleID helper. Group role permissions
// are now batch-loaded via GetPermissionsForRoles in the resolver hot path.

// emptyPermissions returns a UserPermissions with every flag set to a definite
// false pointer. This makes downstream merging straightforward (no nil
// branches) and matches the wire format the frontend expects.
func emptyPermissions() *types.UserPermissions {
	f := false
	return &types.UserPermissions{
		CanChat:        boolp(f),
		CanSearch:      boolp(f),
		CanCreateKB:    boolp(f),
		CanInviteUsers: boolp(f),
		CanManageUsers: boolp(f),
		CanManageKBs:   boolp(f),
	}
}

// applyMapToPermissions merges a raw flag map (from role_permissions rows)
// onto the running result. Each row that exists overrides the running value
// with its allow/deny — absence of a row means "leave the running value".
func applyMapToPermissions(dst *types.UserPermissions, src map[string]bool) {
	for k, v := range src {
		val := v
		switch k {
		case types.PermissionChat:
			dst.CanChat = &val
		case types.PermissionSearch:
			dst.CanSearch = &val
		case types.PermissionCreateKB:
			dst.CanCreateKB = &val
		case types.PermissionInviteUsers:
			dst.CanInviteUsers = &val
		case types.PermissionManageUsers:
			dst.CanManageUsers = &val
		case types.PermissionManageKBs:
			dst.CanManageKBs = &val
		}
	}
}

// applyOverrides merges a sparse UserPermissions onto the running result.
// Pointer semantics matter: a nil pointer means "use what's already there",
// a non-nil pointer (even pointing to false) means "this is the explicit
// decision, override".
func applyOverrides(dst, src *types.UserPermissions) {
	if src.CanChat != nil {
		dst.CanChat = src.CanChat
	}
	if src.CanSearch != nil {
		dst.CanSearch = src.CanSearch
	}
	if src.CanCreateKB != nil {
		dst.CanCreateKB = src.CanCreateKB
	}
	if src.CanInviteUsers != nil {
		dst.CanInviteUsers = src.CanInviteUsers
	}
	if src.CanManageUsers != nil {
		dst.CanManageUsers = src.CanManageUsers
	}
	if src.CanManageKBs != nil {
		dst.CanManageKBs = src.CanManageKBs
	}
}

func boolp(b bool) *bool { return &b }
