package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// InvitationHandler exposes the admin endpoints to create / list / revoke invitations
// and the public endpoint to preview an invitation by token.
type InvitationHandler struct {
	service       interfaces.InvitationService
	userService   interfaces.UserService
	tenantService interfaces.TenantService
}

// NewInvitationHandler constructs the handler with its dependencies.
func NewInvitationHandler(
	service interfaces.InvitationService,
	userService interfaces.UserService,
	tenantService interfaces.TenantService,
) *InvitationHandler {
	return &InvitationHandler{service: service, userService: userService, tenantService: tenantService}
}

// CreateInvitation godoc
// @Summary  Create a new invitation
// @Tags     admin/invitations
// @Accept   json
// @Produce  json
// @Param    request  body  types.CreateInvitationRequest  true  "Invitation"
// @Success  201      {object}  types.InvitationResponse
// @Router   /admin/invitations [post]
func (h *InvitationHandler) CreateInvitation(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}

	var req types.CreateInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}

	inv, rawToken, err := h.service.CreateInvitation(ctx, actor, &req)
	if err != nil {
		logger.Errorf(ctx, "Failed to create invitation: %v", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	resp := h.toInvitationResponse(inv, actor)
	resp.MagicLinkPath = "/invite/" + rawToken
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": resp, "token": rawToken})
}

// ListInvitations godoc
// @Summary  List invitations of the current tenant
// @Tags     admin/invitations
// @Produce  json
// @Success  200  {object}  map[string]interface{}
// @Router   /admin/invitations [get]
func (h *InvitationHandler) ListInvitations(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	if !actor.Can("invite_users") && !actor.Can("manage_users") {
		c.Error(errors.NewForbiddenError("permission denied"))
		return
	}

	invs, err := h.service.ListInvitationsByTenant(ctx, actor.TenantID)
	if err != nil {
		c.Error(errors.NewInternalServerError("failed to list invitations").WithDetails(err.Error()))
		return
	}
	out := make([]types.InvitationResponse, 0, len(invs))
	for _, inv := range invs {
		out = append(out, h.toInvitationResponse(inv, actor))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// RevokeInvitation godoc
// @Summary  Revoke a pending invitation
// @Tags     admin/invitations
// @Param    id  path  string  true  "Invitation ID"
// @Success  200
// @Router   /admin/invitations/{id} [delete]
func (h *InvitationHandler) RevokeInvitation(c *gin.Context) {
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
	if err := h.service.RevokeInvitation(ctx, actor, id); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// PreviewInvitation godoc
// @Summary  Public preview of an invitation by token
// @Tags     auth
// @Produce  json
// @Param    token  path  string  true  "Invitation token"
// @Success  200  {object}  types.InvitationPublicView
// @Router   /auth/invitations/{token} [get]
//
// Returns a sanitized view safe for unauthenticated callers; never exposes tenant id,
// permissions, or KB grants — those are applied server-side at acceptance time.
func (h *InvitationHandler) PreviewInvitation(c *gin.Context) {
	ctx := c.Request.Context()
	rawToken := strings.TrimSpace(c.Param("token"))
	if rawToken == "" {
		c.Error(errors.NewValidationError("token is required"))
		return
	}

	inv, err := h.service.GetInvitationByToken(ctx, rawToken)
	if err != nil {
		c.Error(errors.NewNotFoundError("invitation not found"))
		return
	}
	if !inv.IsActive(time.Now()) {
		c.Error(errors.NewBadRequestError("invitation is no longer valid"))
		return
	}

	view := types.InvitationPublicView{
		Email:     inv.Email,
		Role:      inv.Role,
		ExpiresAt: inv.ExpiresAt,
		Note:      inv.Note,
	}
	if inviter, _ := h.userService.GetUserByID(ctx, inv.InvitedByUserID); inviter != nil {
		view.InvitedByUsername = inviter.Username
	}
	if tenant, _ := h.tenantService.GetTenantByID(ctx, inv.TenantID); tenant != nil {
		view.TenantName = tenant.Name
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func (h *InvitationHandler) toInvitationResponse(inv *types.UserInvitation, actor *types.User) types.InvitationResponse {
	resp := types.InvitationResponse{
		ID:              inv.ID,
		Email:           inv.Email,
		TenantID:        inv.TenantID,
		InvitedByUserID: inv.InvitedByUserID,
		Role:            inv.Role,
		Status:          inv.Status,
		ExpiresAt:       inv.ExpiresAt,
		AcceptedAt:      inv.AcceptedAt,
		AcceptedUserID:  inv.AcceptedUserID,
		RevokedAt:       inv.RevokedAt,
		Note:            inv.Note,
		CreatedAt:       inv.CreatedAt,
	}
	if inv.Permissions != nil {
		var perms types.UserPermissions
		if err := inv.Permissions.Unmarshal(&perms); err == nil {
			resp.Permissions = &perms
		}
	}
	if inv.KnowledgeBaseGrants != nil {
		var grants []types.InvitationKBGrant
		if err := inv.KnowledgeBaseGrants.Unmarshal(&grants); err == nil {
			resp.KnowledgeBaseGrants = grants
		}
	}
	if actor != nil && actor.ID == inv.InvitedByUserID {
		resp.InvitedByUsername = actor.Username
	} else if inv.InvitedByUser != nil {
		resp.InvitedByUsername = inv.InvitedByUser.Username
	}
	return resp
}
