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
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/wepala/weos/v3/domain/entities"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/labstack/echo/v4"
)

// PasswordAuthHandlerConfig wires the dependencies used by the password
// register/login HTTP handlers. The same SessionManager and AuthService
// instances powering the OAuth flows are reused so password sessions and
// OAuth sessions share a single cookie and JWT lifecycle.
type PasswordAuthHandlerConfig struct {
	AuthService     authapp.AuthenticationService
	SessionManager  session.SessionManager
	SessionDuration time.Duration
	JWTCookieName   string
	JWTCookieMaxAge int
	Logger          entities.Logger
}

type PasswordAuthHandler struct {
	cfg PasswordAuthHandlerConfig
}

func NewPasswordAuthHandler(cfg PasswordAuthHandlerConfig) *PasswordAuthHandler {
	if cfg.SessionDuration == 0 {
		cfg.SessionDuration = 24 * time.Hour
	}
	if cfg.JWTCookieName == "" {
		cfg.JWTCookieName = "pericarp_token"
	}
	if cfg.JWTCookieMaxAge == 0 {
		cfg.JWTCookieMaxAge = 900
	}
	return &PasswordAuthHandler{cfg: cfg}
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authSuccessResponse struct {
	Agent     authAgentResponse    `json:"agent"`
	Account   *authAccountResponse `json:"account,omitempty"`
	Token     string               `json:"token,omitempty"`
	ExpiresAt time.Time            `json:"expires_at"`
}

type authAgentResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type authAccountResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Register handles POST /api/auth/register. Mirrors the success branch of
// pericarp's OAuth Callback: register → create auth session → set HTTP
// session cookie → issue JWT cookie.
func (h *PasswordAuthHandler) Register(c echo.Context) error {
	var req registerRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request body")
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || req.Password == "" {
		return respondError(c, http.StatusBadRequest, "email and password are required")
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		// Fall back to the local-part of the email so the agent has a name
		// even when the client doesn't supply one. RegisterPassword needs
		// a non-empty display name to build the personal-account name.
		if at := strings.Index(email, "@"); at > 0 {
			displayName = email[:at]
		} else {
			displayName = email
		}
	}

	ctx := c.Request().Context()
	agent, credential, account, err := h.cfg.AuthService.RegisterPassword(ctx, email, displayName, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, authapp.ErrEmailAlreadyTaken):
			return respondError(c, http.StatusConflict, "email already registered")
		case errors.Is(err, authapp.ErrPasswordSupportNotConfigured):
			h.cfg.Logger.Error(ctx, "password support not configured")
			return respondError(c, http.StatusServiceUnavailable, "password registration unavailable")
		default:
			h.cfg.Logger.Error(ctx, "register password failed", "error", err)
			return respondError(c, http.StatusInternalServerError, "failed to register account")
		}
	}

	return h.completeAuth(c, agent, credential, account, email)
}

// Login handles POST /api/auth/password-login. Issues the same session/JWT
// cookies as Register so a freshly-signed-up client and a returning client
// look identical to downstream middleware.
func (h *PasswordAuthHandler) Login(c echo.Context) error {
	var req loginRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request body")
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || req.Password == "" {
		return respondError(c, http.StatusBadRequest, "email and password are required")
	}

	ctx := c.Request().Context()
	agent, credential, account, err := h.cfg.AuthService.VerifyPassword(ctx, email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, authapp.ErrInvalidPassword):
			return respondError(c, http.StatusUnauthorized, "invalid email or password")
		case errors.Is(err, authapp.ErrPasswordSupportNotConfigured):
			h.cfg.Logger.Error(ctx, "password support not configured")
			return respondError(c, http.StatusServiceUnavailable, "password login unavailable")
		default:
			h.cfg.Logger.Error(ctx, "verify password failed", "error", err)
			return respondError(c, http.StatusInternalServerError, "failed to log in")
		}
	}

	return h.completeAuth(c, agent, credential, account, email)
}

func (h *PasswordAuthHandler) completeAuth(
	c echo.Context,
	agent *authentities.Agent,
	credential *authentities.Credential,
	account *authentities.Account,
	email string,
) error {
	ctx := c.Request().Context()
	r := c.Request()
	w := c.Response().Writer

	authSession, err := h.cfg.AuthService.CreateSession(
		ctx, agent.GetID(), credential.GetID(),
		clientIP(r), r.UserAgent(), h.cfg.SessionDuration,
	)
	if err != nil {
		h.cfg.Logger.Error(ctx, "password auth: session creation failed", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to create session")
	}

	sessionData := session.SessionData{
		SessionID: authSession.GetID(),
		AgentID:   agent.GetID(),
		CreatedAt: time.Now(),
		ExpiresAt: authSession.ExpiresAt(),
	}
	var accountResp *authAccountResponse
	activeAccountID := ""
	if account != nil {
		sessionData.AccountID = account.GetID()
		activeAccountID = account.GetID()
		accountResp = &authAccountResponse{ID: account.GetID(), Name: account.Name()}
	}
	if err := h.cfg.SessionManager.CreateHTTPSession(w, r, sessionData); err != nil {
		h.cfg.Logger.Error(ctx, "password auth: HTTP session creation failed", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to create session")
	}

	// IssueIdentityToken is best-effort; an outage here must not block login.
	tokenString, issueErr := h.cfg.AuthService.IssueIdentityToken(ctx, agent, activeAccountID)
	if issueErr != nil {
		h.cfg.Logger.Warn(ctx, "password auth: failed to issue identity token", "error", issueErr)
	} else if tokenString != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     h.cfg.JWTCookieName,
			Value:    tokenString,
			Path:     "/",
			MaxAge:   h.cfg.JWTCookieMaxAge,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	return respond(c, http.StatusOK, authSuccessResponse{
		Agent: authAgentResponse{
			ID:    agent.GetID(),
			Name:  agent.Name(),
			Email: email,
		},
		Account:   accountResp,
		Token:     tokenString,
		ExpiresAt: authSession.ExpiresAt(),
	})
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if comma := strings.Index(fwd, ","); comma >= 0 {
			return strings.TrimSpace(fwd[:comma])
		}
		return strings.TrimSpace(fwd)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
