package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBPermissionService manages per-user knowledge base grants.
type KBPermissionService interface {
	// Grant adds a new permission for a user on a KB (or returns existing one if active).
	Grant(ctx context.Context, actor *types.User, kbID, userID string, permission types.KBPermission) (*types.KBUserPermission, error)
	// UpdatePermission updates the permission level of an existing grant.
	UpdatePermission(ctx context.Context, actor *types.User, grantID string, permission types.KBPermission) (*types.KBUserPermission, error)
	// Revoke removes a grant (soft delete).
	Revoke(ctx context.Context, actor *types.User, grantID string) error
	// ListByKB returns all active grants for a knowledge base.
	ListByKB(ctx context.Context, actor *types.User, kbID string) ([]types.KBUserPermissionResponse, error)
	// ResolvePermission returns the highest permission a user has on a KB,
	// considering: ownership, direct grants, and tenant membership.
	// Returns ("", false) when the user has no access at all.
	ResolvePermission(ctx context.Context, user *types.User, kbID string) (types.KBPermission, bool, error)
	// FilterAccessibleSameTenant returns the subset of input KBs the user is
	// allowed to view under the same-tenant rules. Cross-tenant KBs are returned
	// unchanged so the caller's existing org-share logic stays authoritative.
	FilterAccessibleSameTenant(ctx context.Context, user *types.User, kbs []*types.KnowledgeBase) ([]*types.KnowledgeBase, error)
}

// KBPermissionRepository is the storage interface for kb_user_permissions.
type KBPermissionRepository interface {
	Create(ctx context.Context, p *types.KBUserPermission) error
	GetByID(ctx context.Context, id string) (*types.KBUserPermission, error)
	GetByKBAndUser(ctx context.Context, kbID, userID string) (*types.KBUserPermission, error)
	ListByKB(ctx context.Context, kbID string) ([]*types.KBUserPermission, error)
	ListByUser(ctx context.Context, userID string) ([]*types.KBUserPermission, error)
	// ListGrantsForUserInKBs returns the user's active grants restricted to a KB ID set.
	// Used to bulk-resolve permission decisions for a list of KBs.
	ListGrantsForUserInKBs(ctx context.Context, userID string, kbIDs []string) ([]*types.KBUserPermission, error)
	// KBsWithAnyGrants returns the subset of the input KB IDs that have at least one
	// active grant. Used to apply the "any grant -> KB becomes restricted" rule.
	KBsWithAnyGrants(ctx context.Context, kbIDs []string) (map[string]bool, error)
	Update(ctx context.Context, p *types.KBUserPermission) error
	Delete(ctx context.Context, id string) error
}
