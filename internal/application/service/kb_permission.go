package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type kbPermissionService struct {
	repo     interfaces.KBPermissionRepository
	userRepo interfaces.UserRepository
	kbRepo   interfaces.KnowledgeBaseRepository
}

// NewKBPermissionService wires the per-user KB permission service.
func NewKBPermissionService(
	repo interfaces.KBPermissionRepository,
	userRepo interfaces.UserRepository,
	kbRepo interfaces.KnowledgeBaseRepository,
) interfaces.KBPermissionService {
	return &kbPermissionService{repo: repo, userRepo: userRepo, kbRepo: kbRepo}
}

// Grant adds (or revives) a permission for a user on a KB.
//
// Authorization: actor must either own the KB, hold an admin grant on it, or be a
// tenant-level admin/owner.
func (s *kbPermissionService) Grant(
	ctx context.Context, actor *types.User, kbID, userID string, permission types.KBPermission,
) (*types.KBUserPermission, error) {
	if actor == nil {
		return nil, errors.New("actor required")
	}
	if !permission.IsValid() {
		return nil, errors.New("invalid permission")
	}
	if err := s.requireKBAdmin(ctx, actor, kbID); err != nil {
		return nil, err
	}

	target, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if target.TenantID != actor.TenantID {
		return nil, errors.New("can only grant access to users in your tenant")
	}

	existing, err := s.repo.GetByKBAndUser(ctx, kbID, userID)
	if err == nil {
		// Re-use the existing row by updating its permission level.
		existing.Permission = permission
		existing.GrantedByUserID = actor.ID
		existing.UpdatedAt = time.Now()
		if uerr := s.repo.Update(ctx, existing); uerr != nil {
			return nil, uerr
		}
		return existing, nil
	}
	if !errors.Is(err, apprepo.ErrKBPermissionNotFound) {
		return nil, err
	}

	now := time.Now()
	grant := &types.KBUserPermission{
		ID:              uuid.New().String(),
		KnowledgeBaseID: kbID,
		UserID:          userID,
		TenantID:        actor.TenantID,
		Permission:      permission,
		GrantedByUserID: actor.ID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.repo.Create(ctx, grant); err != nil {
		return nil, err
	}
	return grant, nil
}

// UpdatePermission changes the permission level of an existing grant.
func (s *kbPermissionService) UpdatePermission(
	ctx context.Context, actor *types.User, grantID string, permission types.KBPermission,
) (*types.KBUserPermission, error) {
	if actor == nil {
		return nil, errors.New("actor required")
	}
	if !permission.IsValid() {
		return nil, errors.New("invalid permission")
	}
	grant, err := s.repo.GetByID(ctx, grantID)
	if err != nil {
		return nil, err
	}
	if err := s.requireKBAdmin(ctx, actor, grant.KnowledgeBaseID); err != nil {
		return nil, err
	}
	grant.Permission = permission
	grant.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, grant); err != nil {
		return nil, err
	}
	return grant, nil
}

// Revoke soft-deletes a grant.
func (s *kbPermissionService) Revoke(ctx context.Context, actor *types.User, grantID string) error {
	if actor == nil {
		return errors.New("actor required")
	}
	grant, err := s.repo.GetByID(ctx, grantID)
	if err != nil {
		return err
	}
	if err := s.requireKBAdmin(ctx, actor, grant.KnowledgeBaseID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, grantID)
}

// ListByKB returns all grants for a KB enriched with user display data.
func (s *kbPermissionService) ListByKB(
	ctx context.Context, actor *types.User, kbID string,
) ([]types.KBUserPermissionResponse, error) {
	if actor == nil {
		return nil, errors.New("actor required")
	}
	if err := s.requireKBAdmin(ctx, actor, kbID); err != nil {
		return nil, err
	}
	grants, err := s.repo.ListByKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if len(grants) == 0 {
		return []types.KBUserPermissionResponse{}, nil
	}

	// Collect related user IDs for a single batch lookup.
	idSet := make(map[string]struct{}, len(grants)*2)
	for _, g := range grants {
		idSet[g.UserID] = struct{}{}
		idSet[g.GrantedByUserID] = struct{}{}
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	users, err := s.userRepo.GetUsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	userMap := make(map[string]*types.User, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}

	out := make([]types.KBUserPermissionResponse, 0, len(grants))
	for _, g := range grants {
		u := userMap[g.UserID]
		grantedBy := userMap[g.GrantedByUserID]
		resp := types.KBUserPermissionResponse{
			ID:              g.ID,
			KnowledgeBaseID: g.KnowledgeBaseID,
			UserID:          g.UserID,
			Permission:      g.Permission,
			GrantedByUserID: g.GrantedByUserID,
			CreatedAt:       g.CreatedAt,
			UpdatedAt:       g.UpdatedAt,
		}
		if u != nil {
			resp.Username = u.Username
			resp.Email = u.Email
			resp.Avatar = u.Avatar
		}
		if grantedBy != nil {
			resp.GrantedByName = grantedBy.Username
		}
		out = append(out, resp)
	}
	return out, nil
}

// ResolvePermission returns the highest permission a user holds on a KB.
//
// Resolution order (highest wins):
//  1. Cross-tenant       -> not handled here (caller falls back to org-share path)
//  2. Tenant owner/admin -> admin
//  3. KB owner_id == user-> admin
//  4. Has explicit grant -> grant level
//  5. KB has ZERO grants -> viewer (preserves legacy tenant-wide visibility)
//  6. KB has grants but none for this user -> denied
//
// Rule 5+6 together mean: a KB stays tenant-wide-visible until someone adds the
// first explicit grant; once any grant exists, the KB becomes restricted to the
// granted users (plus tenant admins/owners and the KB owner).
func (s *kbPermissionService) ResolvePermission(
	ctx context.Context, user *types.User, kbID string,
) (types.KBPermission, bool, error) {
	if user == nil {
		return "", false, errors.New("user required")
	}
	kb, err := s.kbRepo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return "", false, err
	}
	if kb == nil {
		return "", false, errors.New("knowledge base not found")
	}
	return s.ResolvePermissionWithKB(ctx, user, kb)
}

// NEU: ResolvePermissionWithKB is the inner resolution that skips the KB lookup.
// Callers that already loaded the KB (e.g. handlers that called validateAndGetKnowledgeBase)
// should prefer this to avoid an extra GetKnowledgeBaseByID query per request.
func (s *kbPermissionService) ResolvePermissionWithKB(
	ctx context.Context, user *types.User, kb *types.KnowledgeBase,
) (types.KBPermission, bool, error) {
	if user == nil {
		return "", false, errors.New("user required")
	}
	if kb == nil {
		return "", false, errors.New("knowledge base required")
	}

	// Different tenant: no direct access (cross-tenant lives via org shares, handled elsewhere).
	if kb.TenantID != user.TenantID {
		return "", false, nil
	}

	// Tenant-level admins/owners always get full access.
	if user.Role == types.UserRoleOwner || user.Role == types.UserRoleAdmin {
		return types.KBPermissionAdmin, true, nil
	}
	// KB owner always gets full access.
	if kb.OwnerID != "" && kb.OwnerID == user.ID {
		return types.KBPermissionAdmin, true, nil
	}

	// Direct grant takes precedence over the legacy tenant-wide visibility.
	grant, err := s.repo.GetByKBAndUser(ctx, kb.ID, user.ID)
	if err == nil {
		return grant.Permission, true, nil
	}
	if !errors.Is(err, apprepo.ErrKBPermissionNotFound) {
		return "", false, err
	}

	// No grant for this user. Check if the KB has any grants at all.
	hasGrants, err := s.repo.KBsWithAnyGrants(ctx, []string{kb.ID})
	if err != nil {
		return "", false, err
	}
	if hasGrants[kb.ID] {
		// KB has grants but not for this user -> restricted, no access.
		return "", false, nil
	}
	// No grants exist on this KB anywhere -> tenant-wide viewer access (legacy compat).
	return types.KBPermissionViewer, true, nil
}

// FilterAccessibleSameTenant filters a list of KBs to those the user can view.
//
// Cross-tenant KBs are passed through unchanged; the caller is expected to apply
// org-share rules to those. For same-tenant KBs we apply the rules from
// ResolvePermission but with a single batch DB lookup instead of N queries.
func (s *kbPermissionService) FilterAccessibleSameTenant(
	ctx context.Context, user *types.User, kbs []*types.KnowledgeBase,
) ([]*types.KnowledgeBase, error) {
	if user == nil {
		return nil, errors.New("user required")
	}
	if len(kbs) == 0 {
		return kbs, nil
	}

	// Tenant admins/owners see everything in their tenant; nothing to filter.
	tenantAdmin := user.Role == types.UserRoleOwner || user.Role == types.UserRoleAdmin

	// Collect same-tenant KB IDs for the batch lookups.
	sameTenantIDs := make([]string, 0, len(kbs))
	for _, kb := range kbs {
		if kb != nil && kb.TenantID == user.TenantID {
			sameTenantIDs = append(sameTenantIDs, kb.ID)
		}
	}
	if len(sameTenantIDs) == 0 || tenantAdmin {
		return kbs, nil
	}

	grants, err := s.repo.ListGrantsForUserInKBs(ctx, user.ID, sameTenantIDs)
	if err != nil {
		return nil, err
	}
	userGrants := make(map[string]struct{}, len(grants))
	for _, g := range grants {
		userGrants[g.KnowledgeBaseID] = struct{}{}
	}

	hasGrants, err := s.repo.KBsWithAnyGrants(ctx, sameTenantIDs)
	if err != nil {
		return nil, err
	}

	out := make([]*types.KnowledgeBase, 0, len(kbs))
	for _, kb := range kbs {
		if kb == nil {
			continue
		}
		// Cross-tenant KB -> caller decides via org-share path.
		if kb.TenantID != user.TenantID {
			out = append(out, kb)
			continue
		}
		// KB owner sees their own KBs.
		if kb.OwnerID != "" && kb.OwnerID == user.ID {
			out = append(out, kb)
			continue
		}
		// Has explicit grant -> visible.
		if _, ok := userGrants[kb.ID]; ok {
			out = append(out, kb)
			continue
		}
		// No grants exist anywhere on this KB -> legacy tenant-wide visibility.
		if !hasGrants[kb.ID] {
			out = append(out, kb)
			continue
		}
		// KB has other grants but not for this user -> hidden.
	}
	return out, nil
}

// requireKBAdmin asserts the actor can manage the KB's permissions.
func (s *kbPermissionService) requireKBAdmin(ctx context.Context, actor *types.User, kbID string) error {
	perm, ok, err := s.ResolvePermission(ctx, actor, kbID)
	if err != nil {
		return fmt.Errorf("failed to resolve permission: %w", err)
	}
	if !ok || !perm.HasAtLeast(types.KBPermissionAdmin) {
		return errors.New("permission denied")
	}
	return nil
}
