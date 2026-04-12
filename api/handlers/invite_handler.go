package handlers

import (
	"context"
	"errors"
	"net/http"

	"weos/domain/entities"

	apimw "weos/api/middleware"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

type InviteHandler struct {
	inviteService *authapp.InviteService
	inviteRepo    authrepos.InviteRepository
	accountRepo   authrepos.AccountRepository
	logger        entities.Logger
}

type InviteHandlerConfig struct {
	InviteService *authapp.InviteService
	InviteRepo    authrepos.InviteRepository
	AccountRepo   authrepos.AccountRepository
	Logger        entities.Logger
}

func NewInviteHandler(cfg InviteHandlerConfig) *InviteHandler {
	return &InviteHandler{
		inviteService: cfg.InviteService,
		inviteRepo:    cfg.InviteRepo,
		accountRepo:   cfg.AccountRepo,
		logger:        cfg.Logger,
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

	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return respondError(c, http.StatusUnauthorized, "not authenticated")
	}

	accountID := identity.ActiveAccountID
	if accountID == "" {
		var acctErr error
		accountID, acctErr = h.defaultAccountID(ctx)
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
		h.logger.Error(ctx, "failed to create invite", "error", err)
		switch {
		case errors.Is(err, authapp.ErrNotAccountAdmin):
			return respondError(c, http.StatusForbidden, "admin role required for this account")
		default:
			return respondError(c, http.StatusInternalServerError, "failed to create invite")
		}
	}

	return respond(c, http.StatusCreated, InviteResponse{
		ID:        invite.GetID(),
		Email:     invite.Email(),
		Role:      invite.RoleID(),
		Status:    invite.Status(),
		Token:     token,
		ExpiresAt: invite.ExpiresAt().Format("2006-01-02T15:04:05Z"),
		CreatedAt: invite.CreatedAt().Format("2006-01-02T15:04:05Z"),
	})
}

// List handles GET /api/invites — lists all invites for the current account.
func (h *InviteHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	identity := auth.AgentFromCtx(ctx)
	accountID := ""
	if identity != nil {
		accountID = identity.ActiveAccountID
	}
	if accountID == "" {
		var acctErr error
		accountID, acctErr = h.defaultAccountID(ctx)
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
			ExpiresAt: inv.ExpiresAt().Format("2006-01-02T15:04:05Z"),
			CreatedAt: inv.CreatedAt().Format("2006-01-02T15:04:05Z"),
		})
	}

	return respond(c, http.StatusOK, result)
}

// Revoke handles DELETE /api/invites/:id — revokes a pending invite.
func (h *InviteHandler) Revoke(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return respondError(c, http.StatusUnauthorized, "not authenticated")
	}

	if err := h.inviteService.RevokeInvite(ctx, id, identity.AgentID); err != nil {
		h.logger.Error(ctx, "failed to revoke invite", "error", err, "invite_id", id)
		switch {
		case errors.Is(err, authapp.ErrInviteNotFound):
			return respondError(c, http.StatusNotFound, "invite not found")
		case errors.Is(err, authapp.ErrInviteNotPending):
			return respondError(c, http.StatusConflict, "invite is no longer pending")
		case errors.Is(err, authapp.ErrNotAccountAdmin):
			return respondError(c, http.StatusForbidden, "admin role required for this account")
		default:
			return respondError(c, http.StatusInternalServerError, "failed to revoke invite")
		}
	}

	return c.NoContent(http.StatusNoContent)
}

// Accept handles POST /api/invites/accept — accepts an invite using the token.
// This is a public endpoint (no auth required) — the token IS the authorization.
// The invitee provides their info so AcceptInvite can create the credential
// and verify the email matches the invite.
func (h *InviteHandler) Accept(c echo.Context) error {
	ctx := c.Request().Context()

	var req struct {
		Token string `json:"token"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := c.Bind(&req); err != nil || req.Token == "" {
		return respondError(c, http.StatusBadRequest, "token is required")
	}
	if req.Email == "" {
		return respondError(c, http.StatusBadRequest, "email is required")
	}

	userInfo := authapp.UserInfo{
		Provider:       "invite",
		ProviderUserID: req.Email,
		Email:          req.Email,
		DisplayName:    req.Name,
	}

	agent, credential, _, err := h.inviteService.AcceptInvite(ctx, req.Token, userInfo)
	if err != nil {
		h.logger.Error(ctx, "failed to accept invite", "error", err)
		switch {
		case errors.Is(err, authapp.ErrInviteTokenInvalid):
			return respondError(c, http.StatusBadRequest, "invite token is invalid")
		case errors.Is(err, authapp.ErrInviteExpired):
			return respondError(c, http.StatusBadRequest, "invite has expired")
		case errors.Is(err, authapp.ErrInviteNotPending):
			return respondError(c, http.StatusConflict, "invite is no longer pending")
		case errors.Is(err, authapp.ErrInviteEmailMismatch):
			return respondError(c, http.StatusBadRequest, "email does not match invite")
		case errors.Is(err, authapp.ErrInviteNotFound):
			return respondError(c, http.StatusNotFound, "invite not found")
		default:
			return respondError(c, http.StatusInternalServerError, "failed to accept invite")
		}
	}

	email := ""
	if credential != nil {
		email = credential.Email()
	}

	return respond(c, http.StatusOK, UserResponse{
		ID:    agent.GetID(),
		Name:  agent.Name(),
		Email: email,
	})
}

func (h *InviteHandler) defaultAccountID(ctx context.Context) (string, error) {
	accounts, err := h.accountRepo.FindAll(ctx, "", 1)
	if err != nil {
		return "", err
	}
	if len(accounts.Data) == 0 {
		return "", nil
	}
	return accounts.Data[0].GetID(), nil
}
