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

// PasswordAuthHandlerConfig wires dependencies for the password
// register/login endpoints. SessionManager and AuthService are shared
// with the OAuth flow so a password-authed and an OAuth-authed request
// are indistinguishable to downstream middleware.
type PasswordAuthHandlerConfig struct {
	AuthService     authapp.AuthenticationService
	SessionManager  session.SessionManager
	SessionDuration time.Duration
	JWTCookieName   string
	JWTCookieMaxAge int
	// SecureCookies controls the Secure flag on the JWT cookie. Must match
	// the session manager's setting (false in plain-HTTP local dev, true
	// otherwise) — a mismatch means the browser drops one of the two
	// cookies and auth never sticks.
	SecureCookies bool
	Logger        entities.Logger
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
	// Keep the JWT cookie alive for the full session window so the browser
	// keeps sending it during that period. Whether an expired JWT can fall
	// back to a valid gorilla session is a property of the auth middleware
	// on each route, not of this cookie's max age.
	if cfg.JWTCookieMaxAge == 0 {
		cfg.JWTCookieMaxAge = int(cfg.SessionDuration.Seconds())
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
		// RegisterPassword requires a non-empty display name (it's used to
		// build the personal-account name); the email's local-part is the
		// least surprising default.
		if local, _, ok := strings.Cut(email, "@"); ok && local != "" {
			displayName = local
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

// Logout clears both the gorilla session cookie (delegated to pericarp's
// Logout handler) and the JWT cookie set by completeAuth. The JWT cookie
// is currently informational on the server side (no middleware reads it)
// but a SPA may attach it as a Bearer token, so an endpoint that only
// invalidates the session would leave a still-presentable JWT.
func (h *PasswordAuthHandler) Logout(c echo.Context, oauthLogout http.HandlerFunc) error {
	w := c.Response().Writer
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.JWTCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	oauthLogout(w, c.Request())
	return nil
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
			Secure:   h.cfg.SecureCookies,
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
		first, _, _ := strings.Cut(fwd, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
