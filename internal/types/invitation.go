package types

import (
	"time"

	"gorm.io/gorm"
)

// InvitationStatus represents the lifecycle status of a user invitation.
type InvitationStatus string

const (
	InvitationStatusPending  InvitationStatus = "pending"
	InvitationStatusAccepted InvitationStatus = "accepted"
	InvitationStatusRevoked  InvitationStatus = "revoked"
	InvitationStatusExpired  InvitationStatus = "expired"
)

// IsValid reports whether the status string is a known value.
func (s InvitationStatus) IsValid() bool {
	switch s {
	case InvitationStatusPending, InvitationStatusAccepted, InvitationStatusRevoked, InvitationStatusExpired:
		return true
	default:
		return false
	}
}

// InvitationKBGrant captures a KB-level permission to apply when an invitation is accepted.
type InvitationKBGrant struct {
	KnowledgeBaseID string       `json:"knowledge_base_id" binding:"required"`
	Permission      KBPermission `json:"permission" binding:"required"`
}

// UserInvitation represents an admin-issued invitation.
//
// Acceptance flow:
//  1. Admin POSTs /admin/invitations -> row inserted with token + expires_at.
//  2. Frontend builds magic link `/invite/<token>` for the admin to share.
//  3. Invitee opens link -> frontend GETs /auth/invitations/:token to display role,
//     pre-fill email, and confirm validity.
//  4. Invitee POSTs /auth/register-with-invitation with token + username + password;
//     backend creates the user (linked to invited_by_user_id) and applies grants.
type UserInvitation struct {
	ID                  string           `json:"id" gorm:"type:varchar(36);primaryKey"`
	Email               string           `json:"email" gorm:"type:varchar(255);not null;index"`
	Token               string           `json:"-" gorm:"type:varchar(128);not null"`
	TenantID            uint64           `json:"tenant_id" gorm:"not null;index"`
	InvitedByUserID     string           `json:"invited_by_user_id" gorm:"type:varchar(36);not null;index"`
	Role                UserRole         `json:"role" gorm:"type:varchar(32);not null;default:'member'"`
	Permissions         *JSON            `json:"permissions,omitempty" gorm:"type:jsonb"`
	KnowledgeBaseGrants *JSON            `json:"knowledge_base_grants,omitempty" gorm:"column:knowledge_base_grants;type:jsonb"`
	Status              InvitationStatus `json:"status" gorm:"type:varchar(32);not null;default:'pending';index"`
	ExpiresAt           time.Time        `json:"expires_at" gorm:"not null"`
	AcceptedAt          *time.Time       `json:"accepted_at,omitempty"`
	AcceptedUserID      string           `json:"accepted_user_id,omitempty" gorm:"type:varchar(36)"`
	RevokedAt           *time.Time       `json:"revoked_at,omitempty"`
	RevokedByUserID     string           `json:"revoked_by_user_id,omitempty" gorm:"type:varchar(36)"`
	Note                string           `json:"note,omitempty" gorm:"type:text"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
	DeletedAt           gorm.DeletedAt   `json:"deleted_at,omitempty" gorm:"index"`

	InvitedByUser *User `json:"invited_by_user,omitempty" gorm:"foreignKey:InvitedByUserID"`
}

// TableName returns the GORM table name.
func (UserInvitation) TableName() string {
	return "user_invitations"
}

// IsActive reports whether the invitation can still be accepted right now.
func (i *UserInvitation) IsActive(now time.Time) bool {
	if i.Status != InvitationStatusPending {
		return false
	}
	return now.Before(i.ExpiresAt)
}

// CreateInvitationRequest is the admin payload to create a new invitation.
type CreateInvitationRequest struct {
	Email             string              `json:"email" binding:"required,email"`
	Role              UserRole            `json:"role"`
	Permissions       *UserPermissions    `json:"permissions,omitempty"`
	KnowledgeBaseGrants []InvitationKBGrant `json:"knowledge_base_grants,omitempty"`
	// ValidityDays sets the magic-link lifetime (1..90, default 7).
	ValidityDays int    `json:"validity_days,omitempty"`
	Note         string `json:"note,omitempty"`
}

// AcceptInvitationRequest is the public payload to redeem an invitation.
type AcceptInvitationRequest struct {
	Token    string `json:"token" binding:"required"`
	Username string `json:"username" binding:"required,min=2,max=50"`
	Password string `json:"password" binding:"required,min=6"`
}

// InvitationResponse is the admin-facing representation of an invitation row.
type InvitationResponse struct {
	ID                  string              `json:"id"`
	Email               string              `json:"email"`
	TenantID            uint64              `json:"tenant_id"`
	InvitedByUserID     string              `json:"invited_by_user_id"`
	InvitedByUsername   string              `json:"invited_by_username,omitempty"`
	Role                UserRole            `json:"role"`
	Permissions         *UserPermissions    `json:"permissions,omitempty"`
	KnowledgeBaseGrants []InvitationKBGrant `json:"knowledge_base_grants,omitempty"`
	Status              InvitationStatus    `json:"status"`
	ExpiresAt           time.Time           `json:"expires_at"`
	AcceptedAt          *time.Time          `json:"accepted_at,omitempty"`
	AcceptedUserID      string              `json:"accepted_user_id,omitempty"`
	RevokedAt           *time.Time          `json:"revoked_at,omitempty"`
	Note                string              `json:"note,omitempty"`
	CreatedAt           time.Time           `json:"created_at"`
	// MagicLinkPath is the relative URL the admin can share. Frontend joins with the public origin.
	MagicLinkPath string `json:"magic_link_path,omitempty"`
}

// InvitationPublicView is the limited information shown to the invitee before they accept.
type InvitationPublicView struct {
	Email             string    `json:"email"`
	Role              UserRole  `json:"role"`
	InvitedByUsername string    `json:"invited_by_username,omitempty"`
	TenantName        string    `json:"tenant_name,omitempty"`
	ExpiresAt         time.Time `json:"expires_at"`
	Note              string    `json:"note,omitempty"`
}
