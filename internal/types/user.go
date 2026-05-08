package types

import (
	"time"

	"gorm.io/gorm"
)

// UserRole represents the global role of a user inside their tenant.
// Roles are ordered from most to least privileged. Use HasAtLeast to compare.
type UserRole string

const (
	// UserRoleOwner has full control of the tenant; only one per tenant by convention.
	UserRoleOwner UserRole = "owner"
	// UserRoleAdmin can invite users, manage permissions, and create knowledge bases.
	UserRoleAdmin UserRole = "admin"
	// UserRoleMember can use chat/search and create their own knowledge bases.
	UserRoleMember UserRole = "member"
	// UserRoleViewer can only consume content (chat/search on permitted KBs).
	UserRoleViewer UserRole = "viewer"
)

// IsValid reports whether the role string is one of the known values.
func (r UserRole) IsValid() bool {
	switch r {
	case UserRoleOwner, UserRoleAdmin, UserRoleMember, UserRoleViewer:
		return true
	default:
		return false
	}
}

// roleLevel maps roles to comparable integers; higher = more privileged.
func (r UserRole) level() int {
	switch r {
	case UserRoleOwner:
		return 4
	case UserRoleAdmin:
		return 3
	case UserRoleMember:
		return 2
	case UserRoleViewer:
		return 1
	default:
		return 0
	}
}

// HasAtLeast returns true when the current role is at least as privileged as required.
func (r UserRole) HasAtLeast(required UserRole) bool {
	return r.level() >= required.level()
}

// UserPermissions captures feature-level toggles that override role defaults.
// Pointers distinguish "not set / use role default" from "explicitly false".
type UserPermissions struct {
	CanChat        *bool `json:"can_chat,omitempty"`
	CanSearch      *bool `json:"can_search,omitempty"`
	CanCreateKB    *bool `json:"can_create_kb,omitempty"`
	CanInviteUsers *bool `json:"can_invite_users,omitempty"`
	CanManageUsers *bool `json:"can_manage_users,omitempty"`
	CanManageKBs   *bool `json:"can_manage_kbs,omitempty"`
}

// boolPtr returns the value of a *bool with the supplied default applied.
func boolPtr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// User represents a user in the system
type User struct {
	// Unique identifier of the user
	ID string `json:"id"         gorm:"type:varchar(36);primaryKey"`
	// Username of the user
	Username string `json:"username"   gorm:"type:varchar(100);uniqueIndex;not null"`
	// Email address of the user
	Email string `json:"email"      gorm:"type:varchar(255);uniqueIndex;not null"`
	// Hashed password of the user
	PasswordHash string `json:"-"          gorm:"type:varchar(255);not null"`
	// Avatar URL of the user
	Avatar string `json:"avatar"     gorm:"type:varchar(500)"`
	// Tenant ID that the user belongs to
	TenantID uint64 `json:"tenant_id"  gorm:"index"`
	// Whether the user is active
	IsActive bool `json:"is_active"  gorm:"default:true"`
	// Whether the user can access all tenants (cross-tenant access)
	CanAccessAllTenants bool `json:"can_access_all_tenants" gorm:"default:false"`
	// Global role within the tenant
	Role UserRole `json:"role" gorm:"type:varchar(32);not null;default:'member'"`
	// Optional feature-flag overrides (JSON map persisted as JSONB)
	Permissions *JSON `json:"permissions,omitempty" gorm:"type:jsonb"`
	// User ID of the admin who invited this account; empty for self-registered or OIDC users
	InvitedByUserID string `json:"invited_by_user_id,omitempty" gorm:"type:varchar(36);column:invited_by_user_id"`
	// EffectiveCache is filled by the auth middleware via PermissionResolverService
	// before the request reaches a handler. When non-nil, EffectivePermissions()
	// returns it directly instead of running the legacy hard-coded resolution.
	// Not persisted (gorm:"-"), not serialized (json:"-").
	EffectiveCache *UserPermissions `json:"-" gorm:"-"`
	// Creation time of the user
	CreatedAt time.Time `json:"created_at"`
	// Last updated time of the user
	UpdatedAt time.Time `json:"updated_at"`
	// Deletion time of the user
	DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"index"`

	// Association relationship, not stored in the database
	Tenant *Tenant `json:"tenant,omitempty" gorm:"foreignKey:TenantID"`
}

// EffectivePermissions returns the user's effective feature flags.
//
// Resolution order:
//  1. EffectiveCache (filled by PermissionResolverService inside the auth
//     middleware) — preferred, since it accounts for custom roles, group
//     membership, and tenant-scoped role overrides
//  2. Legacy hard-coded role map — defense-in-depth fallback for code paths
//     that don't go through the resolver (tests, Lite first-boot before the
//     service is wired, internal jobs that call user.Can() with a stub User)
//
// The legacy map is intentionally preserved exactly as-is so a misconfigured
// deploy where the resolver fails to populate the cache degrades gracefully
// to the pre-RBACv2 behavior rather than locking everyone out.
func (u *User) EffectivePermissions() UserPermissions {
	if u.EffectiveCache != nil {
		return *u.EffectiveCache
	}
	role := u.Role
	// GEÄNDERT: only the truly-empty case falls back to Member. Custom-role
	// strings (e.g. "marketing-editor") are NOT replaced — they fall through
	// the legacy switch with no hardcoded defaults, which means custom-role
	// users in the legacy fallback path get all-deny + their own overrides.
	// That's the safe choice when the resolver isn't available; the operator
	// must run the seed migration to make custom roles meaningful.
	if role == "" {
		role = UserRoleMember
	}

	// Defaults derived from role; admins/owners get everything.
	defaults := UserPermissions{}
	switch role {
	case UserRoleOwner, UserRoleAdmin:
		t := true
		defaults = UserPermissions{
			CanChat: &t, CanSearch: &t, CanCreateKB: &t,
			CanInviteUsers: &t, CanManageUsers: &t, CanManageKBs: &t,
		}
	case UserRoleMember:
		t, f := true, false
		defaults = UserPermissions{
			CanChat: &t, CanSearch: &t, CanCreateKB: &t,
			CanInviteUsers: &f, CanManageUsers: &f, CanManageKBs: &f,
		}
	case UserRoleViewer:
		t, f := true, false
		defaults = UserPermissions{
			CanChat: &t, CanSearch: &t, CanCreateKB: &f,
			CanInviteUsers: &f, CanManageUsers: &f, CanManageKBs: &f,
		}
	}

	overrides := UserPermissions{}
	if u.Permissions != nil {
		_ = u.Permissions.Unmarshal(&overrides)
	}

	merge := func(o, d *bool) *bool {
		if o != nil {
			return o
		}
		return d
	}
	return UserPermissions{
		CanChat:        merge(overrides.CanChat, defaults.CanChat),
		CanSearch:      merge(overrides.CanSearch, defaults.CanSearch),
		CanCreateKB:    merge(overrides.CanCreateKB, defaults.CanCreateKB),
		CanInviteUsers: merge(overrides.CanInviteUsers, defaults.CanInviteUsers),
		CanManageUsers: merge(overrides.CanManageUsers, defaults.CanManageUsers),
		CanManageKBs:   merge(overrides.CanManageKBs, defaults.CanManageKBs),
	}
}

// Can returns the value of a single feature flag, defaulting to false.
func (u *User) Can(flag string) bool {
	p := u.EffectivePermissions()
	switch flag {
	case "chat":
		return boolPtr(p.CanChat, false)
	case "search":
		return boolPtr(p.CanSearch, false)
	case "create_kb":
		return boolPtr(p.CanCreateKB, false)
	case "invite_users":
		return boolPtr(p.CanInviteUsers, false)
	case "manage_users":
		return boolPtr(p.CanManageUsers, false)
	case "manage_kbs":
		return boolPtr(p.CanManageKBs, false)
	default:
		return false
	}
}

// AuthToken represents an authentication token
type AuthToken struct {
	// Unique identifier of the token
	ID string `json:"id"         gorm:"type:varchar(36);primaryKey"`
	// User ID that owns this token
	UserID string `json:"user_id"    gorm:"type:varchar(36);index;not null"`
	// Token value (JWT or other format)
	Token string `json:"token"      gorm:"type:text;not null"`
	// Token type (access_token, refresh_token)
	TokenType string `json:"token_type" gorm:"type:varchar(50);not null"`
	// Token expiration time
	ExpiresAt time.Time `json:"expires_at"`
	// Whether the token is revoked
	IsRevoked bool `json:"is_revoked" gorm:"default:false"`
	// Creation time of the token
	CreatedAt time.Time `json:"created_at"`
	// Last updated time of the token
	UpdatedAt time.Time `json:"updated_at"`

	// Association relationship
	User *User `json:"user,omitempty" gorm:"foreignKey:UserID"`
}

// LoginRequest represents a login request
type LoginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type OIDCAuthURLResponse struct {
	Success             bool   `json:"success"`
	ProviderDisplayName string `json:"provider_display_name,omitempty"`
	AuthorizationURL    string `json:"authorization_url,omitempty"`
	State               string `json:"state,omitempty"`
}

type OIDCConfigResponse struct {
	Success             bool   `json:"success"`
	Enabled             bool   `json:"enabled"`
	ProviderDisplayName string `json:"provider_display_name,omitempty"`
}

type OIDCCallbackResponse struct {
	Success      bool    `json:"success"`
	Message      string  `json:"message,omitempty"`
	User         *User   `json:"user,omitempty"`
	Tenant       *Tenant `json:"tenant,omitempty"`
	Token        string  `json:"token,omitempty"`
	RefreshToken string  `json:"refresh_token,omitempty"`
	IsNewUser    bool    `json:"is_new_user,omitempty"`
}

type OIDCUserInfo struct {
	Subject  string                 `json:"subject,omitempty"`
	Username string                 `json:"username,omitempty"`
	Email    string                 `json:"email,omitempty"`
	Claims   map[string]interface{} `json:"claims,omitempty"`
}

// RegisterRequest represents a registration request
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=2,max=50"`
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	// InvitationToken redeems a pending invitation; required in invite_only mode
	// unless the email is on the registration whitelist.
	InvitationToken string `json:"invitation_token,omitempty"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	Success      bool    `json:"success"`
	Message      string  `json:"message,omitempty"`
	User         *User   `json:"user,omitempty"`
	Tenant       *Tenant `json:"tenant,omitempty"`
	Token        string  `json:"token,omitempty"`
	RefreshToken string  `json:"refresh_token,omitempty"`
}

// RegisterResponse represents a registration response
type RegisterResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message,omitempty"`
	User    *User   `json:"user,omitempty"`
	Tenant  *Tenant `json:"tenant,omitempty"`
}

// UserInfo represents user information for API responses
type UserInfo struct {
	ID                  string          `json:"id"`
	Username            string          `json:"username"`
	Email               string          `json:"email"`
	Avatar              string          `json:"avatar"`
	TenantID            uint64          `json:"tenant_id"`
	IsActive            bool            `json:"is_active"`
	CanAccessAllTenants bool            `json:"can_access_all_tenants"`
	Role                UserRole        `json:"role"`
	Permissions         UserPermissions `json:"permissions"`
	InvitedByUserID     string          `json:"invited_by_user_id,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// ToUserInfo converts User to UserInfo (without sensitive data)
// GEÄNDERT: returns the actual stored role string (including custom roles).
// Only the truly-empty role is replaced with Member as a safety net for
// pre-RBAC users — never the case for users created after migration 000040.
func (u *User) ToUserInfo() *UserInfo {
	role := u.Role
	if role == "" {
		role = UserRoleMember
	}
	return &UserInfo{
		ID:                  u.ID,
		Username:            u.Username,
		Email:               u.Email,
		Avatar:              u.Avatar,
		TenantID:            u.TenantID,
		IsActive:            u.IsActive,
		CanAccessAllTenants: u.CanAccessAllTenants,
		Role:                role,
		Permissions:         u.EffectivePermissions(),
		InvitedByUserID:     u.InvitedByUserID,
		CreatedAt:           u.CreatedAt,
		UpdatedAt:           u.UpdatedAt,
	}
}
