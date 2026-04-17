package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/wepala/weos/domain/entities"

	apimw "github.com/wepala/weos/api/middleware"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

type InviteHandler struct {
	inviteService  *authapp.InviteService
	inviteRepo     authrepos.InviteRepository
	accountRepo    authrepos.AccountRepository
	credentialRepo authrepos.CredentialRepository
	logger         entities.Logger
}

type InviteHandlerConfig struct {
	InviteService  *authapp.InviteService
	InviteRepo     authrepos.InviteRepository
	AccountRepo    authrepos.AccountRepository
	CredentialRepo authrepos.CredentialRepository
	Logger         entities.Logger
}

func NewInviteHandler(cfg InviteHandlerConfig) *InviteHandler {
	return &InviteHandler{
		inviteService:  cfg.InviteService,
		inviteRepo:     cfg.InviteRepo,
		accountRepo:    cfg.AccountRepo,
		credentialRepo: cfg.CredentialRepo,
		logger:         cfg.Logger,
	}
}

type CreateInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type InviteResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	Token     string `json:"token,omitempty"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

// Create handles POST /api/invites — creates an invite and returns it with a token.
func (h *InviteHandler) Create(c echo.Context) error {
	ctx := c.Request().Context()

	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return respondError(c, http.StatusUnauthorized, "not authenticated")
	}

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	var req CreateInviteRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request")
	}
	if req.Email == "" {
		return respondError(c, http.StatusBadRequest, "email is required")
	}
	if req.Role == "" {
		return respondError(c, http.StatusBadRequest, "role is required")
	}

	accountID := identity.ActiveAccountID
	if accountID == "" {
		var acctErr error
		accountID, acctErr = h.agentAccountID(ctx, identity.AgentID)
		if acctErr != nil {
			h.logger.Error(ctx, "failed to resolve account", "error", acctErr)
			return respondError(c, http.StatusInternalServerError, "failed to resolve account")
		}
	}
	if accountID == "" {
		return respondError(c, http.StatusInternalServerError, "no account found")
	}

	invite, token, err := h.inviteService.CreateInvite(ctx, accountID, req.Email, req.Role, identity.AgentID)
	if err != nil {
		switch {
		case errors.Is(err, authapp.ErrNotAccountAdmin):
			h.logger.Warn(ctx, "invite create forbidden", "error", err)
			return respondError(c, http.StatusForbidden, "admin role required for this account")
		default:
			h.logger.Error(ctx, "failed to create invite", "error", err)
			return respondError(c, http.StatusInternalServerError, "failed to create invite")
		}
	}

	return respond(c, http.StatusCreated, InviteResponse{
		ID:        invite.GetID(),
		Email:     invite.Email(),
		Role:      invite.RoleID(),
		Status:    invite.Status(),
		Token:     token,
		ExpiresAt: invite.ExpiresAt().UTC().Format(time.RFC3339),
		CreatedAt: invite.CreatedAt().UTC().Format(time.RFC3339),
	})
}

// List handles GET /api/invites — lists all invites for the current account.
func (h *InviteHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return respondError(c, http.StatusUnauthorized, "not authenticated")
	}

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	accountID := identity.ActiveAccountID
	if accountID == "" {
		var acctErr error
		accountID, acctErr = h.agentAccountID(ctx, identity.AgentID)
		if acctErr != nil {
			h.logger.Error(ctx, "failed to resolve account", "error", acctErr)
			return respondError(c, http.StatusInternalServerError, "failed to resolve account")
		}
	}
	if accountID == "" {
		return respond(c, http.StatusOK, []InviteResponse{})
	}

	invites, err := h.inviteRepo.FindByAccount(ctx, accountID)
	if err != nil {
		h.logger.Error(ctx, "failed to list invites", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to list invites")
	}

	result := make([]InviteResponse, 0, len(invites))
	for _, inv := range invites {
		result = append(result, InviteResponse{
			ID:        inv.GetID(),
			Email:     inv.Email(),
			Role:      inv.RoleID(),
			Status:    inv.Status(),
			ExpiresAt: inv.ExpiresAt().UTC().Format(time.RFC3339),
			CreatedAt: inv.CreatedAt().UTC().Format(time.RFC3339),
		})
	}

	return respond(c, http.StatusOK, result)
}

// Revoke handles DELETE /api/invites/:id — revokes a pending invite.
func (h *InviteHandler) Revoke(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return respondError(c, http.StatusUnauthorized, "not authenticated")
	}

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	if err := h.inviteService.RevokeInvite(ctx, id, identity.AgentID); err != nil {
		switch {
		case errors.Is(err, authapp.ErrInviteNotFound):
			h.logger.Warn(ctx, "invite not found for revoke", "invite_id", id)
			return respondError(c, http.StatusNotFound, "invite not found")
		case errors.Is(err, authapp.ErrInviteNotPending):
			h.logger.Warn(ctx, "invite not pending for revoke", "invite_id", id)
			return respondError(c, http.StatusConflict, "invite is no longer pending")
		case errors.Is(err, authapp.ErrNotAccountAdmin):
			h.logger.Warn(ctx, "revoke forbidden", "error", err, "invite_id", id)
			return respondError(c, http.StatusForbidden, "admin role required for this account")
		default:
			h.logger.Error(ctx, "failed to revoke invite", "error", err, "invite_id", id)
			return respondError(c, http.StatusInternalServerError, "failed to revoke invite")
		}
	}

	return c.NoContent(http.StatusNoContent)
}

// Accept handles POST /api/invites/accept — accepts an invite using the token.
// This is a public endpoint (no auth required) — the token IS the authorization.
// When the caller is authenticated (post-OAuth), the email is derived from
// the session identity rather than trusting the request body.
func (h *InviteHandler) Accept(c echo.Context) error {
	ctx := c.Request().Context()

	var req struct {
		Token string `json:"token"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request")
	}
	if req.Token == "" {
		return respondError(c, http.StatusBadRequest, "token is required")
	}

	// When authenticated, derive email from the session to prevent spoofing.
	// Fail closed: if authenticated but email cannot be determined, reject.
	email := req.Email
	name := req.Name
	if identity := auth.AgentFromCtx(ctx); identity != nil {
		if h.credentialRepo == nil {
			h.logger.Error(ctx, "credential repository unavailable for authenticated accept")
			return respondError(c, http.StatusInternalServerError, "failed to accept invite")
		}
		creds, credErr := h.credentialRepo.FindByAgent(ctx, identity.AgentID)
		if credErr != nil {
			h.logger.Error(ctx, "failed to derive authenticated email", "error", credErr, "agent_id", identity.AgentID)
			return respondError(c, http.StatusInternalServerError, "failed to accept invite")
		}
		if len(creds) == 0 || creds[0].Email() == "" {
			return respondError(c, http.StatusUnauthorized, "authenticated email could not be determined")
		}
		email = creds[0].Email()
		if req.Email != "" && req.Email != email {
			return respondError(c, http.StatusBadRequest, "email does not match authenticated user")
		}
	}

	if email == "" {
		return respondError(c, http.StatusBadRequest, "email is required")
	}

	userInfo := authapp.UserInfo{
		Provider:       "invite",
		ProviderUserID: email,
		Email:          email,
		DisplayName:    name,
	}

	agent, credential, _, err := h.inviteService.AcceptInvite(ctx, req.Token, userInfo)
	if err != nil {
		switch {
		case errors.Is(err, authapp.ErrInviteTokenInvalid):
			h.logger.Warn(ctx, "invalid invite token", "error", err)
			return respondError(c, http.StatusBadRequest, "invite token is invalid")
		case errors.Is(err, authapp.ErrInviteExpired):
			h.logger.Warn(ctx, "expired invite token", "error", err)
			return respondError(c, http.StatusBadRequest, "invite has expired")
		case errors.Is(err, authapp.ErrInviteNotPending):
			h.logger.Warn(ctx, "invite not pending", "error", err)
			return respondError(c, http.StatusConflict, "invite is no longer pending")
		case errors.Is(err, authapp.ErrInviteEmailMismatch):
			h.logger.Warn(ctx, "invite email mismatch", "error", err)
			return respondError(c, http.StatusBadRequest, "email does not match invite")
		case errors.Is(err, authapp.ErrInviteNotFound):
			h.logger.Warn(ctx, "invite not found", "error", err)
			return respondError(c, http.StatusNotFound, "invite not found")
		default:
			h.logger.Error(ctx, "failed to accept invite", "error", err)
			return respondError(c, http.StatusInternalServerError, "failed to accept invite")
		}
	}

	credEmail := email
	if credential != nil && credential.Email() != "" {
		credEmail = credential.Email()
	}

	return respond(c, http.StatusOK, UserResponse{
		ID:     agent.GetID(),
		Name:   agent.Name(),
		Email:  credEmail,
		Status: agent.Status(),
	})
}

func (h *InviteHandler) agentAccountID(ctx context.Context, agentID string) (string, error) {
	accounts, err := h.accountRepo.FindByMember(ctx, agentID)
	if err != nil {
		return "", err
	}
	if len(accounts) == 0 {
		return "", nil
	}
	return accounts[0].GetID(), nil
}
