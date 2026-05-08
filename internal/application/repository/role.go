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

// ErrRoleNotFound is returned when a role lookup misses or the row is soft-deleted.
var ErrRoleNotFound = errors.New("role not found")

// roleRepository implements interfaces.RoleRepository against the roles +
// role_permissions tables.
type roleRepository struct {
	db *gorm.DB
}

// NewRoleRepository constructs the repository.
func NewRoleRepository(db *gorm.DB) interfaces.RoleRepository {
	return &roleRepository{db: db}
}

func (r *roleRepository) CreateRole(ctx context.Context, role *types.Role) error {
	return r.db.WithContext(ctx).Create(role).Error
}

func (r *roleRepository) UpdateRole(ctx context.Context, role *types.Role) error {
	role.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Save(role).Error
}

// DeleteRole soft-deletes the role plus its permission rows in one transaction.
// The permission rows are wiped so a future role with the same key starts clean.
func (r *roleRepository) DeleteRole(ctx context.Context, roleID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&types.RolePermission{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", roleID).Delete(&types.Role{}).Error
	})
}

func (r *roleRepository) GetRoleByID(ctx context.Context, roleID string) (*types.Role, error) {
	var role types.Role
	if err := r.db.WithContext(ctx).Where("id = ?", roleID).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRoleNotFound
		}
		return nil, err
	}
	return &role, nil
}

// GetRoleByKey returns the tenant-scoped row first, falling back to the global
// system row. Returns (nil, nil) when nothing matches so the caller can decide
// whether to seed a missing role.
func (r *roleRepository) GetRoleByKey(ctx context.Context, tenantID uint64, key string) (*types.Role, error) {
	key = types.NormalizeRoleKey(key)
	var role types.Role
	err := r.db.WithContext(ctx).
		Where("key = ? AND tenant_id = ?", key, tenantID).
		First(&role).Error
	if err == nil {
		return &role, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// Fall back to global system role (tenant_id = 0).
	err = r.db.WithContext(ctx).
		Where("key = ? AND tenant_id = ?", key, uint64(0)).
		First(&role).Error
	if err == nil {
		return &role, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return nil, err
}

// ListRolesForTenant returns every role visible to the tenant: system + own.
// System roles always come first so the matrix UI can render them in a
// stable order. Within each scope, alphabetical by key for predictable diffs.
func (r *roleRepository) ListRolesForTenant(ctx context.Context, tenantID uint64) ([]*types.Role, error) {
	var roles []*types.Role
	err := r.db.WithContext(ctx).
		Where("tenant_id = 0 OR tenant_id = ?", tenantID).
		Order("is_system DESC, tenant_id ASC, key ASC").
		Find(&roles).Error
	if err != nil {
		return nil, err
	}
	return roles, nil
}

func (r *roleRepository) CountUsersWithRole(ctx context.Context, tenantID uint64, roleKey string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&types.User{}).
		Where("tenant_id = ? AND role = ?", tenantID, roleKey).
		Count(&n).Error
	return n, err
}

func (r *roleRepository) CountGroupsWithRole(ctx context.Context, roleID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&types.UserGroup{}).
		Where("role_id = ?", roleID).
		Count(&n).Error
	return n, err
}

// LoadPermissions populates the Permissions map on each role in one query so
// the admin UI can render a multi-row matrix without N+1.
func (r *roleRepository) LoadPermissions(ctx context.Context, roles []*types.Role) error {
	if len(roles) == 0 {
		return nil
	}
	ids := make([]string, 0, len(roles))
	indexByID := make(map[string]*types.Role, len(roles))
	for _, role := range roles {
		ids = append(ids, role.ID)
		indexByID[role.ID] = role
		if role.Permissions == nil {
			role.Permissions = map[string]bool{}
		}
	}
	var rows []types.RolePermission
	if err := r.db.WithContext(ctx).Where("role_id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if role, ok := indexByID[row.RoleID]; ok {
			role.Permissions[row.PermissionKey] = row.Allowed
		}
	}
	return nil
}

func (r *roleRepository) GetPermissionsForRole(ctx context.Context, roleID string) (map[string]bool, error) {
	var rows []types.RolePermission
	if err := r.db.WithContext(ctx).Where("role_id = ?", roleID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.PermissionKey] = row.Allowed
	}
	return out, nil
}

// GetPermissionsForRoles is the batch variant. NEU: returns one matrix row
// per role in a single query so the resolver can hydrate a user with N group
// memberships without N separate roundtrips.
func (r *roleRepository) GetPermissionsForRoles(ctx context.Context, roleIDs []string) (map[string]map[string]bool, error) {
	out := make(map[string]map[string]bool, len(roleIDs))
	if len(roleIDs) == 0 {
		return out, nil
	}
	var rows []types.RolePermission
	if err := r.db.WithContext(ctx).Where("role_id IN ?", roleIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		bucket, ok := out[row.RoleID]
		if !ok {
			bucket = make(map[string]bool)
			out[row.RoleID] = bucket
		}
		bucket[row.PermissionKey] = row.Allowed
	}
	return out, nil
}

// SetPermission upserts the (role, key) row. ON CONFLICT DO UPDATE handles
// both the "first time setting" and "flipping an existing value" paths.
func (r *roleRepository) SetPermission(ctx context.Context, roleID, key string, allowed bool) error {
	now := time.Now()
	row := types.RolePermission{
		RoleID:        roleID,
		PermissionKey: key,
		Allowed:       allowed,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "role_id"}, {Name: "permission_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"allowed", "updated_at"}),
		}).
		Create(&row).Error
}

func (r *roleRepository) ClearPermission(ctx context.Context, roleID, key string) error {
	return r.db.WithContext(ctx).
		Unscoped(). // Delete the row entirely; soft-delete would leave an "absent default"
		Where("role_id = ? AND permission_key = ?", roleID, key).
		Delete(&types.RolePermission{}).Error
}
