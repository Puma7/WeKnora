package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ErrGroupNotFound is returned when a group lookup misses or is soft-deleted.
var ErrGroupNotFound = errors.New("group not found")

// groupRepository implements interfaces.GroupRepository.
type groupRepository struct {
	db *gorm.DB
}

// NewGroupRepository constructs the repository.
func NewGroupRepository(db *gorm.DB) interfaces.GroupRepository {
	return &groupRepository{db: db}
}

func (r *groupRepository) CreateGroup(ctx context.Context, g *types.UserGroup) error {
	return r.db.WithContext(ctx).Create(g).Error
}

func (r *groupRepository) UpdateGroup(ctx context.Context, g *types.UserGroup) error {
	g.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Save(g).Error
}

// DeleteGroup soft-deletes the group, then removes its memberships and KB
// grants in the same transaction. Hard-deleting members/grants matches the
// "group is gone, so dependent rows have no anchor" semantics — we never want
// orphaned membership rows resurrected if a group with the same name is
// re-created later.
func (r *groupRepository) DeleteGroup(ctx context.Context, groupID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", groupID).Delete(&types.UserGroupMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", groupID).Delete(&types.GroupKBPermission{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", groupID).Delete(&types.UserGroup{}).Error
	})
}

func (r *groupRepository) GetGroupByID(ctx context.Context, groupID string) (*types.UserGroup, error) {
	var g types.UserGroup
	if err := r.db.WithContext(ctx).Where("id = ?", groupID).First(&g).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, err
	}
	return &g, nil
}

func (r *groupRepository) ListGroupsForTenant(ctx context.Context, tenantID uint64) ([]*types.UserGroup, error) {
	var groups []*types.UserGroup
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("name ASC").
		Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

// ListGroupsForUser is the hot path used by the resolver on every request.
// We join via the membership table directly rather than loading members
// separately to keep this single-query.
func (r *groupRepository) ListGroupsForUser(ctx context.Context, userID string) ([]*types.UserGroup, error) {
	var groups []*types.UserGroup
	err := r.db.WithContext(ctx).
		Joins("JOIN user_group_members m ON m.group_id = user_groups.id").
		Where("m.user_id = ? AND user_groups.deleted_at IS NULL", userID).
		Find(&groups).Error
	if err != nil {
		return nil, err
	}
	return groups, nil
}

func (r *groupRepository) AddMember(ctx context.Context, groupID, userID string) error {
	row := &types.UserGroupMember{
		GroupID:   groupID,
		UserID:    userID,
		CreatedAt: time.Now(),
	}
	// ON CONFLICT DO NOTHING so re-adding the same user is idempotent (the
	// admin UI's "add" button stays usable even if the user is already in the
	// group due to a stale render).
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(row).Error
}

func (r *groupRepository) RemoveMember(ctx context.Context, groupID, userID string) error {
	return r.db.WithContext(ctx).
		Where("group_id = ? AND user_id = ?", groupID, userID).
		Delete(&types.UserGroupMember{}).Error
}

func (r *groupRepository) ListMembers(ctx context.Context, groupID string) ([]string, error) {
	var rows []types.UserGroupMember
	if err := r.db.WithContext(ctx).Where("group_id = ?", groupID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.UserID)
	}
	return out, nil
}

func (r *groupRepository) CountMembers(ctx context.Context, groupID string) (int, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&types.UserGroupMember{}).
		Where("group_id = ?", groupID).
		Count(&n).Error
	return int(n), err
}

func (r *groupRepository) CreateKBGrant(ctx context.Context, g *types.GroupKBPermission) error {
	return r.db.WithContext(ctx).Create(g).Error
}

func (r *groupRepository) RemoveKBGrant(ctx context.Context, grantID string) error {
	return r.db.WithContext(ctx).Where("id = ?", grantID).Delete(&types.GroupKBPermission{}).Error
}

func (r *groupRepository) ListKBGrantsForKB(ctx context.Context, kbID string) ([]*types.GroupKBPermission, error) {
	var grants []*types.GroupKBPermission
	if err := r.db.WithContext(ctx).
		Where("knowledge_base_id = ?", kbID).
		Find(&grants).Error; err != nil {
		return nil, err
	}
	return grants, nil
}

// ListKBGrantsForUser pulls every KB grant from every group the user is in,
// in a single join. The KB permission service merges this with direct user
// grants when resolving access.
func (r *groupRepository) ListKBGrantsForUser(ctx context.Context, userID string) ([]*types.GroupKBPermission, error) {
	var grants []*types.GroupKBPermission
	err := r.db.WithContext(ctx).
		Joins("JOIN user_group_members m ON m.group_id = kb_group_permissions.group_id").
		Where("m.user_id = ? AND kb_group_permissions.deleted_at IS NULL", userID).
		Find(&grants).Error
	if err != nil {
		return nil, err
	}
	return grants, nil
}
