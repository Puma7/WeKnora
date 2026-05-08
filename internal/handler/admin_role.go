package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// AdminRoleHandler exposes role CRUD + permission matrix endpoints.
// Gated at the route level by RequireFeature("manage_users") so a 403 is
// returned before the handler runs for unauthorized callers.
type AdminRoleHandler struct {
	userService  interfaces.UserService
	roleService  interfaces.RoleService
	resolver     interfaces.PermissionResolverService
}

// NewAdminRoleHandler constructs the handler.
func NewAdminRoleHandler(
	userService interfaces.UserService,
	roleService interfaces.RoleService,
	resolver interfaces.PermissionResolverService,
) *AdminRoleHandler {
	return &AdminRoleHandler{userService: userService, roleService: roleService, resolver: resolver}
}

// roleResponse is the wire shape for a single role; mirrors types.Role with
// the permission map flattened for easy frontend consumption.
type roleResponse struct {
	ID          string          `json:"id"`
	TenantID    uint64          `json:"tenant_id"`
	Key         string          `json:"key"`
	Label       string          `json:"label"`
	Description string          `json:"description,omitempty"`
	IsSystem    bool            `json:"is_system"`
	Permissions map[string]bool `json:"permissions"`
}

func toRoleResponse(role *types.Role) roleResponse {
	if role.Permissions == nil {
		role.Permissions = map[string]bool{}
	}
	return roleResponse{
		ID:          role.ID,
		TenantID:    role.TenantID,
		Key:         role.Key,
		Label:       role.Label,
		Description: role.Description,
		IsSystem:    role.IsSystem,
		Permissions: role.Permissions,
	}
}

// ListRoles returns all roles visible to the actor's tenant + the permission catalog.
// Returning the catalog alongside the roles means the frontend matrix editor
// can render with one round-trip — no need for a second /catalog call.
func (h *AdminRoleHandler) ListRoles(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	roles, err := h.roleService.ListRoles(ctx, actor.TenantID)
	if err != nil {
		c.Error(errors.NewInternalServerError("failed to list roles").WithDetails(err.Error()))
		return
	}
	out := make([]roleResponse, 0, len(roles))
	for _, role := range roles {
		out = append(out, toRoleResponse(role))
	}
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"data":     out,
		"catalog":  h.resolver.PermissionCatalog(),
	})
}

// GetRole returns a single role with its full permission matrix.
func (h *AdminRoleHandler) GetRole(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	role, err := h.roleService.GetRole(ctx, actor.TenantID, c.Param("id"))
	if err != nil {
		c.Error(errors.NewNotFoundError("role not found").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": toRoleResponse(role)})
}

// createRoleRequest is the JSON body for POST /admin/roles.
type createRoleRequest struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// CreateRole inserts a tenant-scoped custom role.
func (h *AdminRoleHandler) CreateRole(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	var req createRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	role := &types.Role{
		Key:         req.Key,
		Label:       strings.TrimSpace(req.Label),
		Description: strings.TrimSpace(req.Description),
	}
	created, err := h.roleService.CreateRole(ctx, actor, role)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": toRoleResponse(created)})
}

// updateRoleRequest is the JSON body for PATCH /admin/roles/:id.
type updateRoleRequest struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// UpdateRole renames / re-describes an existing custom role.
func (h *AdminRoleHandler) UpdateRole(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	role := &types.Role{
		ID:          c.Param("id"),
		Key:         req.Key,
		Label:       strings.TrimSpace(req.Label),
		Description: strings.TrimSpace(req.Description),
	}
	updated, err := h.roleService.UpdateRole(ctx, actor, role)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": toRoleResponse(updated)})
}

// DeleteRole removes a custom role. Refused with structured error when the
// role is still referenced by users or groups.
func (h *AdminRoleHandler) DeleteRole(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	if err := h.roleService.DeleteRole(ctx, actor, c.Param("id")); err != nil {
		// "role_in_use:" prefix is the agreed-on signal between service and
		// frontend — i18n handler can match on it to render a specific message.
		if strings.HasPrefix(err.Error(), "role_in_use") {
			c.Error(errors.NewBadRequestError(err.Error()))
			return
		}
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// setPermissionRequest is the JSON body for PUT /admin/roles/:id/permissions/:key.
// Allowed pointer distinguishes:
//   - true  -> grant the flag
//   - false -> explicit deny
//   - nil (field omitted) -> clear the cell, fall back to system default
type setPermissionRequest struct {
	Allowed *bool `json:"allowed,omitempty"`
}

// SetRolePermission upserts (or clears) one matrix cell.
func (h *AdminRoleHandler) SetRolePermission(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	var req setPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	if err := h.roleService.SetRolePermission(ctx, actor, c.Param("id"), c.Param("key"), req.Allowed); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetPermissionCatalog returns the catalog of available permission flags.
// Used by the matrix editor before any role is selected.
func (h *AdminRoleHandler) GetPermissionCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.resolver.PermissionCatalog()})
}
