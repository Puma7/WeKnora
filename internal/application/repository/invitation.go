package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// ErrInvitationNotFound is returned when no invitation matches the lookup key.
var ErrInvitationNotFound = errors.New("invitation not found")

type invitationRepository struct {
	db *gorm.DB
}

// NewInvitationRepository constructs the GORM-backed invitation repository.
func NewInvitationRepository(db *gorm.DB) interfaces.InvitationRepository {
	return &invitationRepository{db: db}
}

// Create persists a brand-new invitation row. Token is expected to be already hashed by the caller.
func (r *invitationRepository) Create(ctx context.Context, inv *types.UserInvitation) error {
	return r.db.WithContext(ctx).Create(inv).Error
}

// GetByID returns a single invitation, including soft-deleted ones excluded by GORM defaults.
func (r *invitationRepository) GetByID(ctx context.Context, id string) (*types.UserInvitation, error) {
	var inv types.UserInvitation
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvitationNotFound
		}
		return nil, err
	}
	return &inv, nil
}

// GetByTokenHash looks up an invitation by the hashed token value. The caller is
// responsible for hashing the raw token before calling this method.
func (r *invitationRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*types.UserInvitation, error) {
	var inv types.UserInvitation
	if err := r.db.WithContext(ctx).Where("token = ?", tokenHash).First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvitationNotFound
		}
		return nil, err
	}
	return &inv, nil
}

// ListByTenant returns all invitations of a tenant ordered by newest first.
func (r *invitationRepository) ListByTenant(ctx context.Context, tenantID uint64) ([]*types.UserInvitation, error) {
	var invs []*types.UserInvitation
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&invs).Error; err != nil {
		return nil, err
	}
	return invs, nil
}

// ListPendingByEmail returns all pending invitations for an email (case-insensitive).
// Used by the registration flow to honor any matching invitation when the user signs up
// without explicitly redeeming a token (e.g. via OIDC or in whitelist mode).
func (r *invitationRepository) ListPendingByEmail(ctx context.Context, email string) ([]*types.UserInvitation, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return []*types.UserInvitation{}, nil
	}
	var invs []*types.UserInvitation
	if err := r.db.WithContext(ctx).
		Where("LOWER(email) = ? AND status = ?", email, types.InvitationStatusPending).
		Order("created_at DESC").
		Find(&invs).Error; err != nil {
		return nil, err
	}
	return invs, nil
}

// Update persists changes to an invitation row.
func (r *invitationRepository) Update(ctx context.Context, inv *types.UserInvitation) error {
	return r.db.WithContext(ctx).Save(inv).Error
}
