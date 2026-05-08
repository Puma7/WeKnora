package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// fakeRoleRepo is a minimal in-memory RoleRepository implementing the methods
// the resolver actually calls. Other methods panic so the test surface is
// explicit about what's being exercised.
type fakeRoleRepo struct {
	rolesByKey  map[string]*types.Role
	permsByRole map[string]map[string]bool
}

func (r *fakeRoleRepo) GetRoleByKey(_ context.Context, tenantID uint64, key string) (*types.Role, error) {
	// Mirrors the production behavior: look up tenant-scoped first (we don't
	// model that here since the test only seeds globals), fall back to global.
	return r.rolesByKey[key], nil
}

func (r *fakeRoleRepo) GetPermissionsForRole(_ context.Context, roleID string) (map[string]bool, error) {
	return r.permsByRole[roleID], nil
}

// Unused methods — panic so a future signature change reaches us through the test surface.
func (r *fakeRoleRepo) CreateRole(context.Context, *types.Role) error                  { panic("not used") }
func (r *fakeRoleRepo) UpdateRole(context.Context, *types.Role) error                  { panic("not used") }
func (r *fakeRoleRepo) DeleteRole(context.Context, string) error                       { panic("not used") }
func (r *fakeRoleRepo) GetRoleByID(context.Context, string) (*types.Role, error)       { panic("not used") }
func (r *fakeRoleRepo) ListRolesForTenant(context.Context, uint64) ([]*types.Role, error) {
	panic("not used")
}
func (r *fakeRoleRepo) CountUsersWithRole(context.Context, uint64, string) (int64, error) {
	panic("not used")
}
func (r *fakeRoleRepo) CountGroupsWithRole(context.Context, string) (int64, error) {
	panic("not used")
}
func (r *fakeRoleRepo) LoadPermissions(context.Context, []*types.Role) error            { panic("not used") }
func (r *fakeRoleRepo) SetPermission(context.Context, string, string, bool) error       { panic("not used") }
func (r *fakeRoleRepo) ClearPermission(context.Context, string, string) error           { panic("not used") }

// seedSystemRoles populates the fake repo with the same role+permission shape
// the migration seeds in production. The whole point of this test file is to
// guarantee Resolve() output equals the previously hard-coded role map for
// every system role.
func seedSystemRoles(r *fakeRoleRepo) {
	r.rolesByKey = map[string]*types.Role{}
	r.permsByRole = map[string]map[string]bool{}
	add := func(id, key string, perms map[string]bool) {
		r.rolesByKey[key] = &types.Role{ID: id, Key: key, IsSystem: true}
		r.permsByRole[id] = perms
	}
	allTrue := map[string]bool{
		types.PermissionChat: true, types.PermissionSearch: true, types.PermissionCreateKB: true,
		types.PermissionInviteUsers: true, types.PermissionManageUsers: true, types.PermissionManageKBs: true,
	}
	add("role-owner", types.SystemRoleKeyOwner, allTrue)
	add("role-admin", types.SystemRoleKeyAdmin, allTrue)
	add("role-member", types.SystemRoleKeyMember, map[string]bool{
		types.PermissionChat: true, types.PermissionSearch: true, types.PermissionCreateKB: true,
		types.PermissionInviteUsers: false, types.PermissionManageUsers: false, types.PermissionManageKBs: false,
	})
	add("role-viewer", types.SystemRoleKeyViewer, map[string]bool{
		types.PermissionChat: true, types.PermissionSearch: true, types.PermissionCreateKB: false,
		types.PermissionInviteUsers: false, types.PermissionManageUsers: false, types.PermissionManageKBs: false,
	})
}

// boolPtrV converts a bool literal into *bool for the expected struct.
func boolPtrV(b bool) *bool { return &b }

// Test_Resolve_MatchesLegacyMap is the regression test for the invisible
// migration promise: every existing user's effective permissions stay
// byte-identical after switching to the DB-driven resolver.
func Test_Resolve_MatchesLegacyMap(t *testing.T) {
	repo := &fakeRoleRepo{}
	seedSystemRoles(repo)
	// No groups in this test path: pass nil so the resolver's groupRepo is
	// nil and step 2 is skipped.
	resolver := NewPermissionResolverService(repo, nil)

	cases := []struct {
		role     types.UserRole
		expected types.UserPermissions
	}{
		{
			role: types.UserRoleOwner,
			expected: types.UserPermissions{
				CanChat: boolPtrV(true), CanSearch: boolPtrV(true), CanCreateKB: boolPtrV(true),
				CanInviteUsers: boolPtrV(true), CanManageUsers: boolPtrV(true), CanManageKBs: boolPtrV(true),
			},
		},
		{
			role: types.UserRoleAdmin,
			expected: types.UserPermissions{
				CanChat: boolPtrV(true), CanSearch: boolPtrV(true), CanCreateKB: boolPtrV(true),
				CanInviteUsers: boolPtrV(true), CanManageUsers: boolPtrV(true), CanManageKBs: boolPtrV(true),
			},
		},
		{
			role: types.UserRoleMember,
			expected: types.UserPermissions{
				CanChat: boolPtrV(true), CanSearch: boolPtrV(true), CanCreateKB: boolPtrV(true),
				CanInviteUsers: boolPtrV(false), CanManageUsers: boolPtrV(false), CanManageKBs: boolPtrV(false),
			},
		},
		{
			role: types.UserRoleViewer,
			expected: types.UserPermissions{
				CanChat: boolPtrV(true), CanSearch: boolPtrV(true), CanCreateKB: boolPtrV(false),
				CanInviteUsers: boolPtrV(false), CanManageUsers: boolPtrV(false), CanManageKBs: boolPtrV(false),
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.role), func(t *testing.T) {
			user := &types.User{ID: "u1", TenantID: 42, Role: tc.role}
			got, err := resolver.Resolve(context.Background(), user)
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			if !equalPerms(got, &tc.expected) {
				t.Errorf("permissions mismatch for role %s\n  got:      %+v\n  expected: %+v",
					tc.role, ptrSnapshot(got), ptrSnapshot(&tc.expected))
			}
		})
	}
}

// Test_Resolve_UserOverrideWins verifies that an explicit user-level override
// flips a role-default flag. This is the per-user permissions feature.
func Test_Resolve_UserOverrideWins(t *testing.T) {
	repo := &fakeRoleRepo{}
	seedSystemRoles(repo)
	resolver := NewPermissionResolverService(repo, nil)

	// Member normally has manage_users=false. Override flips it to true.
	user := &types.User{ID: "u1", TenantID: 42, Role: types.UserRoleMember}
	override := types.UserPermissions{CanManageUsers: boolPtrV(true)}
	raw, err := types.MarshalToJSON(override)
	if err != nil {
		t.Fatalf("MarshalToJSON: %v", err)
	}
	user.Permissions = raw

	got, err := resolver.Resolve(context.Background(), user)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.CanManageUsers == nil || !*got.CanManageUsers {
		t.Errorf("expected CanManageUsers=true (override) but got %v", ptrSnapshot(got).CanManageUsers)
	}
	// The other member-defaults must still be intact.
	if got.CanCreateKB == nil || !*got.CanCreateKB {
		t.Errorf("expected CanCreateKB=true (member default) but got %v", ptrSnapshot(got).CanCreateKB)
	}
}

// Test_Resolve_SeedMissingReturnsError documents the safety: when the system
// role rows aren't seeded and the user's role is a system key, Resolve returns
// ErrRoleSeedMissing so the middleware falls back to the legacy hard-coded map
// instead of caching all-deny.
func Test_Resolve_SeedMissingReturnsError(t *testing.T) {
	repo := &fakeRoleRepo{rolesByKey: map[string]*types.Role{}, permsByRole: map[string]map[string]bool{}}
	resolver := NewPermissionResolverService(repo, nil)
	user := &types.User{ID: "u1", TenantID: 42, Role: types.UserRoleOwner}
	_, err := resolver.Resolve(context.Background(), user)
	if err != ErrRoleSeedMissing {
		t.Errorf("expected ErrRoleSeedMissing, got %v", err)
	}
}

// equalPerms compares two UserPermissions for value-equality across all
// pointer fields. Required because reflect.DeepEqual on different *bool
// addresses with the same dereferenced value still returns true, but writing
// the comparison ourselves makes test failures easier to read.
func equalPerms(a, b *types.UserPermissions) bool {
	eq := func(x, y *bool) bool {
		if x == nil || y == nil {
			return x == y
		}
		return *x == *y
	}
	return eq(a.CanChat, b.CanChat) &&
		eq(a.CanSearch, b.CanSearch) &&
		eq(a.CanCreateKB, b.CanCreateKB) &&
		eq(a.CanInviteUsers, b.CanInviteUsers) &&
		eq(a.CanManageUsers, b.CanManageUsers) &&
		eq(a.CanManageKBs, b.CanManageKBs)
}

// permSnapshot is a copy of UserPermissions with values dereferenced into
// optional bools (nil = not-set) for human-readable test output.
type permSnapshot struct {
	CanChat        *bool `json:"can_chat,omitempty"`
	CanSearch      *bool `json:"can_search,omitempty"`
	CanCreateKB    *bool `json:"can_create_kb,omitempty"`
	CanInviteUsers *bool `json:"can_invite_users,omitempty"`
	CanManageUsers *bool `json:"can_manage_users,omitempty"`
	CanManageKBs   *bool `json:"can_manage_kbs,omitempty"`
}

func ptrSnapshot(p *types.UserPermissions) permSnapshot {
	return permSnapshot{
		CanChat: p.CanChat, CanSearch: p.CanSearch, CanCreateKB: p.CanCreateKB,
		CanInviteUsers: p.CanInviteUsers, CanManageUsers: p.CanManageUsers, CanManageKBs: p.CanManageKBs,
	}
}
