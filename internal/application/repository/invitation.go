package repository

import (
	"context"
	"errors"
	"strings"
	"time"

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

// NEU: ConsumeIfPending runs a single conditional UPDATE that flips the
// invitation from pending to accepted only when it's still pending AND
// unexpired AND not soft-deleted. The whole check happens at row-write time
// inside the same statement, so a parallel revoke that lost the race cannot
// be silently overwritten by a non-conditional Save afterwards.
func (r *invitationRepository) ConsumeIfPending(ctx context.Context, invitationID, acceptedUserID string) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).
		Model(&types.UserInvitation{}).
		Where("id = ? AND status = ? AND expires_at > ?",
			invitationID, types.InvitationStatusPending, now).
		Updates(map[string]interface{}{
			"status":           types.InvitationStatusAccepted,
			"accepted_user_id": acceptedUserID,
			"accepted_at":      now,
			"updated_at":       now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
