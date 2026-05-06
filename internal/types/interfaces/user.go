package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// UserService defines the user service interface
type UserService interface {
	// Register creates a new user account
	Register(ctx context.Context, req *types.RegisterRequest) (*types.User, error)
	// RegisterTrusted creates a user while bypassing the registration-mode gate.
	// Reserved for trusted callers (AutoSetup on Lite first-boot, OIDC auto-provisioning).
	RegisterTrusted(ctx context.Context, req *types.RegisterRequest) (*types.User, error)
	// Login authenticates a user and returns tokens
	Login(ctx context.Context, req *types.LoginRequest) (*types.LoginResponse, error)
	// GetOIDCAuthorizationURL builds the third-party OIDC authorization URL
	GetOIDCAuthorizationURL(ctx context.Context, redirectURI string) (*types.OIDCAuthURLResponse, error)
	// LoginWithOIDC exchanges the callback code, auto-provisions users if needed, and completes login
	LoginWithOIDC(ctx context.Context, code, redirectURI string) (*types.OIDCCallbackResponse, error)
	// GetUserByID gets a user by ID
	GetUserByID(ctx context.Context, id string) (*types.User, error)
	// GetUserByEmail gets a user by email
	GetUserByEmail(ctx context.Context, email string) (*types.User, error)
	// GetUserByUsername gets a user by username
	GetUserByUsername(ctx context.Context, username string) (*types.User, error)
	// GetUserByTenantID gets the first user (owner) of a tenant
	GetUserByTenantID(ctx context.Context, tenantID uint64) (*types.User, error)
	// UpdateUser updates user information
	UpdateUser(ctx context.Context, user *types.User) error
	// DeleteUser deletes a user
	DeleteUser(ctx context.Context, id string) error
	// ChangePassword changes user password
	ChangePassword(ctx context.Context, userID string, oldPassword, newPassword string) error
	// ValidatePassword validates user password
	ValidatePassword(ctx context.Context, userID string, password string) error
	// GenerateTokens generates access and refresh tokens for user
	GenerateTokens(ctx context.Context, user *types.User) (accessToken, refreshToken string, err error)
	// ValidateToken validates an access token
	ValidateToken(ctx context.Context, token string) (*types.User, error)
	// RefreshToken refreshes access token using refresh token
	RefreshToken(ctx context.Context, refreshToken string) (accessToken, newRefreshToken string, err error)
	// RevokeToken revokes a token
	RevokeToken(ctx context.Context, token string) error
	// GetCurrentUser gets current user from context
	GetCurrentUser(ctx context.Context) (*types.User, error)
	// SearchUsers searches users by username or email
	SearchUsers(ctx context.Context, query string, limit int) ([]*types.User, error)
	// SearchUsersInTenant scopes the search to a single tenant.
	SearchUsersInTenant(ctx context.Context, tenantID uint64, query string, limit int) ([]*types.User, error)
	// RegistrationSettings returns the active registration mode + whitelist (env-derived).
	RegistrationSettings(ctx context.Context) types.RegistrationSettings
	// ListTenantUsers lists users belonging to the given tenant (admin scope).
	ListTenantUsers(ctx context.Context, tenantID uint64, offset, limit int) ([]*types.User, int64, error)
	// UpdateUserRole updates a user's global role; the actor must outrank the target.
	UpdateUserRole(ctx context.Context, actor *types.User, targetUserID string, role types.UserRole) (*types.User, error)
	// UpdateUserPermissions overrides a user's feature flags.
	UpdateUserPermissions(ctx context.Context, actor *types.User, targetUserID string, perms types.UserPermissions) (*types.User, error)
	// SetUserActive enables or disables a user account.
	SetUserActive(ctx context.Context, actor *types.User, targetUserID string, active bool) (*types.User, error)
}

// UserRepository defines the user repository interface
type UserRepository interface {
	// CreateUser creates a user
	CreateUser(ctx context.Context, user *types.User) error
	// GetUserByID gets a user by ID
	GetUserByID(ctx context.Context, id string) (*types.User, error)
	// GetUserByEmail gets a user by email
	GetUserByEmail(ctx context.Context, email string) (*types.User, error)
	// GetUserByUsername gets a user by username
	GetUserByUsername(ctx context.Context, username string) (*types.User, error)
	// GetUserByTenantID gets the first user (owner) of a tenant
	GetUserByTenantID(ctx context.Context, tenantID uint64) (*types.User, error)
	// UpdateUser updates a user
	UpdateUser(ctx context.Context, user *types.User) error
	// DeleteUser deletes a user
	DeleteUser(ctx context.Context, id string) error
	// ListUsers lists users with pagination
	ListUsers(ctx context.Context, offset, limit int) ([]*types.User, error)
	// ListUsersByTenant lists users in a single tenant with pagination + total count.
	ListUsersByTenant(ctx context.Context, tenantID uint64, offset, limit int) ([]*types.User, int64, error)
	// SearchUsers searches users by username or email
	SearchUsers(ctx context.Context, query string, limit int) ([]*types.User, error)
	// SearchUsersInTenant searches users by username or email scoped to a single tenant.
	SearchUsersInTenant(ctx context.Context, tenantID uint64, query string, limit int) ([]*types.User, error)
	// GetUsersByIDs loads a batch of users by ID (for joining display data).
	GetUsersByIDs(ctx context.Context, ids []string) ([]*types.User, error)
	// CountActiveOwners returns the number of active users with role='owner' in a tenant.
	// Used to prevent demoting the last owner under race conditions.
	CountActiveOwners(ctx context.Context, tenantID uint64) (int64, error)
	// DemoteOwnerIfSafe atomically changes the user's role to newRole only when
	// at least one OTHER active owner remains in the tenant. Returns (true, nil)
	// when the demotion succeeded, (false, nil) when the user is the sole owner
	// (no rows updated), or (_, err) on a database error.
	DemoteOwnerIfSafe(ctx context.Context, userID string, tenantID uint64, newRole string) (bool, error)
}

// AuthTokenRepository defines the auth token repository interface
type AuthTokenRepository interface {
	// CreateToken creates an auth token
	CreateToken(ctx context.Context, token *types.AuthToken) error
	// GetTokenByValue gets a token by its value
	GetTokenByValue(ctx context.Context, tokenValue string) (*types.AuthToken, error)
	// GetTokensByUserID gets all tokens for a user
	GetTokensByUserID(ctx context.Context, userID string) ([]*types.AuthToken, error)
	// UpdateToken updates a token
	UpdateToken(ctx context.Context, token *types.AuthToken) error
	// DeleteToken deletes a token
	DeleteToken(ctx context.Context, id string) error
	// DeleteExpiredTokens deletes all expired tokens
	DeleteExpiredTokens(ctx context.Context) error
	// RevokeTokensByUserID revokes all tokens for a user
	RevokeTokensByUserID(ctx context.Context, userID string) error
}
