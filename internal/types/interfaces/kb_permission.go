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
}

// KBPermissionRepository is the storage interface for kb_user_permissions.
type KBPermissionRepository interface {
	Create(ctx context.Context, p *types.KBUserPermission) error
	GetByID(ctx context.Context, id string) (*types.KBUserPermission, error)
	GetByKBAndUser(ctx context.Context, kbID, userID string) (*types.KBUserPermission, error)
	ListByKB(ctx context.Context, kbID string) ([]*types.KBUserPermission, error)
	ListByUser(ctx context.Context, userID string) ([]*types.KBUserPermission, error)
	Update(ctx context.Context, p *types.KBUserPermission) error
	Delete(ctx context.Context, id string) error
}
