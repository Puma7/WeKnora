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

// groupService implements interfaces.GroupService.
//
// Authorization: every mutation requires the actor to hold "manage_users".
// All operations are tenant-scoped — a forged group ID from a different
// tenant returns "group not found" rather than the cross-tenant data.
type groupService struct {
	groupRepo interfaces.GroupRepository
	roleRepo  interfaces.RoleRepository
	userRepo  interfaces.UserRepository
}

// NewGroupService constructs the service.
func NewGroupService(
	groupRepo interfaces.GroupRepository,
	roleRepo interfaces.RoleRepository,
	userRepo interfaces.UserRepository,
) interfaces.GroupService {
	return &groupService{groupRepo: groupRepo, roleRepo: roleRepo, userRepo: userRepo}
}

func (s *groupService) ListGroups(ctx context.Context, tenantID uint64) ([]*types.UserGroup, error) {
	groups, err := s.groupRepo.ListGroupsForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	// Hydrate member count for the list view. Members themselves are loaded
	// lazily by GetGroup so the list call stays cheap on tenants with many
	// groups + many members.
	for _, g := range groups {
		if c, err := s.groupRepo.CountMembers(ctx, g.ID); err == nil {
			g.MemberCount = c
		}
	}
	return groups, nil
}

func (s *groupService) GetGroup(ctx context.Context, tenantID uint64, groupID string) (*types.UserGroup, error) {
	g, err := s.groupRepo.GetGroupByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if g.TenantID != tenantID {
		return nil, errors.New("group not found")
	}
	memberIDs, err := s.groupRepo.ListMembers(ctx, groupID)
	if err != nil {
		return nil, err
	}
	g.MemberIDs = memberIDs
	g.MemberCount = len(memberIDs)
	return g, nil
}

func (s *groupService) CreateGroup(ctx context.Context, actor *types.User, g *types.UserGroup) (*types.UserGroup, error) {
	if actor == nil {
		return nil, errors.New("actor required")
	}
	if err := validateGroupInput(g); err != nil {
		return nil, err
	}
	g.ID = uuid.New().String()
	g.TenantID = actor.TenantID
	g.CreatedAt = time.Now()
	g.UpdatedAt = g.CreatedAt
	// Role assignment must point at a role visible to the tenant.
	if g.RoleID != nil && *g.RoleID != "" {
		if err := s.assertRoleVisible(ctx, actor.TenantID, *g.RoleID); err != nil {
			return nil, err
		}
	}
	if err := s.groupRepo.CreateGroup(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *groupService) UpdateGroup(ctx context.Context, actor *types.User, g *types.UserGroup) (*types.UserGroup, error) {
	if actor == nil {
		return nil, errors.New("actor required")
	}
	existing, err := s.groupRepo.GetGroupByID(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	if existing.TenantID != actor.TenantID {
		return nil, errors.New("group not found")
	}
	if err := validateGroupInput(g); err != nil {
		return nil, err
	}
	if g.RoleID != nil && *g.RoleID != "" {
		if err := s.assertRoleVisible(ctx, actor.TenantID, *g.RoleID); err != nil {
			return nil, err
		}
	}
	existing.Name = g.Name
	existing.Description = g.Description
	existing.RoleID = g.RoleID
	existing.Permissions = g.Permissions
	existing.UpdatedAt = time.Now()
	if err := s.groupRepo.UpdateGroup(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *groupService) DeleteGroup(ctx context.Context, actor *types.User, groupID string) error {
	if actor == nil {
		return errors.New("actor required")
	}
	g, err := s.groupRepo.GetGroupByID(ctx, groupID)
	if err != nil {
		return err
	}
	if g.TenantID != actor.TenantID {
		return errors.New("group not found")
	}
	return s.groupRepo.DeleteGroup(ctx, groupID)
}

func (s *groupService) AddMember(ctx context.Context, actor *types.User, groupID, userID string) error {
	if actor == nil {
		return errors.New("actor required")
	}
	g, err := s.groupRepo.GetGroupByID(ctx, groupID)
	if err != nil {
		return err
	}
	if g.TenantID != actor.TenantID {
		return errors.New("group not found")
	}
	target, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	// Prevent cross-tenant membership: a user can only belong to groups in
	// their own tenant. Attempting otherwise is treated as a 404 to avoid
	// leaking the existence of users from other tenants.
	if target.TenantID != actor.TenantID {
		return errors.New("user not found")
	}
	return s.groupRepo.AddMember(ctx, groupID, userID)
}

func (s *groupService) RemoveMember(ctx context.Context, actor *types.User, groupID, userID string) error {
	if actor == nil {
		return errors.New("actor required")
	}
	g, err := s.groupRepo.GetGroupByID(ctx, groupID)
	if err != nil {
		return err
	}
	if g.TenantID != actor.TenantID {
		return errors.New("group not found")
	}
	return s.groupRepo.RemoveMember(ctx, groupID, userID)
}

// ListMembers returns the actual user records, not just IDs, so the admin UI
// can render the table without a second round-trip per member.
func (s *groupService) ListMembers(ctx context.Context, tenantID uint64, groupID string) ([]*types.User, error) {
	g, err := s.groupRepo.GetGroupByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if g.TenantID != tenantID {
		return nil, errors.New("group not found")
	}
	ids, err := s.groupRepo.ListMembers(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []*types.User{}, nil
	}
	return s.userRepo.GetUsersByIDs(ctx, ids)
}

// SetGroupPermission updates the JSON map on the group row. Pass allowed=nil
// to clear the override.
func (s *groupService) SetGroupPermission(ctx context.Context, actor *types.User, groupID, permissionKey string, allowed *bool) error {
	if actor == nil {
		return errors.New("actor required")
	}
	if !types.IsKnownPermissionKey(permissionKey) {
		return fmt.Errorf("unknown permission key %q", permissionKey)
	}
	g, err := s.groupRepo.GetGroupByID(ctx, groupID)
	if err != nil {
		return err
	}
	if g.TenantID != actor.TenantID {
		return errors.New("group not found")
	}
	current := types.UserPermissions{}
	if g.Permissions != nil {
		_ = g.Permissions.Unmarshal(&current)
	}
	switch permissionKey {
	case types.PermissionChat:
		current.CanChat = allowed
	case types.PermissionSearch:
		current.CanSearch = allowed
	case types.PermissionCreateKB:
		current.CanCreateKB = allowed
	case types.PermissionInviteUsers:
		current.CanInviteUsers = allowed
	case types.PermissionManageUsers:
		current.CanManageUsers = allowed
	case types.PermissionManageKBs:
		current.CanManageKBs = allowed
	}
	// All-nil overrides means "no overrides anymore" — store NULL so the
	// resolver doesn't waste a JSON parse on every request.
	allNil := current.CanChat == nil && current.CanSearch == nil && current.CanCreateKB == nil &&
		current.CanInviteUsers == nil && current.CanManageUsers == nil && current.CanManageKBs == nil
	if allNil {
		g.Permissions = nil
	} else {
		raw, err := types.MarshalToJSON(current)
		if err != nil {
			return err
		}
		g.Permissions = raw
	}
	g.UpdatedAt = time.Now()
	return s.groupRepo.UpdateGroup(ctx, g)
}

func (s *groupService) assertRoleVisible(ctx context.Context, tenantID uint64, roleID string) error {
	role, err := s.roleRepo.GetRoleByID(ctx, roleID)
	if err != nil {
		return err
	}
	if !roleVisibleToTenant(role, tenantID) {
		return errors.New("role not found in tenant scope")
	}
	return nil
}

func validateGroupInput(g *types.UserGroup) error {
	if strings.TrimSpace(g.Name) == "" {
		return errors.New("group name is required")
	}
	if len(g.Name) > 128 {
		return errors.New("group name too long")
	}
	return nil
}
