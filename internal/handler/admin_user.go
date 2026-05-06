package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// AdminUserHandler exposes the tenant-scoped user management endpoints.
type AdminUserHandler struct {
	userService interfaces.UserService
}

// NewAdminUserHandler constructs the admin user handler.
func NewAdminUserHandler(userService interfaces.UserService) *AdminUserHandler {
	return &AdminUserHandler{userService: userService}
}

// ListUsers godoc
// @Summary  List all users in the current tenant
// @Tags     admin/users
// @Produce  json
// @Param    page      query  int  false  "Page number (1-based)"
// @Param    page_size query  int  false  "Page size (default 50, max 200)"
// @Success  200  {object}  types.AdminListUsersResponse
// @Router   /admin/users [get]
func (h *AdminUserHandler) ListUsers(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	if !actor.Can("manage_users") && !actor.Can("invite_users") {
		c.Error(errors.NewForbiddenError("permission denied"))
		return
	}

	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if pageSize <= 0 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	users, total, err := h.userService.ListTenantUsers(ctx, actor.TenantID, offset, pageSize)
	if err != nil {
		c.Error(errors.NewInternalServerError("failed to list users").WithDetails(err.Error()))
		return
	}
	infos := make([]*types.UserInfo, 0, len(users))
	for _, u := range users {
		infos = append(infos, u.ToUserInfo())
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": types.AdminListUsersResponse{
			Users: infos,
			Total: total,
		},
	})
}

// UpdateUserRole godoc
// @Summary  Update a user's role
// @Tags     admin/users
// @Accept   json
// @Produce  json
// @Param    id       path  string                       true  "User ID"
// @Param    request  body  types.UpdateUserRoleRequest  true  "New role"
// @Success  200
// @Router   /admin/users/{id}/role [put]
func (h *AdminUserHandler) UpdateUserRole(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.Error(errors.NewValidationError("id is required"))
		return
	}
	var req types.UpdateUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	updated, err := h.userService.UpdateUserRole(ctx, actor, id, req.Role)
	if err != nil {
		logger.Errorf(ctx, "Failed to update user role: %v", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated.ToUserInfo()})
}

// UpdateUserPermissions godoc
// @Summary  Override a user's feature permissions
// @Tags     admin/users
// @Accept   json
// @Produce  json
// @Param    id       path  string                              true  "User ID"
// @Param    request  body  types.UpdateUserPermissionsRequest  true  "Permissions"
// @Success  200
// @Router   /admin/users/{id}/permissions [put]
func (h *AdminUserHandler) UpdateUserPermissions(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.Error(errors.NewValidationError("id is required"))
		return
	}
	var req types.UpdateUserPermissionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	updated, err := h.userService.UpdateUserPermissions(ctx, actor, id, req.Permissions)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated.ToUserInfo()})
}

// SetUserActive godoc
// @Summary  Enable or disable a user account
// @Tags     admin/users
// @Accept   json
// @Produce  json
// @Param    id       path  string                          true  "User ID"
// @Param    request  body  types.UpdateUserActiveRequest   true  "Active flag"
// @Success  200
// @Router   /admin/users/{id}/active [put]
func (h *AdminUserHandler) SetUserActive(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.Error(errors.NewValidationError("id is required"))
		return
	}
	var req types.UpdateUserActiveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	updated, err := h.userService.SetUserActive(ctx, actor, id, req.IsActive)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated.ToUserInfo()})
}
