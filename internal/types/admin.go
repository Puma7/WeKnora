package types

// UpdateUserRoleRequest is the admin payload to change a user's global role.
type UpdateUserRoleRequest struct {
	Role UserRole `json:"role" binding:"required"`
}

// UpdateUserPermissionsRequest overrides feature flags for a single user.
// Pass null/missing for fields that should fall back to the role's defaults.
type UpdateUserPermissionsRequest struct {
	Permissions UserPermissions `json:"permissions"`
}

// UpdateUserActiveRequest enables/disables a user account without deleting it.
type UpdateUserActiveRequest struct {
	IsActive bool `json:"is_active"`
}

// AdminListUsersResponse is the paginated response for /admin/users.
type AdminListUsersResponse struct {
	Users []*UserInfo `json:"users"`
	Total int64       `json:"total"`
}
