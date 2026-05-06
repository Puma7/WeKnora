package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// InvitationService is the business-layer entrypoint for invitation lifecycle.
type InvitationService interface {
	// CreateInvitation issues a new invitation and returns it together with the raw token.
	// The token is only returned here so the admin UI can build the magic link;
	// it must not be stored elsewhere.
	CreateInvitation(ctx context.Context, actor *types.User, req *types.CreateInvitationRequest) (*types.UserInvitation, string, error)
	// ListInvitationsByTenant returns all (non-deleted) invitations of a tenant.
	ListInvitationsByTenant(ctx context.Context, tenantID uint64) ([]*types.UserInvitation, error)
	// RevokeInvitation marks an invitation as revoked. Already-accepted ones cannot be revoked.
	RevokeInvitation(ctx context.Context, actor *types.User, invitationID string) error
	// GetInvitationByToken returns the invitation for the given token (used for the public preview).
	GetInvitationByToken(ctx context.Context, token string) (*types.UserInvitation, error)
	// AcceptInvitation creates a user account for the invitee; returns the new user.
	AcceptInvitation(ctx context.Context, req *types.AcceptInvitationRequest) (*types.User, error)
}

// InvitationRepository is the storage interface for invitations.
type InvitationRepository interface {
	Create(ctx context.Context, inv *types.UserInvitation) error
	GetByID(ctx context.Context, id string) (*types.UserInvitation, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (*types.UserInvitation, error)
	ListByTenant(ctx context.Context, tenantID uint64) ([]*types.UserInvitation, error)
	ListPendingByEmail(ctx context.Context, email string) ([]*types.UserInvitation, error)
	Update(ctx context.Context, inv *types.UserInvitation) error
	// NEU: ConsumeIfPending atomically transitions an invitation from pending
	// to accepted IFF it's still pending and unexpired. Returns (true, nil)
	// when the consume succeeded, (false, nil) if the invitation was already
	// revoked/expired/accepted, or (_, err) on a database error. Callers must
	// invoke this BEFORE creating the user to close the revoke-vs-accept race.
	ConsumeIfPending(ctx context.Context, invitationID, acceptedUserID string) (bool, error)
}
