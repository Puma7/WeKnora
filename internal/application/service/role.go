package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// roleService implements interfaces.RoleService.
//
// Authorization model: every mutation requires the actor to hold the
// "manage_users" permission and operate in their own tenant. The handler
// layer enforces the permission check before reaching here; the service still
// double-checks tenant scoping so a forged ID in the URL can't reach across
// tenants.
type roleService struct {
	roleRepo interfaces.RoleRepository
}

// NewRoleService constructs the service.
func NewRoleService(roleRepo interfaces.RoleRepository) interfaces.RoleService {
	return &roleService{roleRepo: roleRepo}
}

func (s *roleService) ListRoles(ctx context.Context, tenantID uint64) ([]*types.Role, error) {
	roles, err := s.roleRepo.ListRolesForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.roleRepo.LoadPermissions(ctx, roles); err != nil {
		return nil, err
	}
	return roles, nil
}

func (s *roleService) GetRole(ctx context.Context, tenantID uint64, roleID string) (*types.Role, error) {
	role, err := s.roleRepo.GetRoleByID(ctx, roleID)
	if err != nil {
		return nil, err
	}
	if !roleVisibleToTenant(role, tenantID) {
		return nil, errors.New("role not found")
	}
	if err := s.roleRepo.LoadPermissions(ctx, []*types.Role{role}); err != nil {
		return nil, err
	}
	return role, nil
}

// CreateRole inserts a tenant-scoped custom role. System roles are not
// creatable through this path — the seed migration is the authoritative
// source. We also reject keys that collide with the four reserved system
// keys to prevent confusion (a tenant-private "owner" role would shadow the
// global one and surprise admins).
func (s *roleService) CreateRole(ctx context.Context, actor *types.User, role *types.Role) (*types.Role, error) {
	if actor == nil {
		return nil, errors.New("actor required")
	}
	role.Key = types.NormalizeRoleKey(role.Key)
	if err := validateRoleInput(role); err != nil {
		return nil, err
	}
	if types.IsSystemRoleKey(role.Key) {
		return nil, errors.New("cannot create a role with a reserved system key")
	}
	// Always tenant-scoped through this API.
	role.ID = uuid.New().String()
	role.TenantID = actor.TenantID
	role.IsSystem = false
	role.CreatedAt = time.Now()
	role.UpdatedAt = role.CreatedAt

	// Reject duplicate key inside the same tenant.
	if existing, err := s.roleRepo.GetRoleByKey(ctx, actor.TenantID, role.Key); err != nil {
		return nil, err
	} else if existing != nil && existing.TenantID == actor.TenantID {
		return nil, fmt.Errorf("role with key %q already exists", role.Key)
	}

	if err := s.roleRepo.CreateRole(ctx, role); err != nil {
		return nil, err
	}
	role.Permissions = map[string]bool{}
	return role, nil
}

func (s *roleService) UpdateRole(ctx context.Context, actor *types.User, role *types.Role) (*types.Role, error) {
	if actor == nil {
		return nil, errors.New("actor required")
	}
	existing, err := s.roleRepo.GetRoleByID(ctx, role.ID)
	if err != nil {
		return nil, err
	}
	if existing.IsSystem {
		return nil, errors.New("system roles cannot be modified")
	}
	if existing.TenantID != actor.TenantID {
		return nil, errors.New("role belongs to a different tenant")
	}
	role.Key = types.NormalizeRoleKey(role.Key)
	if err := validateRoleInput(role); err != nil {
		return nil, err
	}
	// Key change check: refuse if the new key collides inside the tenant.
	if role.Key != existing.Key {
		if conflict, err := s.roleRepo.GetRoleByKey(ctx, actor.TenantID, role.Key); err != nil {
			return nil, err
		} else if conflict != nil && conflict.TenantID == actor.TenantID && conflict.ID != existing.ID {
			return nil, fmt.Errorf("role with key %q already exists", role.Key)
		}
	}
	existing.Key = role.Key
	existing.Label = role.Label
	existing.Description = role.Description
	existing.UpdatedAt = time.Now()
	if err := s.roleRepo.UpdateRole(ctx, existing); err != nil {
		return nil, err
	}
	if err := s.roleRepo.LoadPermissions(ctx, []*types.Role{existing}); err != nil {
		return nil, err
	}
	return existing, nil
}

// DeleteRole refuses if any user or group still references the role.
// The error message is structured ("role_in_use") so the frontend can show a
// distinct message vs. generic 400s.
func (s *roleService) DeleteRole(ctx context.Context, actor *types.User, roleID string) error {
	if actor == nil {
		return errors.New("actor required")
	}
	role, err := s.roleRepo.GetRoleByID(ctx, roleID)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return errors.New("system roles cannot be deleted")
	}
	if role.TenantID != actor.TenantID {
		return errors.New("role belongs to a different tenant")
	}
	if userCount, err := s.roleRepo.CountUsersWithRole(ctx, actor.TenantID, role.Key); err != nil {
		return err
	} else if userCount > 0 {
		return fmt.Errorf("role_in_use: %d user(s) still hold this role", userCount)
	}
	if groupCount, err := s.roleRepo.CountGroupsWithRole(ctx, role.ID); err != nil {
		return err
	} else if groupCount > 0 {
		return fmt.Errorf("role_in_use: %d group(s) still reference this role", groupCount)
	}
	return s.roleRepo.DeleteRole(ctx, roleID)
}

// SetRolePermission accepts allowed=nil to clear the row (revert to default).
// System roles are immutable here; an operator wanting to diverge must create
// a tenant-scoped custom role and assign users to it instead.
func (s *roleService) SetRolePermission(ctx context.Context, actor *types.User, roleID, permissionKey string, allowed *bool) error {
	if actor == nil {
		return errors.New("actor required")
	}
	if !types.IsKnownPermissionKey(permissionKey) {
		return fmt.Errorf("unknown permission key %q", permissionKey)
	}
	role, err := s.roleRepo.GetRoleByID(ctx, roleID)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return errors.New("system role permissions cannot be modified")
	}
	if role.TenantID != actor.TenantID {
		return errors.New("role belongs to a different tenant")
	}
	if allowed == nil {
		return s.roleRepo.ClearPermission(ctx, roleID, permissionKey)
	}
	return s.roleRepo.SetPermission(ctx, roleID, permissionKey, *allowed)
}

func validateRoleInput(role *types.Role) error {
	if strings.TrimSpace(role.Label) == "" {
		return errors.New("role label is required")
	}
	if role.Key == "" {
		return errors.New("role key is required")
	}
	if len(role.Key) > 64 {
		return errors.New("role key too long")
	}
	for _, r := range role.Key {
		if !(r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return errors.New("role key may only contain a-z, 0-9, dash, and underscore")
		}
	}
	return nil
}

// roleVisibleToTenant reports whether the role is in the tenant's scope.
// System roles (TenantID = 0) are visible to every tenant.
func roleVisibleToTenant(role *types.Role, tenantID uint64) bool {
	return role.TenantID == 0 || role.TenantID == tenantID
}
