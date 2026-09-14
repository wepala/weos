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

package middleware

import (
	"net/http"
	"strings"

	"github.com/wepala/weos/v3/domain/repositories"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

// BearerOrSession returns Echo middleware that authenticates requests via
// either a Bearer JWT token or the existing session cookie flow.
//
// When a Bearer token is present, it is validated using the JWTService and
// an auth.Identity is injected into context. When no Bearer token is present,
// the request is passed through the sessionAuth middleware (pericarp RequireAuth).
//
// A valid token is not the whole answer. A token that names no account is
// refused as the session path refuses a session that names none: 401
// {"error":"not authenticated","code":"unscoped_session"} (wm-92vba). No
// sign-in issues one, and an identity with no account would reach handlers
// that assume one.
//
// The token's account is then looked up,
// the one lookup the session path always made. A token issued before its
// account was erased would otherwise authenticate until it expired, and a
// write through it would recreate rows — and a per-account graph directory —
// under the deleted account's id. An account that is gone, or suspended, or
// part-way through an erasure, refuses the token; the last two carry the
// codes the session path answers with.
//
// The token's person must also still belong to that account, as the session
// path's ValidateSession requires (wm-jwojd). A person removed from an account
// keeps the token they were given for it until it expires, an hour later, so
// without this they would keep reading and writing there. The refusal is the
// session path's 401 — {"error":"not authenticated","code":
// "account_access_revoked"} — and is given before anything about the
// account's own state, as the session path gives it.
//
// A group that must not answer a third-party connector passes
// RefuseConnectorTokens. That refusal comes after the membership check and
// before the account's state, so a connector's token for a suspended or
// erasure-locked account gets the 403 no refresh changes.
//
// Unauthenticated requests receive a 401 with WWW-Authenticate header per the
// MCP Authorization spec, pointing to the Protected Resource Metadata endpoint.
func BearerOrSession(
	jwtService authapp.JWTService,
	sessionAuth func(http.Handler) http.Handler,
	baseURL string,
	accounts authrepos.AccountRepository,
	locks repositories.AccountErasureLocks,
	opts ...BearerOption,
) echo.MiddlewareFunc {
	check := newTokenCheck(jwtService, baseURL, accounts, locks, opts)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := extractBearer(c.Request())
			if token == "" {
				// No Bearer token — fall through to session auth.
				return sessionAuthEcho(sessionAuth, next, check.challenge.plain)(c)
			}
			return authenticateToken(c, next, token, check)
		}
	}
}

// CodeTokenNotAllowed is the code a valid token is refused with on a route its
// kind of token does not reach: a connector's token on a group that passed
// RefuseConnectorTokens.
const CodeTokenNotAllowed = "token_not_allowed"

// BearerOption tunes the token path for the group it guards.
type BearerOption func(*tokenCheck)

// RefuseConnectorTokens refuses a token the OAuth token endpoint issued to a
// third-party connector (wm-8i8ln). Such a token is for the MCP and agent
// routes a person agreed to connect the client to; the group that takes this
// option answers only a native sign-in's token. A token that carries no
// connector mark — every native sign-in's, including one issued before the
// mark existed — is unaffected.
//
// The refusal is 403 {"error":"insufficient_scope","code":"token_not_allowed"}
// with an insufficient_scope challenge (RFC 6750 section 3.1), not 401
// invalid_token: the token is valid, and a connector told invalid_token
// refreshes it and tries again, for a token no refresh can make acceptable.
func RefuseConnectorTokens() BearerOption {
	return func(c *tokenCheck) { c.refuseConnectorTokens = true }
}

func newTokenCheck(
	jwtService authapp.JWTService, baseURL string,
	accounts authrepos.AccountRepository, locks repositories.AccountErasureLocks,
	opts []BearerOption,
) tokenCheck {
	check := tokenCheck{
		jwtService: jwtService, accounts: accounts, locks: locks, challenge: newBearerChallenge(baseURL),
	}
	for _, opt := range opts {
		opt(&check)
	}
	return check
}

// BearerOrSessionForErasure authenticates the account deletion route. A
// request with no Bearer token goes to sessionAuth, which is
// SessionAuthForErasure in serve. A request with one takes BearerOrSession's
// token path, with the one admission SessionAuthForErasure makes for a
// session: a token scoped to an account whose erasure is unfinished is let
// through, marked as such, so the person can run the deletion again from the
// app that holds it (wm-aj2eb). Everything else the token path refuses stays
// refused, and a token wins over a session cookie beside it.
func BearerOrSessionForErasure(
	jwtService authapp.JWTService,
	baseURL string,
	accounts authrepos.AccountRepository,
	locks repositories.AccountErasureLocks,
	sessionAuth echo.MiddlewareFunc,
	opts ...BearerOption,
) echo.MiddlewareFunc {
	check := newTokenCheck(jwtService, baseURL, accounts, locks, opts)
	check.admitErasureLocked = true
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		bySession := sessionAuth(next)
		return func(c echo.Context) error {
			token := extractBearer(c.Request())
			if token == "" {
				return bySession(c)
			}
			return authenticateToken(c, next, token, check)
		}
	}
}

// bearerChallenge holds the WWW-Authenticate values the bearer path answers
// with: plain for a request with no credentials, invalidToken for a token
// that is refused, insufficientScope for a valid token this route does not
// take.
type bearerChallenge struct {
	plain             string
	invalidToken      string
	insufficientScope string
}

func newBearerChallenge(baseURL string) bearerChallenge {
	resourceMetadata := `resource_metadata="` + strings.TrimRight(baseURL, "/") +
		`/.well-known/oauth-protected-resource"`
	return bearerChallenge{
		plain: `Bearer ` + resourceMetadata,
		invalidToken: `Bearer ` + resourceMetadata +
			`, error="invalid_token", error_description="The access token is invalid or expired"`,
		insufficientScope: `Bearer ` + resourceMetadata +
			`, error="insufficient_scope", error_description="A connector's access token does not reach this route"`,
	}
}

// tokenCheck is what the token path needs. admitErasureLocked is set only for
// the deletion route; refuseConnectorTokens by RefuseConnectorTokens.
type tokenCheck struct {
	jwtService            authapp.JWTService
	accounts              authrepos.AccountRepository
	locks                 repositories.AccountErasureLocks
	challenge             bearerChallenge
	admitErasureLocked    bool
	refuseConnectorTokens bool
}

// authenticateToken is the token path: it validates token, refuses one that
// names no account, and checks that the account exists, that the token's
// person still belongs to it, that the route takes its kind of token, and that
// the account is active — in that order. It writes the refusal
// itself, or puts the token's identity in the request's context and calls
// next.
func authenticateToken(c echo.Context, next echo.HandlerFunc, token string, check tokenCheck) error {
	ctx := c.Request().Context()
	claims, err := check.jwtService.ValidateToken(ctx, token)
	if err != nil {
		return refuseToken(c, check.challenge, "")
	}

	if claims.ActiveAccountID == "" {
		// The session path's own 401 for a session that names no account, with
		// the challenge a refused token carries (wm-92vba).
		c.Response().Header().Set("WWW-Authenticate", check.challenge.invalidToken)
		return refuse(c, CodeUnscopedSession)
	}

	state, err := stateOfAccount(ctx, claims.ActiveAccountID, check.accounts, check.locks)
	if err != nil {
		// Fail closed: the account's state could not be read, so the token is
		// not known to be good.
		return accountStateUnreadable(c)
	}
	if state == accountGone {
		return refuseToken(c, check.challenge, "")
	}
	role, err := check.accounts.FindMemberRole(ctx, claims.ActiveAccountID, claims.AgentID)
	if err != nil {
		// Fail closed, as for an unreadable account state.
		return accountStateUnreadable(c)
	}
	if role == "" {
		// The session path's own 401, body and all — the identity read pins
		// that shape for a token (wm-qqoq2) — with the challenge a refused
		// token carries.
		c.Response().Header().Set("WWW-Authenticate", check.challenge.invalidToken)
		return refuse(c, CodeAccountAccessRevoked)
	}
	// A connector's token is refused here before anything about the account's
	// state: the route refuses it in every state, and a 401 invalid_token for a
	// suspended or erasure-locked account would send the connector to refresh a
	// token no refresh makes acceptable on this route.
	if check.refuseConnectorTokens && weosoauth.IssuedToConnector(claims) {
		c.Response().Header().Set("WWW-Authenticate", check.challenge.insufficientScope)
		return c.JSON(http.StatusForbidden,
			map[string]string{"error": "insufficient_scope", "code": CodeTokenNotAllowed})
	}
	locked := false
	switch state {
	case accountSuspended:
		return refuseToken(c, check.challenge, CodeAccountDeactivated)
	case accountErasurePending:
		if !check.admitErasureLocked {
			return refuseToken(c, check.challenge, CodeAccountErasurePending)
		}
		locked = true
	}

	admit(c, &auth.Identity{
		AgentID:         claims.AgentID,
		AccountIDs:      claims.AccountIDs,
		ActiveAccountID: claims.ActiveAccountID,
	}, locked)
	return next(c)
}

// refuseToken writes the bearer path's 401: invalid_token with the challenge
// that says so, and a code only when there is one to give.
func refuseToken(c echo.Context, challenge bearerChallenge, code string) error {
	c.Response().Header().Set("WWW-Authenticate", challenge.invalidToken)
	body := map[string]string{"error": "invalid_token"}
	if code != "" {
		body["code"] = code
	}
	return c.JSON(http.StatusUnauthorized, body)
}

// accountStateRetryAfter is the Retry-After, in seconds, on the 503 for an
// account state or a membership that could not be read (wm-w0bha). Those reads
// fail on a database blip, which clears in seconds; the header tells a client
// to back off and try again rather than take the answer for an outage.
const accountStateRetryAfter = "5"

// accountStateUnreadable answers a request whose account state, erasure lock or
// membership could not be read: 503 with Retry-After. The request is not let
// through on a guess.
func accountStateUnreadable(c echo.Context) error {
	c.Response().Header().Set("Retry-After", accountStateRetryAfter)
	return c.JSON(http.StatusServiceUnavailable,
		map[string]string{"error": "could not read the account's state"})
}

// BearerWhenPresent authenticates a request that carries a Bearer token exactly
// as BearerOrSession does — the same token check, the same account lookup and
// the same refusals — and passes a request that carries none to the next
// handler untouched.
//
// It is for a route that checks the session its cookie names itself and whose
// cookie path must not change: GET /api/auth/me (wm-ccg4f), which an app in a
// native shell reads with only the token its sign-in handed back (wm-hg3xf).
// A token wins over a session cookie beside it, as under BearerOrSession: a
// token that does not validate is refused and never handed to the cookie path.
// Unlike the MCP group, no Impersonation middleware follows it, so an
// impersonation cookie beside a token changes nothing (wm-fqjc2).
func BearerWhenPresent(
	jwtService authapp.JWTService,
	baseURL string,
	accounts authrepos.AccountRepository,
	locks repositories.AccountErasureLocks,
) echo.MiddlewareFunc {
	return BearerOrSession(jwtService, handlerChecksSession, baseURL, accounts, locks)
}

// handlerChecksSession stands in for session auth on a route whose handler
// checks the session itself. sessionAuthEcho clears its challenge header
// before it calls the handler, so the handler's own answer goes out as it
// would with no middleware in front of it.
func handlerChecksSession(next http.Handler) http.Handler { return next }

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// sessionAuthEcho wraps the pericarp http.Handler middleware into an Echo
// handler and adds the WWW-Authenticate header on 401 responses.
func sessionAuthEcho(
	sessionAuth func(http.Handler) http.Handler,
	next echo.HandlerFunc,
	wwwAuth string,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Set WWW-Authenticate before calling wrapped so it is present
		// if the session middleware commits a 401 response. The inner
		// handler clears it BEFORE calling next(c) so the header doesn't
		// leak onto successful responses (where next(c) writes the body).
		c.Response().Header().Set("WWW-Authenticate", wwwAuth)

		var called bool
		var handlerErr error
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			c.SetRequest(r)
			// Auth succeeded — clear the 401 header before next(c) writes.
			c.Response().Header().Del("WWW-Authenticate")
			handlerErr = next(c)
		})

		wrapped := sessionAuth(inner)
		wrapped.ServeHTTP(c.Response(), c.Request())

		if called {
			return handlerErr
		}
		// Session middleware rejected — 401 already written with header.
		return nil
	}
}
