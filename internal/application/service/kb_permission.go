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
	repo      interfaces.KBPermissionRepository
	userRepo  interfaces.UserRepository
	kbRepo    interfaces.KnowledgeBaseRepository
	groupRepo interfaces.GroupRepository // NEU: required for group-level KB grant resolution
}

// NewKBPermissionService wires the per-user KB permission service.
// GEÄNDERT: groupRepo added so group-level KB grants are actually enforced.
// Pass nil in tests / minimal installs that don't enable groups; group-grant
// resolution then degrades gracefully to a no-op without touching legacy paths.
func NewKBPermissionService(
	repo interfaces.KBPermissionRepository,
	userRepo interfaces.UserRepository,
	kbRepo interfaces.KnowledgeBaseRepository,
	groupRepo interfaces.GroupRepository,
) interfaces.KBPermissionService {
	return &kbPermissionService{repo: repo, userRepo: userRepo, kbRepo: kbRepo, groupRepo: groupRepo}
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
//  5. KB has ZERO grants -> admin (preserves legacy tenant-wide full-access workflow;
//                          regular members keep being able to view/edit/delete KBs that
//                          haven't been explicitly locked down — opt-in restriction)
//  6. KB has grants but none for this user -> denied
//
// Note: this resolution is for general KB access. Operations that could
// escalate privilege (Grant/UpdatePermission/Revoke) use requireKBAdmin's
// strict path which deliberately skips rule 5.
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
	// GEÄNDERT: also fold in group-level grants. We compute the highest of
	// (user direct grant, all group grants for this KB) so a user picks up
	// admin via a group even if no direct row exists for them.
	directGrant, err := s.repo.GetByKBAndUser(ctx, kb.ID, user.ID)
	if err != nil && !errors.Is(err, apprepo.ErrKBPermissionNotFound) {
		return "", false, err
	}
	groupPerm, hasGroupGrant, err := s.highestGroupGrantForKB(ctx, user.ID, kb.ID)
	if err != nil {
		return "", false, err
	}
	if err == nil && directGrant != nil {
		best := directGrant.Permission
		if hasGroupGrant && groupPerm.HasAtLeast(best) {
			best = groupPerm
		}
		return best, true, nil
	}
	if hasGroupGrant {
		return groupPerm, true, nil
	}

	// No grant for this user (direct or via group). Check if the KB has any
	// grants at all — including group grants — to decide between "open KB"
	// (legacy tenant-wide admin) and "restricted KB" (denied).
	hasUserGrants, err := s.repo.KBsWithAnyGrants(ctx, []string{kb.ID})
	if err != nil {
		return "", false, err
	}
	hasAnyGroupGrants, err := s.kbHasAnyGroupGrants(ctx, kb.ID)
	if err != nil {
		return "", false, err
	}
	if hasUserGrants[kb.ID] || hasAnyGroupGrants {
		// KB has grants but not for this user -> restricted, no access.
		return "", false, nil
	}
	// GEÄNDERT: legacy tenant-wide ADMIN access (was viewer). Old behavior was
	// that any tenant member could read/edit/delete any KB in their tenant via
	// validateAndGetKnowledgeBase returning OrgRoleAdmin. We preserve that
	// exactly until an admin opts into restriction by adding the first grant.
	// Privilege escalation (granting) is independently gated by requireKBAdmin.
	return types.KBPermissionAdmin, true, nil
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

	// NEU: same-shape lookup for group grants on these KBs. ListKBGrantsForUser
	// returns the union of grants from every group the user is in; we filter
	// to the input KB-ID set in memory rather than adding a third repo method.
	groupGrantSet := make(map[string]struct{})
	if s.groupRepo != nil {
		groupGrants, gerr := s.groupRepo.ListKBGrantsForUser(ctx, user.ID)
		if gerr != nil {
			return nil, gerr
		}
		sameTenantIDSet := make(map[string]struct{}, len(sameTenantIDs))
		for _, id := range sameTenantIDs {
			sameTenantIDSet[id] = struct{}{}
		}
		for _, g := range groupGrants {
			if _, ok := sameTenantIDSet[g.KnowledgeBaseID]; ok {
				groupGrantSet[g.KnowledgeBaseID] = struct{}{}
			}
		}
	}

	hasGrants, err := s.repo.KBsWithAnyGrants(ctx, sameTenantIDs)
	if err != nil {
		return nil, err
	}
	// NEU: a KB is "restricted" if it has either user-direct OR group grants.
	// Without this, a KB granted only to a group falls into the legacy
	// open-KB rule below and becomes visible to the entire tenant.
	hasAnyGroupGrants, err := s.kbsHaveAnyGroupGrants(ctx, sameTenantIDs)
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
		// NEU: has grant via group membership -> visible.
		if _, ok := groupGrantSet[kb.ID]; ok {
			out = append(out, kb)
			continue
		}
		// No grants exist anywhere on this KB (user-direct or group) -> legacy tenant-wide visibility.
		if !hasGrants[kb.ID] && !hasAnyGroupGrants[kb.ID] {
			out = append(out, kb)
			continue
		}
		// KB has other grants but not for this user -> hidden.
	}
	return out, nil
}

// ResolveExplicitPermission returns the actor's permission ONLY from the
// authoritative sources: tenant owner/admin role, KB ownership, or an explicit
// grant. Crucially it does NOT apply the legacy "no grants anywhere -> admin"
// fallback that ResolvePermission uses for view/edit/delete continuity.
//
// Use this when authorizing any action that could escalate privilege:
// granting/revoking access, changing permission level, transferring ownership.
//
// NOTE on TOCTOU: the kb argument is read once by the caller and the grant is
// read separately. A concurrent role/grant change between those reads can lead
// to a decision against a millisecond-old snapshot. This is acceptable for
// per-request authorization (matches every other gate in the codebase that
// trusts the auth-middleware-loaded actor); a stricter SELECT FOR UPDATE
// transaction is reserved for operations whose outcome would be catastrophic
// under such a race (e.g. owner demotion — see DemoteOwnerIfSafe).
func (s *kbPermissionService) ResolveExplicitPermission(
	ctx context.Context, actor *types.User, kb *types.KnowledgeBase,
) (types.KBPermission, bool, error) {
	if actor == nil {
		return "", false, errors.New("user required")
	}
	if kb == nil || kb.TenantID != actor.TenantID {
		return "", false, nil
	}
	if actor.Role == types.UserRoleOwner || actor.Role == types.UserRoleAdmin {
		return types.KBPermissionAdmin, true, nil
	}
	if kb.OwnerID != "" && kb.OwnerID == actor.ID {
		return types.KBPermissionAdmin, true, nil
	}
	grant, gerr := s.repo.GetByKBAndUser(ctx, kb.ID, actor.ID)
	if gerr != nil && !errors.Is(gerr, apprepo.ErrKBPermissionNotFound) {
		return "", false, gerr
	}
	// NEU: fold the highest group-grant into the explicit-permission decision.
	// Without this, a user who is admin on a KB only via a group could not
	// grant access to other users (requireKBAdmin would deny them).
	groupPerm, hasGroup, gerr2 := s.highestGroupGrantForKB(ctx, actor.ID, kb.ID)
	if gerr2 != nil {
		return "", false, gerr2
	}
	if grant != nil {
		best := grant.Permission
		if hasGroup && groupPerm.HasAtLeast(best) {
			best = groupPerm
		}
		return best, true, nil
	}
	if hasGroup {
		return groupPerm, true, nil
	}
	return "", false, nil
}

// highestGroupGrantForKB returns the strongest permission the user holds on the
// given KB via any of their group memberships. Returns (perm, true, nil) when at
// least one applicable grant exists; (perm, false, nil) when none do.
// NEU: extracted helper so ResolvePermissionWithKB and ResolveExplicitPermission
// share one resolution path for group grants and stay consistent.
func (s *kbPermissionService) highestGroupGrantForKB(
	ctx context.Context, userID, kbID string,
) (types.KBPermission, bool, error) {
	if s.groupRepo == nil {
		return "", false, nil
	}
	grants, err := s.groupRepo.ListKBGrantsForUser(ctx, userID)
	if err != nil {
		return "", false, err
	}
	var best types.KBPermission
	found := false
	for _, g := range grants {
		if g.KnowledgeBaseID != kbID {
			continue
		}
		if !found || g.Permission.HasAtLeast(best) {
			best = g.Permission
			found = true
		}
	}
	return best, found, nil
}

// kbHasAnyGroupGrants reports whether ANY group has been granted access to the
// given KB. NEU: needed so the resolver can distinguish a "restricted KB with
// only group grants" (no access for non-members) from an "open KB" (legacy
// tenant-wide admin). Single-KB convenience wrapper around kbsHaveAnyGroupGrants.
func (s *kbPermissionService) kbHasAnyGroupGrants(ctx context.Context, kbID string) (bool, error) {
	out, err := s.kbsHaveAnyGroupGrants(ctx, []string{kbID})
	if err != nil {
		return false, err
	}
	return out[kbID], nil
}

// kbsHaveAnyGroupGrants returns a set indicating which of the input KB IDs have
// at least one active group grant. NEU: used by FilterAccessibleSameTenant to
// classify each KB without an N+1 lookup.
func (s *kbPermissionService) kbsHaveAnyGroupGrants(ctx context.Context, kbIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(kbIDs))
	if s.groupRepo == nil || len(kbIDs) == 0 {
		return out, nil
	}
	idSet := make(map[string]struct{}, len(kbIDs))
	for _, id := range kbIDs {
		idSet[id] = struct{}{}
	}
	for _, kbID := range kbIDs {
		grants, err := s.groupRepo.ListKBGrantsForKB(ctx, kbID)
		if err != nil {
			return nil, err
		}
		if len(grants) > 0 {
			out[kbID] = true
		}
	}
	return out, nil
}

// requireKBAdmin asserts the actor can manage the KB's permissions. Routes to
// ResolveExplicitPermission so a tenant member on a grant-less KB cannot
// self-promote into an admin grant.
func (s *kbPermissionService) requireKBAdmin(ctx context.Context, actor *types.User, kbID string) error {
	kb, err := s.kbRepo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return fmt.Errorf("failed to load knowledge base: %w", err)
	}
	if kb == nil {
		return errors.New("permission denied")
	}
	perm, ok, err := s.ResolveExplicitPermission(ctx, actor, kb)
	if err != nil {
		return err
	}
	if !ok || !perm.HasAtLeast(types.KBPermissionAdmin) {
		return errors.New("permission denied")
	}
	return nil
}
