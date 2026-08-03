package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lalith-99/echostream/internal/invite"
	"github.com/lalith-99/echostream/internal/middleware"
	"go.uber.org/zap"
)

// WorkspaceHandler handles workspace-level operations.
type WorkspaceHandler struct {
	inviteSvc *invite.Service
	baseURL   string
	logger    *zap.Logger
}

func NewWorkspaceHandler(inviteSvc *invite.Service, baseURL string, logger *zap.Logger) *WorkspaceHandler {
	return &WorkspaceHandler{inviteSvc: inviteSvc, baseURL: baseURL, logger: logger}
}

type inviteResponse struct {
	Token     string    `json:"token"`
	InviteURL string    `json:"invite_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GenerateInvite handles POST /v1/workspace/invite
//
// Returns a multi-use invite URL that lets anyone sign up directly into the
// caller's workspace (tenant). The token is stored in Redis with a 7-day TTL.
func (h *WorkspaceHandler) GenerateInvite(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	token, err := h.inviteSvc.Generate(c.Request.Context(), tenantID.String())
	if err != nil {
		h.logger.Error("failed to generate invite token", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate invite"})
		return
	}

	c.JSON(http.StatusCreated, inviteResponse{
		Token:     token,
		InviteURL: fmt.Sprintf("%s/signup?invite=%s", h.baseURL, token),
		ExpiresAt: time.Now().Add(invite.TokenTTL),
	})
}
