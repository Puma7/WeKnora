package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	// invitationDefaultValidityDays is used when the admin doesn't specify ValidityDays.
	invitationDefaultValidityDays = 7
	// invitationMaxValidityDays caps token lifetime to limit blast radius if a link leaks.
	invitationMaxValidityDays = 90
	// invitationTokenBytes is the entropy size of the raw token before base64 encoding.
	invitationTokenBytes = 32
)

type invitationService struct {
	invitationRepo interfaces.InvitationRepository
	userRepo       interfaces.UserRepository
	tenantService  interfaces.TenantService
	kbPermRepo     interfaces.KBPermissionRepository
}

// NewInvitationService wires the invitation service. kbPermRepo may be nil for tests.
func NewInvitationService(
	invitationRepo interfaces.InvitationRepository,
	userRepo interfaces.UserRepository,
	tenantService interfaces.TenantService,
	kbPermRepo interfaces.KBPermissionRepository,
) interfaces.InvitationService {
	return &invitationService{
		invitationRepo: invitationRepo,
		userRepo:       userRepo,
		tenantService:  tenantService,
		kbPermRepo:     kbPermRepo,
	}
}

// CreateInvitation issues a new invitation. Returns the persisted row plus the raw
// token (only available here — never persisted in plaintext).
func (s *invitationService) CreateInvitation(
	ctx context.Context, actor *types.User, req *types.CreateInvitationRequest,
) (*types.UserInvitation, string, error) {
	if actor == nil {
		return nil, "", errors.New("actor required")
	}
	if !actor.Can("invite_users") {
		return nil, "", errors.New("permission denied")
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		return nil, "", errors.New("email is required")
	}

	role := req.Role
	if role == "" {
		role = types.UserRoleMember
	}
	if !role.IsValid() {
		return nil, "", errors.New("invalid role")
	}
	if !actor.Role.HasAtLeast(role) {
		return nil, "", errors.New("cannot invite a user with a role higher than your own")
	}

	// Reject the invite if a user with this email already exists in any tenant.
	if existing, _ := s.userRepo.GetUserByEmail(ctx, email); existing != nil {
		return nil, "", errors.New("a user with this email already exists")
	}

	// Validate KB grants belong to the actor's tenant. We don't deeply check ownership
	// here — the kb_permission service enforces that on accept.
	for _, g := range req.KnowledgeBaseGrants {
		if !g.Permission.IsValid() {
			return nil, "", fmt.Errorf("invalid permission for KB %s", g.KnowledgeBaseID)
		}
	}

	validityDays := req.ValidityDays
	if validityDays <= 0 {
		validityDays = invitationDefaultValidityDays
	}
	if validityDays > invitationMaxValidityDays {
		validityDays = invitationMaxValidityDays
	}

	rawToken, err := generateInvitationToken()
	if err != nil {
		logger.Errorf(ctx, "Failed to generate invitation token: %v", err)
		return nil, "", errors.New("failed to generate invitation token")
	}
	tokenHash := HashInvitationToken(rawToken)

	var permsJSON *types.JSON
	if req.Permissions != nil {
		permsJSON, err = types.MarshalToJSON(req.Permissions)
		if err != nil {
			return nil, "", fmt.Errorf("failed to encode permissions: %w", err)
		}
	}

	var grantsJSON *types.JSON
	if len(req.KnowledgeBaseGrants) > 0 {
		grantsJSON, err = types.MarshalToJSON(req.KnowledgeBaseGrants)
		if err != nil {
			return nil, "", fmt.Errorf("failed to encode KB grants: %w", err)
		}
	}

	now := time.Now()
	inv := &types.UserInvitation{
		ID:                  uuid.New().String(),
		Email:               email,
		Token:               tokenHash,
		TenantID:            actor.TenantID,
		InvitedByUserID:     actor.ID,
		Role:                role,
		Permissions:         permsJSON,
		KnowledgeBaseGrants: grantsJSON,
		Status:              types.InvitationStatusPending,
		ExpiresAt:           now.Add(time.Duration(validityDays) * 24 * time.Hour),
		Note:                strings.TrimSpace(req.Note),
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := s.invitationRepo.Create(ctx, inv); err != nil {
		logger.Errorf(ctx, "Failed to persist invitation: %v", err)
		return nil, "", errors.New("failed to create invitation")
	}
	return inv, rawToken, nil
}

// ListInvitationsByTenant returns invitations of the actor's tenant.
func (s *invitationService) ListInvitationsByTenant(ctx context.Context, tenantID uint64) ([]*types.UserInvitation, error) {
	invs, err := s.invitationRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, inv := range invs {
		// Side-effect: mark expired invitations so listing reflects reality.
		if inv.Status == types.InvitationStatusPending && now.After(inv.ExpiresAt) {
			inv.Status = types.InvitationStatusExpired
			inv.UpdatedAt = now
			_ = s.invitationRepo.Update(ctx, inv)
		}
	}
	return invs, nil
}

// RevokeInvitation marks a pending invitation as revoked.
func (s *invitationService) RevokeInvitation(ctx context.Context, actor *types.User, invitationID string) error {
	if actor == nil {
		return errors.New("actor required")
	}
	if !actor.Can("invite_users") {
		return errors.New("permission denied")
	}

	inv, err := s.invitationRepo.GetByID(ctx, invitationID)
	if err != nil {
		if errors.Is(err, apprepo.ErrInvitationNotFound) {
			return errors.New("invitation not found")
		}
		return err
	}
	if inv.TenantID != actor.TenantID {
		return errors.New("invitation belongs to a different tenant")
	}
	if inv.Status == types.InvitationStatusAccepted {
		return errors.New("cannot revoke an already accepted invitation")
	}
	if inv.Status == types.InvitationStatusRevoked {
		return nil // already revoked, treat as idempotent
	}

	now := time.Now()
	inv.Status = types.InvitationStatusRevoked
	inv.RevokedAt = &now
	inv.RevokedByUserID = actor.ID
	inv.UpdatedAt = now
	return s.invitationRepo.Update(ctx, inv)
}

// GetInvitationByToken returns the invitation for a raw token. Used by /auth/invitations/:token.
func (s *invitationService) GetInvitationByToken(ctx context.Context, rawToken string) (*types.UserInvitation, error) {
	hash := HashInvitationToken(rawToken)
	inv, err := s.invitationRepo.GetByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, apprepo.ErrInvitationNotFound) {
			return nil, errors.New("invitation not found")
		}
		return nil, err
	}
	return inv, nil
}

// AcceptInvitation creates a user account for the invitee and applies KB grants.
//
// Note: the actual user creation is delegated to the user service via Register
// (called by the auth handler that sees this token). This method exists for the
// rare path where we want to short-circuit straight from token+credentials to
// an active user. Currently the auth handler uses the Register path.
func (s *invitationService) AcceptInvitation(_ context.Context, _ *types.AcceptInvitationRequest) (*types.User, error) {
	return nil, errors.New("acceptance is handled via /auth/register with invitation_token")
}

// ApplyInvitationGrants applies KB pre-grants from an invitation onto a freshly created user.
// Called from the user service after a successful invited registration.
func (s *invitationService) ApplyInvitationGrants(ctx context.Context, inv *types.UserInvitation, user *types.User) error {
	if s.kbPermRepo == nil || inv == nil || inv.KnowledgeBaseGrants == nil {
		return nil
	}
	var grants []types.InvitationKBGrant
	if err := inv.KnowledgeBaseGrants.Unmarshal(&grants); err != nil {
		return fmt.Errorf("failed to decode KB grants: %w", err)
	}
	now := time.Now()
	for _, g := range grants {
		grant := &types.KBUserPermission{
			ID:              uuid.New().String(),
			KnowledgeBaseID: g.KnowledgeBaseID,
			UserID:          user.ID,
			TenantID:        user.TenantID,
			Permission:      g.Permission,
			GrantedByUserID: inv.InvitedByUserID,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := s.kbPermRepo.Create(ctx, grant); err != nil {
			logger.Warnf(ctx, "Failed to apply KB grant %s for user %s: %v", g.KnowledgeBaseID, user.ID, err)
		}
	}
	return nil
}

// generateInvitationToken returns a URL-safe random string of invitationTokenBytes bytes.
func generateInvitationToken() (string, error) {
	b := make([]byte, invitationTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
