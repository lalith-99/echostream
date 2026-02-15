package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/repository"
	"go.uber.org/zap"
)

// MembershipHandler handles channel membership operations.
type MembershipHandler struct {
	repo   repository.MembershipRepository
	logger *zap.Logger
}

func NewMembershipHandler(repo repository.MembershipRepository, logger *zap.Logger) *MembershipHandler {
	return &MembershipHandler{repo: repo, logger: logger}
}

// joinChannelRequest is the JSON body for POST /v1/channels/:id/join
//
// Role defaults to "member" if not provided. Other roles: "admin", "moderator".
// In a real system, only admins can set roles other than "member", but we'll
// keep it simple for now.
type joinChannelRequest struct {
	Role string `json:"role"`
}

// Join handles POST /v1/channels/:id/join
//
// Why a separate "join" endpoint instead of POST /v1/channels/:id/members?
//   - Semantics. "Join" is a user action on themselves. Adding a member is
//     an admin action on someone else. We'd need two endpoints anyway:
//       POST /channels/:id/members (admin adds someone)
//       POST /channels/:id/join (user adds themselves)
//     For this phase, we only implement self-join.
func (h *MembershipHandler) Join(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return
	}

	userID := middleware.GetUserID(c)

	// Read the optional role from the body. Default to "member".
	var req joinChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Body is optional — if it's missing or malformed, just use default.
		req.Role = "member"
	}
	if req.Role == "" {
		req.Role = "member"
	}

	err = h.repo.AddMember(c.Request.Context(), channelID, userID, req.Role)
	if err != nil {
		h.logger.Error("failed to join channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to join channel"})
		return
	}

	// 204 No Content — success, no body to return.
	// Standard for POST/PUT/DELETE that doesn't return data.
	c.Status(http.StatusNoContent)
}

// Leave handles POST /v1/channels/:id/leave
func (h *MembershipHandler) Leave(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return
	}

	userID := middleware.GetUserID(c)

	err = h.repo.RemoveMember(c.Request.Context(), channelID, userID)
	if err != nil {
		h.logger.Error("failed to leave channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to leave channel"})
		return
	}

	c.Status(http.StatusNoContent)
}

// ListMembers handles GET /v1/channels/:id/members
func (h *MembershipHandler) ListMembers(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return
	}

	members, err := h.repo.ListMembers(c.Request.Context(), channelID)
	if err != nil {
		h.logger.Error("failed to list members", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list members"})
		return
	}

	c.JSON(http.StatusOK, members)
}
