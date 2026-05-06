package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// ErrKBPermissionNotFound is returned when no row matches the lookup.
var ErrKBPermissionNotFound = errors.New("kb permission not found")

type kbPermissionRepository struct {
	db *gorm.DB
}

// NewKBPermissionRepository constructs the GORM-backed kb_user_permissions repository.
func NewKBPermissionRepository(db *gorm.DB) interfaces.KBPermissionRepository {
	return &kbPermissionRepository{db: db}
}

func (r *kbPermissionRepository) Create(ctx context.Context, p *types.KBUserPermission) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *kbPermissionRepository) GetByID(ctx context.Context, id string) (*types.KBUserPermission, error) {
	var p types.KBUserPermission
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKBPermissionNotFound
		}
		return nil, err
	}
	return &p, nil
}

// GetByKBAndUser returns the active grant for a (kb, user) pair, if any.
// Returns ErrKBPermissionNotFound when missing.
func (r *kbPermissionRepository) GetByKBAndUser(ctx context.Context, kbID, userID string) (*types.KBUserPermission, error) {
	var p types.KBUserPermission
	err := r.db.WithContext(ctx).
		Where("knowledge_base_id = ? AND user_id = ?", kbID, userID).
		First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKBPermissionNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (r *kbPermissionRepository) ListByKB(ctx context.Context, kbID string) ([]*types.KBUserPermission, error) {
	var perms []*types.KBUserPermission
	if err := r.db.WithContext(ctx).
		Where("knowledge_base_id = ?", kbID).
		Order("created_at ASC").
		Find(&perms).Error; err != nil {
		return nil, err
	}
	return perms, nil
}

func (r *kbPermissionRepository) ListByUser(ctx context.Context, userID string) ([]*types.KBUserPermission, error) {
	var perms []*types.KBUserPermission
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&perms).Error; err != nil {
		return nil, err
	}
	return perms, nil
}

func (r *kbPermissionRepository) Update(ctx context.Context, p *types.KBUserPermission) error {
	return r.db.WithContext(ctx).Save(p).Error
}

// Delete soft-deletes a grant.
func (r *kbPermissionRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.KBUserPermission{}).Error
}
