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
	"errors"
	"net/http"

	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

const impersonationMaxAge = 3600 // 1 hour

type ImpersonationHandler struct {
	store          sessions.Store
	accountRepo    authrepos.AccountRepository
	agentRepo      authrepos.AgentRepository
	credRepo       authrepos.CredentialRepository
	members        repositories.AccountMemberQuery
	sessionManager session.SessionManager
	authService    authapp.AuthenticationService
	locks          repositories.AccountErasureLocks
	logger         entities.Logger
}

type ImpersonationHandlerConfig struct {
	Store       sessions.Store
	AccountRepo authrepos.AccountRepository
	AgentRepo   authrepos.AgentRepository
	CredRepo    authrepos.CredentialRepository
	// Members lets the identity read report how many people share the
	// account the caller acts in, so an app can say so before one of them
	// deletes it. Optional: without it the count is omitted.
	Members repositories.AccountMemberQuery
	// SessionManager and AuthService let the identity read validate the
	// session its cookie names before it answers from it, so a session that
	// no longer serves — its account locked for deletion, or gone — is
	// refused with the code every other route answers. ErasureLocks tells
	// the lock from a suspension. Without them the read answers from the
	// cookie alone, as it did before.
	SessionManager session.SessionManager
	AuthService    authapp.AuthenticationService
	ErasureLocks   repositories.AccountErasureLocks
	Logger         entities.Logger
}

func NewImpersonationHandler(cfg ImpersonationHandlerConfig) *ImpersonationHandler {
	return &ImpersonationHandler{
		store:          cfg.Store,
		accountRepo:    cfg.AccountRepo,
		agentRepo:      cfg.AgentRepo,
		credRepo:       cfg.CredRepo,
		members:        cfg.Members,
		sessionManager: cfg.SessionManager,
		authService:    cfg.AuthService,
		locks:          cfg.ErasureLocks,
		logger:         cfg.Logger,
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
//
// It is mounted outside the protected group, so no middleware has checked the
// session before it runs. It validates the session itself before answering
// anything from the cookie (wm-ccg4f): this is the route an app reads before
// it offers the deletion, and a cookie for an account locked for deletion —
// or a second device's cookie for an account already gone — must get the
// refusal every other route gives, not a 200 that says nothing is wrong.
//
// A request that carries a bearer token is answered for the token's person
// (wm-hg3xf). An app in a native shell holds no cookie for the instance, only
// the token its sign-in handed back. The token is checked by
// apimw.BearerWhenPresent, mounted in front of this route, and that middleware
// is the only thing that puts an identity in this route's context. The answer
// then reads no cookie: a token wins over a session cookie beside it.
//
// On the bearer path Me ignores the impersonation cookie on purpose
// (wm-fqjc2). This does NOT match the MCP group, where apimw.Impersonation
// runs after BearerOrSession (internal/cli/serve.go), so there a token for the
// admin who started an impersonation acts as the person impersonated. Here a
// token is always answered for its own person. Do not "restore a match" with
// the MCP group: a cookie beside a token must not change whom the token
// answers for. TestMe_ABearerTokenIsAnsweredForItsOwnPersonBesideAnImpersonationCookie
// pins this.
func (h *ImpersonationHandler) Me(authHandlers *authhttp.AuthHandlers) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		if identity := auth.AgentFromCtx(ctx); identity != nil {
			return h.answerToken(c, identity)
		}
		info, refused := h.validatedSession(c)
		if refused {
			return nil
		}
		sess, sessErr := h.store.Get(c.Request(), apimw.ImpersonationSessionName)
		if sessErr != nil {
			h.logger.Warn(ctx, "failed to read impersonation session in Me", "error", sessErr)
		}
		impersonatedAgentID, ok := sess.Values[apimw.KeyImpersonatedAgentID].(string)
		if !ok || impersonatedAgentID == "" {
			// No impersonation — delegate to pericarp's Me, then look up role.
			// The validated session says who this is; the cookie's own values
			// are the fallback when nothing validates sessions here.
			authSess, authSessErr := h.store.Get(c.Request(), "weos-session")
			if authSessErr != nil {
				h.logger.Warn(ctx, "failed to read auth session in Me", "error", authSessErr)
			}
			agentID, _ := authSess.Values["agent_id"].(string)
			accountID, _ := authSess.Values["account_id"].(string)
			if info != nil {
				agentID, accountID = info.AgentID, info.AccountID
			}
			if agentID == "" {
				authHandlers.Me(c.Response(), c.Request())
				return nil
			}
			return respond(c, http.StatusOK, h.identityBody(ctx, agentID, accountID, h.roleOrNone(ctx, accountID, agentID)))
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

// validatedSession checks the session the cookie names, the way the
// protected group's middleware would. It reports refused=true after writing
// the 401, with the code the other routes use: account_access_revoked,
// account_erasure_pending, account_deactivated, or none. A request with no
// cookie, or a handler wired without a session manager and auth service,
// gets info=nil and is answered from the cookie as before.
func (h *ImpersonationHandler) validatedSession(c echo.Context) (info *authapp.SessionInfo, refused bool) {
	if h.sessionManager == nil || h.authService == nil {
		return nil, false
	}
	ctx := c.Request().Context()
	data, err := h.sessionManager.GetHTTPSession(c.Request())
	if err != nil || data == nil || data.SessionID == "" {
		return nil, false
	}
	info, err = h.authService.ValidateSession(ctx, data.SessionID)
	if err == nil {
		return info, false
	}
	code := ""
	switch {
	case errors.Is(err, authapp.ErrSessionAccountRevoked):
		code = apimw.CodeAccountAccessRevoked
	case errors.Is(err, authapp.ErrSessionAccountDeactivated):
		code = apimw.CodeAccountDeactivated
		if h.locks != nil {
			locked, lockErr := h.locks.IsLocked(ctx, data.AccountID)
			if lockErr != nil {
				h.logger.Error(ctx, "could not read the erasure lock", "account_id", data.AccountID, "error", lockErr)
				_ = respondError(c, http.StatusServiceUnavailable, "could not read the account's state")
				return nil, true
			}
			if locked {
				code = apimw.CodeAccountErasurePending
			}
		}
	}
	if code == "" {
		_ = respondError(c, http.StatusUnauthorized, "not authenticated")
	} else {
		_ = respondErrorCode(c, http.StatusUnauthorized, "not authenticated", code)
	}
	return nil, true
}

// answerToken is the identity read's answer for a bearer token's person.
// BearerWhenPresent has checked the token and the state of the account it
// names. The session path's ValidateSession also checks that the person still
// belongs to that account, and refuses a session that names no account, so
// this does the same with the same codes and body (wm-qqoq2): a person removed
// from a household must not still see themselves in it on their phone.
func (h *ImpersonationHandler) answerToken(c echo.Context, identity *auth.Identity) error {
	ctx := c.Request().Context()
	if identity.ActiveAccountID == "" {
		return respondErrorCode(c, http.StatusUnauthorized, "not authenticated", apimw.CodeUnscopedSession)
	}
	role, err := h.accountRepo.FindMemberRole(ctx, identity.ActiveAccountID, identity.AgentID)
	if err != nil {
		// Fail closed: the membership could not be read, so the token is not
		// known to be good. The bearer middleware answers an unreadable
		// account state the same way.
		h.logger.Error(ctx, "could not read the member's role for a bearer token",
			"account_id", identity.ActiveAccountID, "agent_id", identity.AgentID, "error", err)
		return respondError(c, http.StatusServiceUnavailable, "could not read the account's state")
	}
	if role == "" {
		return respondErrorCode(c, http.StatusUnauthorized, "not authenticated", apimw.CodeAccountAccessRevoked)
	}
	return respond(c, http.StatusOK, h.identityBody(ctx, identity.AgentID, identity.ActiveAccountID, role))
}

// roleOrNone reads agentID's role in accountID for the session path. An
// unreadable role is answered empty, which withholds what a role would grant
// rather than granting it; the session itself was already validated.
func (h *ImpersonationHandler) roleOrNone(ctx context.Context, accountID, agentID string) string {
	if accountID == "" {
		return ""
	}
	role, err := h.accountRepo.FindMemberRole(ctx, accountID, agentID)
	if err != nil {
		h.logger.Warn(ctx, "could not read the member's role", "account_id", accountID, "agent_id", agentID, "error", err)
		return ""
	}
	return role
}

// identityBody is the identity read's answer for agentID acting in accountID
// with role. A session and a bearer token both answer through it, so the two
// bodies cannot drift apart.
func (h *ImpersonationHandler) identityBody(ctx context.Context, agentID, accountID, role string) map[string]any {
	name, email := h.resolveAgentInfo(ctx, agentID)
	body := map[string]any{
		"id":    agentID,
		"name":  name,
		"email": email,
		"role":  role,
	}
	if accountID != "" {
		body["account_id"] = accountID
		if count, ok := h.memberCount(ctx, accountID); ok {
			body["member_count"] = count
		}
	}
	return body
}

// memberCount reports how many people share accountID, when a member query
// is wired. A count that cannot be read is omitted rather than reported as
// zero: an app that shows "1 person" to a person who is not alone has been
// told something false.
func (h *ImpersonationHandler) memberCount(ctx context.Context, accountID string) (int, bool) {
	if h.members == nil {
		return 0, false
	}
	count, err := h.members.CountMembers(ctx, accountID)
	if err != nil {
		h.logger.Warn(ctx, "could not count the account's members", "account_id", accountID, "error", err)
		return 0, false
	}
	return count, true
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
