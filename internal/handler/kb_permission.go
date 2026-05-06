package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// KBPermissionHandler exposes per-user grants on a knowledge base.
//
// Endpoints live under /knowledge-bases/:id/user-permissions and complement the
// existing /knowledge-bases/:id/shares (which are org-level).
type KBPermissionHandler struct {
	service     interfaces.KBPermissionService
	userService interfaces.UserService
}

// NewKBPermissionHandler constructs the handler.
func NewKBPermissionHandler(
	service interfaces.KBPermissionService, userService interfaces.UserService,
) *KBPermissionHandler {
	return &KBPermissionHandler{service: service, userService: userService}
}

// ListGrants godoc
// @Summary  List per-user grants on a knowledge base
// @Tags     knowledge-base/permissions
// @Produce  json
// @Param    id  path  string  true  "Knowledge base ID"
// @Success  200  {object}  map[string]interface{}
// @Router   /knowledge-bases/{id}/user-permissions [get]
func (h *KBPermissionHandler) ListGrants(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	kbID := strings.TrimSpace(c.Param("id"))
	if kbID == "" {
		c.Error(errors.NewValidationError("id is required"))
		return
	}
	grants, err := h.service.ListByKB(ctx, actor, kbID)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": grants})
}

// CreateGrant godoc
// @Summary  Grant a user permission on a knowledge base
// @Tags     knowledge-base/permissions
// @Accept   json
// @Produce  json
// @Param    id       path  string                            true  "Knowledge base ID"
// @Param    request  body  types.GrantKBPermissionRequest    true  "Grant"
// @Success  201
// @Router   /knowledge-bases/{id}/user-permissions [post]
func (h *KBPermissionHandler) CreateGrant(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	kbID := strings.TrimSpace(c.Param("id"))
	if kbID == "" {
		c.Error(errors.NewValidationError("id is required"))
		return
	}
	var req types.GrantKBPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	grant, err := h.service.Grant(ctx, actor, kbID, req.UserID, req.Permission)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": grant})
}

// UpdateGrant godoc
// @Summary  Update an existing user grant
// @Tags     knowledge-base/permissions
// @Accept   json
// @Produce  json
// @Param    id        path  string                              true  "Knowledge base ID"
// @Param    grant_id  path  string                              true  "Grant ID"
// @Param    request   body  types.UpdateKBPermissionRequest    true  "Permission"
// @Success  200
// @Router   /knowledge-bases/{id}/user-permissions/{grant_id} [put]
func (h *KBPermissionHandler) UpdateGrant(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	grantID := strings.TrimSpace(c.Param("grant_id"))
	if grantID == "" {
		c.Error(errors.NewValidationError("grant_id is required"))
		return
	}
	var req types.UpdateKBPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	grant, err := h.service.UpdatePermission(ctx, actor, grantID, req.Permission)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": grant})
}

// RevokeGrant godoc
// @Summary  Revoke a user grant
// @Tags     knowledge-base/permissions
// @Param    id        path  string  true  "Knowledge base ID"
// @Param    grant_id  path  string  true  "Grant ID"
// @Success  200
// @Router   /knowledge-bases/{id}/user-permissions/{grant_id} [delete]
func (h *KBPermissionHandler) RevokeGrant(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	grantID := strings.TrimSpace(c.Param("grant_id"))
	if grantID == "" {
		c.Error(errors.NewValidationError("grant_id is required"))
		return
	}
	if err := h.service.Revoke(ctx, actor, grantID); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
