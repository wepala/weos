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
	"strings"
	"time"

	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
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
	// AccountRepo and ErasureLocks let a sign-in that resolved no active
	// account look for one whose erasure is unfinished. Both optional: with
	// either missing, such a sign-in stays unscoped, as it always was.
	AccountRepo  authrepos.AccountRepository
	ErasureLocks repositories.AccountErasureLocks
	// RefreshTokens and AgentRepo let an app in a native shell keep its session
	// past the access token's hour (wm-lnimb). With both set, and AccountRepo
	// and ErasureLocks, a sign-in that hands back a token hands back a refresh
	// token beside it, and Refresh renews with it. Optional: without them a
	// sign-in answers as it always did.
	RefreshTokens weosoauth.RefreshTokenRepository
	AgentRepo     authrepos.AgentRepository
	// JWTService reads the bearer token a native app signs out with, so the
	// sign-out can end that person's refresh tokens. Optional.
	JWTService authapp.JWTService
	// RefreshSuccessorKey derives a native refresh token's successor, so a
	// renewal repeated inside the grace window gets the same one (wm-3dgs0).
	// Optional: it defaults to weosoauth.NativeRefreshSuccessorKey(JWTService).
	RefreshSuccessorKey []byte
}

type PasswordAuthHandler struct {
	cfg PasswordAuthHandlerConfig
	// purger removes native refresh token rows past the purge horizon when a
	// native refresh token is written (wm-sa7wv). Nil without RefreshTokens.
	purger *weosoauth.NativeRefreshTokenPurger
}

// purgeNativeRefreshTokens runs the native refresh token purge when it is due.
// It never fails the request that runs it.
func (h *PasswordAuthHandler) purgeNativeRefreshTokens(ctx context.Context) {
	ran, purged, err := h.purger.MaybePurge(ctx)
	if err != nil {
		h.cfg.Logger.Warn(ctx, "native refresh tokens: purge failed; it runs again after the interval", "error", err)
		return
	}
	if ran && purged > 0 {
		h.cfg.Logger.Info(ctx, "native refresh tokens: purged rows past the horizon", "rows", purged)
	}
}

func NewPasswordAuthHandler(cfg PasswordAuthHandlerConfig) *PasswordAuthHandler {
	if len(cfg.RefreshSuccessorKey) == 0 {
		cfg.RefreshSuccessorKey = weosoauth.NativeRefreshSuccessorKey(cfg.JWTService)
	}
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
	h := &PasswordAuthHandler{cfg: cfg}
	if cfg.RefreshTokens != nil {
		h.purger = weosoauth.NewNativeRefreshTokenPurger(cfg.RefreshTokens, nil)
	}
	return h
}

// DefaultDisplayName picks the name to register an account under. Registration
// requires a non-empty display name because the personal account is named after
// it, and the email's local part is the least surprising stand-in when the
// caller didn't give one.
//
// The CLI and the HTTP route share this so an account minted either way is the
// same thing afterwards — which is the whole point of having both.
func DefaultDisplayName(email, given string) string {
	if displayName := strings.TrimSpace(given); displayName != "" {
		return displayName
	}
	if local, _, ok := strings.Cut(email, "@"); ok && local != "" {
		return local
	}
	return email
}

// PasswordAuthRoutes describes which of the two password endpoints an
// instance offers. They are independent on purpose: an instance can accept
// sign-ins from accounts it already has (SignIn) without letting anyone who
// reaches the hostname create another (Registration).
type PasswordAuthRoutes struct {
	// SignIn mounts POST /auth/password-login.
	SignIn bool
	// Registration mounts POST /auth/register. Ignored unless SignIn is
	// also set — registering an account that could never sign in is not a
	// configuration worth honoring.
	Registration bool
}

// MountPasswordAuth registers the password endpoints an instance has turned
// on, and *only* those. An endpoint that is off is never registered, so its
// path answers exactly like a path the server has never had: 404, no
// authentication challenge, no Allow header, nothing to probe and no
// allowlist to keep correct. This is deliberately not a handler that
// inspects the request and refuses it.
//
// serve.go and the acceptance tests both mount through here so there is one
// copy of this decision rather than two that can drift apart.
func MountPasswordAuth(g *echo.Group, h *PasswordAuthHandler, routes PasswordAuthRoutes) {
	if !routes.SignIn {
		return
	}
	if routes.Registration {
		g.POST("/auth/register", h.Register)
	}
	g.POST("/auth/password-login", h.Login)
}

// NativeSession is the value of a sign-in request's "session" field that asks
// for a native session (wm-nybvk). An app in a native shell holds no cookie, so
// only it gets a refresh token beside its token. Any other value, or none, is a
// browser's sign-in, which answers as it always did: a browser renews through
// its cookie session and must not hold a long-lived credential page script can
// read.
const NativeSession = "native"

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Session     string `json:"session"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Session  string `json:"session"`
}

type authSuccessResponse struct {
	Agent     authAgentResponse    `json:"agent"`
	Account   *authAccountResponse `json:"account,omitempty"`
	Token     string               `json:"token,omitempty"`
	ExpiresAt time.Time            `json:"expires_at"`
	// TokenExpiresAt is when Token stops being accepted, an hour after the
	// sign-in, so an app knows when to renew. ExpiresAt is the browser
	// session's, which an app in a native shell does not hold. Present only
	// beside RefreshToken.
	TokenExpiresAt time.Time `json:"token_expires_at,omitzero"`
	// RefreshToken renews Token at POST /auth/refresh before it expires, and
	// RefreshTokenExpiresAt is when it stops renewing (wm-lnimb). Present
	// whenever Token is, on a sign-in that asked for a native session, on an
	// instance that can renew. Any other sign-in answers with exactly the fields
	// it always did (wm-nybvk).
	RefreshToken          string    `json:"refresh_token,omitempty"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at,omitzero"`
	// ErasurePending says the session was scoped to an account whose
	// deletion began and did not finish. The session serves exactly one
	// request, DELETE /api/account, and the app should offer that.
	ErasurePending bool   `json:"erasure_pending,omitempty"`
	Code           string `json:"code,omitempty"`
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
	displayName := DefaultDisplayName(email, req.DisplayName)

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

	return h.completeAuth(c, agent, credential, account, email, req.Session == NativeSession)
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

	return h.completeAuth(c, agent, credential, account, email, req.Session == NativeSession)
}

// Logout clears both the gorilla session cookie (delegated to pericarp's
// Logout handler) and the JWT cookie set by completeAuth. The JWT cookie
// is currently informational on the server side (no middleware reads it)
// but a SPA may attach it as a Bearer token, so an endpoint that only
// invalidates the session would leave a still-presentable JWT.
//
// An app in a native shell signs out with the token it holds, or with
// {"refresh_token":"..."} in the body. Either one ends that device's session —
// its refresh token family — and no other; {"everywhere":true} with a live
// credential ends every native session of the person (wm-lnimb, wm-utb5c; see
// endNativeSessions). The access token itself is stateless and lasts out its
// hour. The cookie and the browser session are cleared whether or not the app's
// session could be ended: a store that cannot be read or written is answered
// 200 with app_session not_ended and code app_session_not_ended, so the app
// keeps its refresh token and signs out again (wm-5rziu).
//
// A sign-out that presents a bearer token, a refresh token or "everywhere"
// says in its answer what it did to the app's sessions: app_session is ended,
// ended_everywhere or not_identified, and code says what it did not do
// (wm-ehtnq). A cookie-only sign-out answers exactly as it always did.
func (h *PasswordAuthHandler) Logout(c echo.Context, oauthLogout http.HandlerFunc) error {
	did, err := h.endNativeSessions(c)
	if err != nil {
		// The browser's sign-out never waits on the refresh token store: the
		// cookie and the browser session are cleared below whatever happened
		// here, and the answer tells the app its session was not ended, so it
		// keeps its refresh token and signs out again (wm-5rziu).
		h.cfg.Logger.Error(c.Request().Context(),
			"sign-out: could not end the app's native session; the browser session is ended and the app is told to sign out again",
			"error", err)
		did = nativeSignOut{AppSession: appSessionNotEnded, Code: CodeAppSessionNotEnded}
	}
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
	if did.AppSession == "" {
		oauthLogout(w, c.Request())
		return nil
	}
	recorded := &signOutRecorder{header: w.Header()}
	oauthLogout(recorded, c.Request())
	return answerNativeSignOut(c, recorded, did)
}

func (h *PasswordAuthHandler) completeAuth(
	c echo.Context,
	agent *authentities.Agent,
	credential *authentities.Credential,
	account *authentities.Account,
	email string,
	native bool,
) error {
	return h.completeAuthAs(c, agent, credential, account, email, native,
		func(r authSuccessResponse) any { return r })
}

// completeAuthAs completes a sign-in exactly as completeAuth does — the same
// session, the same cookies, the same fields — and lets the caller add to the
// answer. shape receives the password sign-in's answer and returns what is
// sent. A sign-in path that answers in this shape plus a field of its own
// uses it, so the shared part cannot drift from password sign-in's. native says
// the request asked for a native session (see NativeSession).
func (h *PasswordAuthHandler) completeAuthAs(
	c echo.Context,
	agent *authentities.Agent,
	credential *authentities.Credential,
	account *authentities.Account,
	email string,
	native bool,
	shape func(authSuccessResponse) any,
) error {
	ctx := c.Request().Context()
	r := c.Request()
	w := c.Response().Writer

	// The account this sign-in resolved is the only thing that knows which
	// account the session acts in, and it is resolved exactly once, here.
	// Nothing downstream can recover it: a lookup would have to guess, and it
	// would guess wrong for anyone who arrived by invite and has no personal
	// account of their own. So it is computed before the session is created
	// and reused for every account-shaped field below.
	accountID := ""
	if account != nil {
		accountID = account.GetID()
	}

	// A sign-in that resolved no active account may still belong to an
	// account whose erasure began and did not finish. Sign-in resolution
	// passes over an inactive account, so the person would otherwise be
	// stranded: locked out, with the operator's command line the only way to
	// finish what they started. The session is scoped to that account for
	// the one purpose of running the deletion again; every other route
	// refuses it with the code that says why (wm-421nl).
	erasurePending := false
	if account == nil {
		if locked := h.lockedAccountFor(ctx, agent.GetID()); locked != nil {
			account = locked
			accountID = locked.GetID()
			erasurePending = true
		}
	}

	// AccountAlreadyVerified is safe here and only here: the account came back
	// from VerifyPassword or FindOrCreateAgent in this same request, so the
	// membership is established and the account is active by construction —
	// or it is the erasure-locked account found just above, whose membership
	// and role were read from the store in this same request and whose
	// inactivity is the point. It also skips a re-read that can lose a race —
	// a first-time signup writes the membership moments earlier, and a replica
	// may not show it yet. Never pass this option for an account that arrived
	// from a client; it disables the membership and deactivation checks.
	authSession, err := h.cfg.AuthService.CreateSession(
		ctx, agent.GetID(), accountID, credential.GetID(),
		c.RealIP(), r.UserAgent(), h.cfg.SessionDuration,
		authapp.AccountAlreadyVerified(),
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
	if account != nil {
		sessionData.AccountID = accountID
		accountResp = &authAccountResponse{ID: accountID, Name: account.Name()}
	}
	if err := h.cfg.SessionManager.CreateHTTPSession(w, r, sessionData); err != nil {
		h.cfg.Logger.Error(ctx, "password auth: HTTP session creation failed", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to create session")
	}

	// IssueIdentityToken is best-effort; an outage here must not block login.
	// On error, drop the token entirely so the client doesn't see a JWT in
	// the response body that has no matching cookie — that mismatch makes
	// "logged in but no cookie" states harder to reason about.
	//
	// A sign-in that resolved no account gets no token at all. Issuing one
	// would mint a credential naming no account, and the session middleware
	// refuses exactly that shape with unscoped_session — so the token could
	// only ever be useful on a path that does not make the same check, which
	// is the hole rather than the feature.
	//
	// A locked account's owner or admin — the only person erasurePending is
	// set for — does get a token, scoped to the locked account (wm-xsvas). An
	// app in a native shell holds no cookie, only a token, so without one it
	// could never finish a deletion that failed part-way. The token serves
	// that and nothing else: the bearer path refuses it with
	// account_erasure_pending on every route except DELETE /api/account, which
	// admits it as it admits a session scoped to the locked account.
	//
	// A native session's id is chosen before its token is issued, so the token
	// names the session (weosoauth.NativeSessionClaim) and a sign-out with only
	// the token can end that session and no other (wm-utb5c).
	nativeSessionID := ""
	if native && accountID != "" && h.renews() {
		nativeSessionID = weosoauth.NewNativeSessionID()
	}
	var tokenString string
	if accountID != "" {
		tokenCtx := ctx
		if nativeSessionID != "" {
			tokenCtx = weosoauth.WithNativeSession(ctx, nativeSessionID)
		}
		var issueErr error
		tokenString, issueErr = h.cfg.AuthService.IssueIdentityToken(
			tokenCtx, agent, accountID, authapp.AccountAlreadyVerified(),
		)
		if issueErr != nil {
			h.cfg.Logger.Warn(ctx, "password auth: failed to issue identity token", "error", issueErr)
			tokenString = ""
		}
	}
	if tokenString != "" {
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

	// A refresh token goes back wherever the token does on a sign-in that asked
	// for a native session (wm-lnimb, wm-nybvk): the token lasts an hour, and an
	// app in a native shell holds nothing else to renew it with. A browser's
	// sign-in gets none; its cookie session is what it renews with. The same
	// rule as the token, so a locked account's owner gets one for the deletion
	// too, and the renewal applies the same limit to it. Like the token it is
	// best-effort: without it the app signs in again when the token expires,
	// which is where it was before refresh tokens existed.
	var refresh weosoauth.NativeRefreshToken
	if nativeSessionID != "" && tokenString != "" {
		var refreshErr error
		refresh, refreshErr = weosoauth.IssueNativeRefreshToken(ctx, h.cfg.RefreshTokens, agent.GetID(), accountID, nativeSessionID)
		if refreshErr != nil {
			h.cfg.Logger.Warn(ctx, "password auth: failed to issue a refresh token; the app signs in again when the token expires",
				"error", refreshErr)
			refresh = weosoauth.NativeRefreshToken{}
		} else {
			h.purgeNativeRefreshTokens(ctx)
		}
	}

	response := authSuccessResponse{
		Agent: authAgentResponse{
			ID:    agent.GetID(),
			Name:  agent.Name(),
			Email: email,
		},
		Account:               accountResp,
		Token:                 tokenString,
		ExpiresAt:             authSession.ExpiresAt(),
		RefreshToken:          refresh.Raw,
		RefreshTokenExpiresAt: refresh.ExpiresAt,
	}
	if refresh.Raw != "" {
		// When to renew goes with what renews it: an instance that hands back no
		// refresh token answers with exactly the fields it always did.
		response.TokenExpiresAt = tokenExpiry(tokenString)
	}
	if erasurePending {
		response.ErasurePending = true
		response.Code = apimw.CodeAccountErasurePending
	}
	return respond(c, http.StatusOK, shape(response))
}

// lockedAccountFor finds an account whose erasure is unfinished that the
// agent may finish deleting, by the one rule the deletion route also applies
// (apimw.LockedAccountFor). Scoping the sign-in's session to it is what lets
// the answer say erasure_pending; the deletion route would admit the person
// from an unscoped session just the same.
func (h *PasswordAuthHandler) lockedAccountFor(ctx context.Context, agentID string) *authentities.Account {
	return apimw.LockedAccountFor(ctx, agentID, h.cfg.AccountRepo, h.cfg.ErasureLocks, h.cfg.Logger)
}
