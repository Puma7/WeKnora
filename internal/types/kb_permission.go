package types

import (
	"time"

	"gorm.io/gorm"
)

// KBPermission is the permission level a user holds on a specific knowledge base.
// Values intentionally mirror OrgMemberRole so existing permission checks compose cleanly.
type KBPermission string

const (
	KBPermissionViewer KBPermission = "viewer"
	KBPermissionEditor KBPermission = "editor"
	KBPermissionAdmin  KBPermission = "admin"
)

// IsValid reports whether the permission string is a known value.
func (p KBPermission) IsValid() bool {
	switch p {
	case KBPermissionViewer, KBPermissionEditor, KBPermissionAdmin:
		return true
	default:
		return false
	}
}

// level orders permissions for comparison: admin > editor > viewer.
func (p KBPermission) level() int {
	switch p {
	case KBPermissionAdmin:
		return 3
	case KBPermissionEditor:
		return 2
	case KBPermissionViewer:
		return 1
	default:
		return 0
	}
}

// HasAtLeast returns true when the permission is at least as privileged as required.
func (p KBPermission) HasAtLeast(required KBPermission) bool {
	return p.level() >= required.level()
}

// KBUserPermission records a per-user grant on a knowledge base.
type KBUserPermission struct {
	ID              string         `json:"id" gorm:"type:varchar(36);primaryKey"`
	KnowledgeBaseID string         `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index"`
	UserID          string         `json:"user_id" gorm:"type:varchar(36);not null;index"`
	TenantID        uint64         `json:"tenant_id" gorm:"not null;index"`
	Permission      KBPermission   `json:"permission" gorm:"type:varchar(32);not null;default:'viewer'"`
	GrantedByUserID string         `json:"granted_by_user_id" gorm:"type:varchar(36);not null"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`

	User          *User          `json:"user,omitempty" gorm:"foreignKey:UserID"`
	KnowledgeBase *KnowledgeBase `json:"knowledge_base,omitempty" gorm:"foreignKey:KnowledgeBaseID"`
}

// TableName returns the GORM table name.
func (KBUserPermission) TableName() string {
	return "kb_user_permissions"
}

// GrantKBPermissionRequest is the payload to grant or update a per-user KB permission.
type GrantKBPermissionRequest struct {
	UserID     string       `json:"user_id" binding:"required"`
	Permission KBPermission `json:"permission" binding:"required"`
}

// UpdateKBPermissionRequest is the payload to update an existing grant.
type UpdateKBPermissionRequest struct {
	Permission KBPermission `json:"permission" binding:"required"`
}

// KBUserPermissionResponse is the API representation including user info for display.
type KBUserPermissionResponse struct {
	ID              string       `json:"id"`
	KnowledgeBaseID string       `json:"knowledge_base_id"`
	UserID          string       `json:"user_id"`
	Username        string       `json:"username"`
	Email           string       `json:"email"`
	Avatar          string       `json:"avatar,omitempty"`
	Permission      KBPermission `json:"permission"`
	GrantedByUserID string       `json:"granted_by_user_id"`
	GrantedByName   string       `json:"granted_by_name,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}
