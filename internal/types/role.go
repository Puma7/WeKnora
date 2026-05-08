package types

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// Permission flag keys. Defined in one place so handlers, the UI catalog, and
// the resolver agree on spelling. Adding a flag is a 3-step change: append a
// constant here, register it in DefaultPermissionCatalog (below), and ship
// a migration row for each system role with the desired default.
const (
	PermissionChat        = "chat"
	PermissionSearch      = "search"
	PermissionCreateKB    = "create_kb"
	PermissionInviteUsers = "invite_users"
	PermissionManageUsers = "manage_users"
	PermissionManageKBs   = "manage_kbs"
)

// PermissionCatalogEntry describes a feature flag for the admin UI.
// Categories let the matrix editor group flags into sections.
type PermissionCatalogEntry struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Category string `json:"category"`
	// Description is optional helper text rendered under the flag in the UI.
	Description string `json:"description,omitempty"`
}

// DefaultPermissionCatalog is the registry of all flags the resolver knows
// about. Order is the rendering order in the admin matrix editor. Labels are
// English defaults — the frontend resolves them via i18n keys
// `permissions.flags.<key>.label` and only falls back to these if a translation
// is missing, so non-English locales aren't required to look at this list.
var DefaultPermissionCatalog = []PermissionCatalogEntry{
	{Key: PermissionChat, Label: "Chat", Category: "consume"},
	{Key: PermissionSearch, Label: "Search", Category: "consume"},
	{Key: PermissionCreateKB, Label: "Create knowledge base", Category: "create"},
	{Key: PermissionManageKBs, Label: "Manage knowledge bases", Category: "manage"},
	{Key: PermissionInviteUsers, Label: "Invite users", Category: "manage"},
	{Key: PermissionManageUsers, Label: "Manage users & roles", Category: "manage"},
}

// IsKnownPermissionKey reports whether the supplied flag is in the catalog.
// Used by the admin handlers to reject typos before persisting.
func IsKnownPermissionKey(key string) bool {
	for _, e := range DefaultPermissionCatalog {
		if e.Key == key {
			return true
		}
	}
	return false
}

// SystemRoleKey is reserved for the four built-in roles seeded by migration.
// Keeping these as constants prevents callers from accidentally introducing a
// new "system" role through CreateRole — those go through ValidateNonSystemKey.
const (
	SystemRoleKeyOwner  = "owner"
	SystemRoleKeyAdmin  = "admin"
	SystemRoleKeyMember = "member"
	SystemRoleKeyViewer = "viewer"
)

// IsSystemRoleKey returns true for the four built-in keys.
func IsSystemRoleKey(key string) bool {
	switch key {
	case SystemRoleKeyOwner, SystemRoleKeyAdmin, SystemRoleKeyMember, SystemRoleKeyViewer:
		return true
	}
	return false
}

// Role represents a tenant-scoped or global (system) role.
//
// SCOPE RULES:
//   - IsSystem = true && TenantID = 0  -> visible to every tenant; key collision
//     is forbidden (one global "owner" only)
//   - IsSystem = false && TenantID > 0 -> tenant-private custom role
//
// The same Key may exist as a system role AND as a tenant-private override
// (allowing a tenant to redefine "admin" without destroying the global one),
// but resolver step 3 always prefers the tenant-scoped row when present.
type Role struct {
	ID          string         `json:"id"          gorm:"type:varchar(36);primaryKey"`
	TenantID    uint64         `json:"tenant_id"   gorm:"index;default:0"`
	Key         string         `json:"key"         gorm:"type:varchar(64);not null;index"`
	Label       string         `json:"label"       gorm:"type:varchar(128);not null"`
	Description string         `json:"description" gorm:"type:text"`
	IsSystem    bool           `json:"is_system"   gorm:"default:false"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`

	// Permissions is filled in by the service layer for response payloads;
	// not stored on the row itself (lives in the role_permissions table).
	Permissions map[string]bool `json:"permissions,omitempty" gorm:"-"`
}

// TableName pins the GORM table name (otherwise it would derive "roles" which
// is fine, but explicit > implicit when types live across packages).
func (Role) TableName() string { return "roles" }

// RolePermission is one row in the matrix; each (role_id, permission_key) is unique.
// Allowed semantics: true = grant the flag; false = explicit deny that even
// overrides higher-tier role defaults (so a custom role can shadow "admin gets
// everything"). Absence of the row means "use system default" (typically false).
type RolePermission struct {
	RoleID        string         `json:"role_id"        gorm:"type:varchar(36);primaryKey"`
	PermissionKey string         `json:"permission_key" gorm:"type:varchar(64);primaryKey"`
	Allowed       bool           `json:"allowed"        gorm:"not null"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName pins the join table name.
func (RolePermission) TableName() string { return "role_permissions" }

// UserGroup is a tenant-scoped user group. Membership is many-to-many via
// user_group_members. A group may carry both a base role (RoleID) and ad-hoc
// per-flag overrides (Permissions), the same shape a single user has.
type UserGroup struct {
	ID          string         `json:"id"          gorm:"type:varchar(36);primaryKey"`
	TenantID    uint64         `json:"tenant_id"   gorm:"index;not null"`
	Name        string         `json:"name"        gorm:"type:varchar(128);not null"`
	Description string         `json:"description" gorm:"type:text"`
	// RoleID assigns a base role to all members. Optional; nil means "no role
	// override; members fall back to their own user.role".
	RoleID *string `json:"role_id,omitempty" gorm:"type:varchar(36)"`
	// Permissions stores per-flag overrides as a JSON map of {flag: bool}.
	// Same shape as users.permissions so the resolver can reuse one helper.
	Permissions *JSON `json:"permissions,omitempty" gorm:"type:jsonb"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`

	// Members is filled by the service for response payloads (count + sample).
	MemberIDs   []string `json:"member_ids,omitempty"   gorm:"-"`
	MemberCount int      `json:"member_count,omitempty" gorm:"-"`
}

// TableName pins the GORM table name.
func (UserGroup) TableName() string { return "user_groups" }

// UserGroupMember is the join table.
type UserGroupMember struct {
	GroupID   string    `json:"group_id" gorm:"type:varchar(36);primaryKey"`
	UserID    string    `json:"user_id"  gorm:"type:varchar(36);primaryKey"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName pins the GORM table name.
func (UserGroupMember) TableName() string { return "user_group_members" }

// GroupKBPermission is the group-level analog of KBUserPermission.
// Resolves identically to user grants but applies to every group member.
type GroupKBPermission struct {
	ID              string         `json:"id"                gorm:"type:varchar(36);primaryKey"`
	KnowledgeBaseID string         `json:"knowledge_base_id" gorm:"type:varchar(36);index;not null"`
	GroupID         string         `json:"group_id"          gorm:"type:varchar(36);index;not null"`
	TenantID        uint64         `json:"tenant_id"         gorm:"index;not null"`
	Permission      KBPermission   `json:"permission"        gorm:"type:varchar(32);not null"`
	GrantedByUserID string         `json:"granted_by_user_id" gorm:"type:varchar(36);not null"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName pins the GORM table name.
func (GroupKBPermission) TableName() string { return "kb_group_permissions" }

// NormalizeRoleKey strips whitespace and lowercases — applied before insert/lookup
// so "Member" and "member" don't drift apart.
func NormalizeRoleKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
