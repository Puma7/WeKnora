package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// AdminGroupHandler exposes group CRUD + membership + permission endpoints.
// Gated at the route level by RequireFeature("manage_users").
type AdminGroupHandler struct {
	userService  interfaces.UserService
	groupService interfaces.GroupService
}

// NewAdminGroupHandler constructs the handler.
func NewAdminGroupHandler(
	userService interfaces.UserService,
	groupService interfaces.GroupService,
) *AdminGroupHandler {
	return &AdminGroupHandler{userService: userService, groupService: groupService}
}

// groupResponse is the wire shape: mirrors UserGroup with permissions parsed.
type groupResponse struct {
	ID          string             `json:"id"`
	TenantID    uint64             `json:"tenant_id"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	RoleID      *string            `json:"role_id,omitempty"`
	Permissions types.UserPermissions `json:"permissions"`
	MemberIDs   []string           `json:"member_ids,omitempty"`
	MemberCount int                `json:"member_count"`
}

func toGroupResponse(g *types.UserGroup) groupResponse {
	perms := types.UserPermissions{}
	if g.Permissions != nil {
		_ = g.Permissions.Unmarshal(&perms)
	}
	return groupResponse{
		ID:          g.ID,
		TenantID:    g.TenantID,
		Name:        g.Name,
		Description: g.Description,
		RoleID:      g.RoleID,
		Permissions: perms,
		MemberIDs:   g.MemberIDs,
		MemberCount: g.MemberCount,
	}
}

// ListGroups returns all groups in the actor's tenant.
func (h *AdminGroupHandler) ListGroups(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	groups, err := h.groupService.ListGroups(ctx, actor.TenantID)
	if err != nil {
		c.Error(errors.NewInternalServerError("failed to list groups").WithDetails(err.Error()))
		return
	}
	out := make([]groupResponse, 0, len(groups))
	for _, g := range groups {
		out = append(out, toGroupResponse(g))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// GetGroup returns one group with its members.
func (h *AdminGroupHandler) GetGroup(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	g, err := h.groupService.GetGroup(ctx, actor.TenantID, c.Param("id"))
	if err != nil {
		c.Error(errors.NewNotFoundError("group not found").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": toGroupResponse(g)})
}

type createGroupRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	RoleID      *string `json:"role_id,omitempty"`
}

// CreateGroup inserts a new group in the actor's tenant.
func (h *AdminGroupHandler) CreateGroup(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	var req createGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	g := &types.UserGroup{
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		RoleID:      req.RoleID,
	}
	created, err := h.groupService.CreateGroup(ctx, actor, g)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": toGroupResponse(created)})
}

type updateGroupRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	RoleID      *string `json:"role_id,omitempty"`
}

// UpdateGroup renames / re-roles an existing group.
func (h *AdminGroupHandler) UpdateGroup(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	var req updateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	g := &types.UserGroup{
		ID:          c.Param("id"),
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		RoleID:      req.RoleID,
	}
	updated, err := h.groupService.UpdateGroup(ctx, actor, g)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": toGroupResponse(updated)})
}

// DeleteGroup removes a group plus all memberships and KB grants.
func (h *AdminGroupHandler) DeleteGroup(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	if err := h.groupService.DeleteGroup(ctx, actor, c.Param("id")); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

type addMemberRequest struct {
	UserID string `json:"user_id"`
}

// AddMember adds a user to the group.
func (h *AdminGroupHandler) AddMember(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	var req addMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	if err := h.groupService.AddMember(ctx, actor, c.Param("id"), strings.TrimSpace(req.UserID)); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RemoveMember removes a user from the group.
func (h *AdminGroupHandler) RemoveMember(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	if err := h.groupService.RemoveMember(ctx, actor, c.Param("id"), c.Param("user_id")); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListMembers returns the user records of all current group members.
func (h *AdminGroupHandler) ListMembers(c *gin.Context) {
	ctx := c.Request.Context()
	actor, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("authentication required"))
		return
	}
	users, err := h.groupService.ListMembers(ctx, actor.TenantID, c.Param("id"))
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	infos := make([]*types.UserInfo, 0, len(users))
	for _, u := range users {
		infos = append(infos, u.ToUserInfo())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": infos})
}

// SetGroupPermission upserts a per-flag override on the group.
func (h *AdminGroupHandler) SetGroupPermission(c *gin.Context) {
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
	if err := h.groupService.SetGroupPermission(ctx, actor, c.Param("id"), c.Param("key"), req.Allowed); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
