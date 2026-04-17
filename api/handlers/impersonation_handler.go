// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package handlers

import (
	"context"
	"net/http"

	apimw "github.com/wepala/weos/api/middleware"
	"github.com/wepala/weos/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

const impersonationMaxAge = 3600 // 1 hour

type ImpersonationHandler struct {
	store       sessions.Store
	accountRepo authrepos.AccountRepository
	agentRepo   authrepos.AgentRepository
	credRepo    authrepos.CredentialRepository
	logger      entities.Logger
}

type ImpersonationHandlerConfig struct {
	Store       sessions.Store
	AccountRepo authrepos.AccountRepository
	AgentRepo   authrepos.AgentRepository
	CredRepo    authrepos.CredentialRepository
	Logger      entities.Logger
}

func NewImpersonationHandler(cfg ImpersonationHandlerConfig) *ImpersonationHandler {
	return &ImpersonationHandler{
		store:       cfg.Store,
		accountRepo: cfg.AccountRepo,
		agentRepo:   cfg.AgentRepo,
		credRepo:    cfg.CredRepo,
		logger:      cfg.Logger,
	}
}

type startImpersonationRequest struct {
	AgentID string `json:"agent_id"`
}

// Start begins impersonation of another user. Only admins/owners may call this.
func (h *ImpersonationHandler) Start(c echo.Context) error {
	var req startImpersonationRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request")
	}
	if req.AgentID == "" {
		return respondError(c, http.StatusBadRequest, "agent_id is required")
	}

	ctx := c.Request().Context()
	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return respondError(c, http.StatusUnauthorized, "not authenticated")
	}

	// The impersonation middleware may have already swapped the identity.
	// Read the real admin ID from the impersonation cookie if one exists.
	adminAgentID := identity.AgentID
	sess, sessErr := h.store.Get(c.Request(), apimw.ImpersonationSessionName)
	if sessErr != nil {
		h.logger.Warn(ctx, "failed to read impersonation session", "error", sessErr)
	}
	if realID, ok := sess.Values[apimw.KeyRealAgentID].(string); ok && realID != "" {
		adminAgentID = realID
	}

	isAdmin, adminErr := apimw.IsAdmin(ctx, h.accountRepo)
	if adminErr != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", adminErr)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		// Re-check with the real admin identity if impersonation is active.
		if adminAgentID != identity.AgentID {
			origIdentity := &auth.Identity{
				AgentID:         adminAgentID,
				AccountIDs:      identity.AccountIDs,
				ActiveAccountID: identity.ActiveAccountID,
			}
			adminCtx := auth.ContextWithAgent(ctx, origIdentity)
			isAdmin, adminErr = apimw.IsAdmin(adminCtx, h.accountRepo)
			if adminErr != nil {
				h.logger.Error(ctx, "failed to re-check admin status", "error", adminErr)
				return respondError(c, http.StatusInternalServerError, "authorization check failed")
			}
			if !isAdmin {
				return respondError(c, http.StatusForbidden, "admin role required")
			}
		} else {
			return respondError(c, http.StatusForbidden, "admin role required")
		}
	}

	if req.AgentID == adminAgentID {
		return respondError(c, http.StatusBadRequest, "cannot impersonate yourself")
	}

	target, err := h.agentRepo.FindByID(ctx, req.AgentID)
	if err != nil || target == nil {
		return respondError(c, http.StatusNotFound, "user not found")
	}
	if target.Status() != "active" {
		return respondError(c, http.StatusBadRequest, "can only impersonate active users")
	}

	sess.Options = &sessions.Options{
		MaxAge:   impersonationMaxAge,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	sess.Values[apimw.KeyImpersonatedAgentID] = req.AgentID
	sess.Values[apimw.KeyRealAgentID] = adminAgentID
	sess.Values[apimw.KeyRealAccountID] = identity.ActiveAccountID

	if err := sess.Save(c.Request(), c.Response()); err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to create impersonation session")
	}

	h.logger.Info(ctx, "impersonation started",
		"admin_agent_id", adminAgentID,
		"target_agent_id", req.AgentID,
		"ip", c.RealIP(),
	)

	name, email := h.resolveAgentInfo(ctx, req.AgentID)
	return respond(c, http.StatusOK, map[string]any{
		"impersonating": map[string]string{
			"id":    req.AgentID,
			"name":  name,
			"email": email,
		},
	})
}

// Stop ends the current impersonation session.
func (h *ImpersonationHandler) Stop(c echo.Context) error {
	sess, err := h.store.Get(c.Request(), apimw.ImpersonationSessionName)
	if err != nil {
		return respond(c, http.StatusOK, map[string]string{"status": "ok"})
	}

	realAgentID, _ := sess.Values[apimw.KeyRealAgentID].(string)
	targetAgentID, _ := sess.Values[apimw.KeyImpersonatedAgentID].(string)

	sess.Options = &sessions.Options{
		MaxAge:   -1,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	for key := range sess.Values {
		delete(sess.Values, key)
	}
	if err := sess.Save(c.Request(), c.Response()); err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to clear impersonation session")
	}

	if realAgentID != "" {
		h.logger.Info(c.Request().Context(), "impersonation stopped",
			"admin_agent_id", realAgentID,
			"target_agent_id", targetAgentID,
			"ip", c.RealIP(),
		)
	}

	return respond(c, http.StatusOK, map[string]string{"status": "ok"})
}

// Status returns the current impersonation state.
func (h *ImpersonationHandler) Status(c echo.Context) error {
	sess, err := h.store.Get(c.Request(), apimw.ImpersonationSessionName)
	if err != nil {
		return respond(c, http.StatusOK, map[string]any{"active": false})
	}

	impersonatedAgentID, ok := sess.Values[apimw.KeyImpersonatedAgentID].(string)
	if !ok || impersonatedAgentID == "" {
		return respond(c, http.StatusOK, map[string]any{"active": false})
	}

	name, email := h.resolveAgentInfo(c.Request().Context(), impersonatedAgentID)
	return respond(c, http.StatusOK, map[string]any{
		"active": true,
		"user": map[string]string{
			"id":    impersonatedAgentID,
			"name":  name,
			"email": email,
		},
	})
}

// Me wraps pericarp's AuthHandlers.Me to return impersonated user info when active,
// and always includes the user's role.
func (h *ImpersonationHandler) Me(authHandlers *authhttp.AuthHandlers) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		sess, sessErr := h.store.Get(c.Request(), apimw.ImpersonationSessionName)
		if sessErr != nil {
			h.logger.Warn(ctx, "failed to read impersonation session in Me", "error", sessErr)
		}
		impersonatedAgentID, ok := sess.Values[apimw.KeyImpersonatedAgentID].(string)
		if !ok || impersonatedAgentID == "" {
			// No impersonation — delegate to pericarp's Me, then look up role.
			// Try to get agentID from the auth session cookie for role lookup.
			authSess, authSessErr := h.store.Get(c.Request(), "weos-session")
			if authSessErr != nil {
				h.logger.Warn(ctx, "failed to read auth session in Me", "error", authSessErr)
			}
			agentID, _ := authSess.Values["agent_id"].(string)
			accountID, _ := authSess.Values["account_id"].(string)
			if agentID == "" {
				authHandlers.Me(c.Response(), c.Request())
				return nil
			}
			name, email := h.resolveAgentInfo(ctx, agentID)
			role := ""
			if accountID != "" {
				role, _ = h.accountRepo.FindMemberRole(ctx, accountID, agentID)
			}
			return respond(c, http.StatusOK, map[string]any{
				"id":    agentID,
				"name":  name,
				"email": email,
				"role":  role,
			})
		}

		realAgentID, _ := sess.Values[apimw.KeyRealAgentID].(string)
		name, email := h.resolveAgentInfo(ctx, impersonatedAgentID)
		realName, _ := h.resolveAgentInfo(ctx, realAgentID)
		// Look up the impersonated user's account to resolve their role.
		role := ""
		accounts, _ := h.accountRepo.FindByMember(ctx, impersonatedAgentID)
		if len(accounts) > 0 {
			role, _ = h.accountRepo.FindMemberRole(ctx, accounts[0].GetID(), impersonatedAgentID)
		}

		return respond(c, http.StatusOK, map[string]any{
			"id":            impersonatedAgentID,
			"name":          name,
			"email":         email,
			"role":          role,
			"impersonating": true,
			"real_user": map[string]string{
				"id":   realAgentID,
				"name": realName,
			},
		})
	}
}

func (h *ImpersonationHandler) resolveAgentInfo(ctx context.Context, agentID string) (string, string) {
	if agentID == "" {
		return "", ""
	}
	agent, err := h.agentRepo.FindByID(ctx, agentID)
	if err != nil {
		h.logger.Warn(ctx, "failed to find agent for impersonation info", "agent_id", agentID, "error", err)
		return "", ""
	}
	if agent == nil {
		return "", ""
	}
	name := agent.Name()
	email := ""
	creds, credErr := h.credRepo.FindByAgent(ctx, agentID)
	if credErr != nil {
		h.logger.Warn(ctx, "failed to load credentials for agent", "agent_id", agentID, "error", credErr)
	}
	if len(creds) > 0 {
		email = creds[0].Email()
		if name == "" {
			name = creds[0].DisplayName()
		}
	}
	return name, email
}
