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

package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

const oauthSessionName = "weos-oauth-flow"

// SupportedScopesList is the canonical list of scopes the server issues
// tokens for. Discovery metadata and the validateScope helper both derive
// from this single source to prevent drift.
var SupportedScopesList = []string{
	"mcp:read",
	"mcp:write",
	"mcp:admin",
}

// SupportedScopes is a set built from SupportedScopesList for fast lookup.
var SupportedScopes = func() map[string]bool {
	m := make(map[string]bool, len(SupportedScopesList))
	for _, s := range SupportedScopesList {
		m[s] = true
	}
	return m
}()

// validateScope returns an error if the requested scope string contains
// any unknown scope. An empty scope is allowed (caller may apply defaults).
func validateScope(scope string) error {
	if scope == "" {
		return nil
	}
	for _, s := range strings.Fields(scope) {
		if !SupportedScopes[s] {
			return fmt.Errorf("unsupported scope: %s", s)
		}
	}
	return nil
}

// redirectAuthError redirects back to the MCP client's redirect_uri with
// the standard OAuth error parameters per RFC 6749 §4.1.2.1. Used after
// client_id and redirect_uri have been validated.
func redirectAuthError(c echo.Context, redirectURI, errCode, errDesc, state string) error {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid_request",
				"error_description": "invalid redirect_uri"})
	}
	q := u.Query()
	q.Set("error", errCode)
	if errDesc != "" {
		q.Set("error_description", errDesc)
	}
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return c.Redirect(http.StatusFound, u.String())
}

// signInPath is where a person is sent when this instance has to authenticate
// them itself. It is the SPA's root, which shows whichever sign-in the instance
// offers — password, OAuth buttons, or both — so this handler never has to know
// which. returnToParam carries the authorization the sign-in interrupted.
const signInPath = "/"
const returnToParam = "return_to"

// Authorize returns a handler for the OAuth authorization endpoint.
// GET /oauth/authorize
//
// It validates the MCP client's OAuth parameters and then establishes who the
// resource owner is, by whichever means this instance has:
//
//   - A request that already carries a valid session needs no further
//     authentication. The code is issued against that session's identity and
//     the person goes straight back to the client.
//   - Otherwise, an instance with an OAuth provider hands off to it, exactly as
//     before, and the callback finishes the job.
//   - An instance with no provider sends the person to its own sign-in with a
//     way back, rather than to a provider it does not have. Without this such an
//     instance could never complete an authorization at all, however well the
//     person was signed in to it.
//
// What the protocol requires is unchanged on every one of those paths: this
// decides *who* authenticates the resource owner, not what the client must
// prove.
func Authorize(
	authService authapp.AuthenticationService,
	sessionManager session.SessionManager,
	sessionStore sessions.Store,
	clientRepo ClientRepository,
	codeRepo AuthCodeRepository,
	credentialRepo authrepos.CredentialRepository,
	logger entities.Logger,
	baseURL string,
	oauthProviderConfigured bool,
	allowedEmails []string,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		clientID := c.QueryParam("client_id")
		redirectURI := c.QueryParam("redirect_uri")

		// Step 1: Validate client_id and redirect_uri FIRST. These errors
		// must use JSON responses because we can't yet trust the redirect_uri
		// (RFC 6749 §4.1.2.1: don't redirect to an unverified URI).
		if clientID == "" || redirectURI == "" {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid_request",
					"error_description": "client_id and redirect_uri are required"})
		}
		client, err := clientRepo.FindByID(c.Request().Context(), clientID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return c.JSON(http.StatusBadRequest,
					map[string]string{"error": "invalid_client"})
			}
			logger.Error(c.Request().Context(), "oauth authorize: client lookup failed",
				"client", clientID, "error", err)
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}
		allowed, uriErr := isRedirectURIAllowed(client.RedirectURIs, redirectURI)
		if uriErr != nil {
			logger.Error(c.Request().Context(), "oauth authorize: corrupt redirect_uris",
				"client", clientID, "error", uriErr)
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}
		if !allowed {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid_request",
					"error_description": "redirect_uri not registered for this client"})
		}

		// Step 2: redirect_uri is now trusted. From here on, return errors
		// via redirect to the client per RFC 6749 §4.1.2.1.
		responseType := c.QueryParam("response_type")
		codeChallenge := c.QueryParam("code_challenge")
		codeChallengeMethod := c.QueryParam("code_challenge_method")
		state := c.QueryParam("state")
		scope := c.QueryParam("scope")

		if responseType != "code" {
			return redirectAuthError(c, redirectURI,
				"unsupported_response_type", "only response_type=code is supported", state)
		}
		if codeChallenge == "" {
			return redirectAuthError(c, redirectURI,
				"invalid_request", "code_challenge is required", state)
		}
		if codeChallengeMethod != "S256" {
			return redirectAuthError(c, redirectURI,
				"invalid_request", "code_challenge_method must be S256", state)
		}
		if err := validateScope(scope); err != nil {
			return redirectAuthError(c, redirectURI,
				"invalid_scope", err.Error(), state)
		}

		// Step 3: establish who the resource owner is. Everything the protocol
		// requires has already been checked above, and stays checked on every
		// branch below — a session is not a reason to skip the proof key.

		// Already authenticated? Then there is nobody left to authenticate.
		if identity := resolveSession(c, sessionManager, authService); identity != nil {
			// The allowlist decides who may reach this instance, not which door
			// they came through. It is enforced on the Google path in the
			// callback, so without this an instance that also offers password
			// sign-in would hand a code to someone the allowlist excludes —
			// a way around the gate rather than a second gate.
			if !sessionAllowed(c, credentialRepo, identity, allowedEmails, logger) {
				return redirectAuthError(c, redirectURI,
					"access_denied", "this account is not permitted on this instance", state)
			}
			return issueCodeForSession(c, codeRepo, logger, identity, authorizationRequest{
				clientID:            clientID,
				redirectURI:         redirectURI,
				codeChallenge:       codeChallenge,
				codeChallengeMethod: codeChallengeMethod,
				scope:               scope,
				state:               state,
			})
		}

		// No session, and no provider to hand off to: this instance has to
		// authenticate the person itself. Send them to its own sign-in with a
		// way back to the authorization they interrupted.
		if !oauthProviderConfigured {
			return c.Redirect(http.StatusFound, signInURL(c.Request().URL))
		}

		// Initiate Google OAuth via pericarp's auth flow.
		callbackURL := baseURL + "/oauth/callback"
		authReq, err := authService.InitiateAuthFlow(
			c.Request().Context(), "google", callbackURL)
		if err != nil {
			logger.Error(c.Request().Context(), "oauth authorize: initiate flow failed",
				"error", err)
			return redirectAuthError(c, redirectURI, "server_error", "", state)
		}

		// Pre-generate the code value so it can be stored in the session
		// before we persist the DB row, allowing us to defer the DB write
		// until after the session save succeeds.
		codeValue, err := generateRandomCode()
		if err != nil {
			logger.Error(c.Request().Context(), "oauth authorize: code generation failed",
				"error", err)
			return redirectAuthError(c, redirectURI, "server_error", "", state)
		}

		sess, err := sessionStore.Get(c.Request(), oauthSessionName)
		if err != nil {
			logger.Error(c.Request().Context(), "oauth authorize: session get failed",
				"error", err)
			return redirectAuthError(c, redirectURI, "server_error", "", state)
		}
		sess.Values["oauth_code"] = codeValue
		sess.Values["oauth_code_verifier"] = authReq.CodeVerifier
		sess.Values["oauth_state"] = authReq.State
		if err := sess.Save(c.Request(), c.Response()); err != nil {
			logger.Error(c.Request().Context(), "oauth authorize: session save failed",
				"error", err)
			return redirectAuthError(c, redirectURI, "server_error", "", state)
		}

		// Persist the pending authorization code only after session save
		// succeeds, so transient failures don't leave orphaned DB rows.
		authCode := &OAuthAuthorizationCode{
			Code:                codeValue,
			ClientID:            clientID,
			RedirectURI:         redirectURI,
			CodeChallenge:       codeChallenge,
			CodeChallengeMethod: "S256",
			Scope:               scope,
			State:               state,
			Status:              StatusPending,
		}
		if err := codeRepo.Create(c.Request().Context(), authCode); err != nil {
			logger.Error(c.Request().Context(), "oauth authorize: code persistence failed",
				"error", err)
			return redirectAuthError(c, redirectURI, "server_error", "", state)
		}

		return c.Redirect(http.StatusFound, authReq.AuthURL)
	}
}

func isRedirectURIAllowed(registeredJSON, uri string) (bool, error) {
	var uris []string
	if err := json.Unmarshal([]byte(registeredJSON), &uris); err != nil {
		return false, fmt.Errorf("corrupt redirect_uris in client record: %w", err)
	}
	for _, u := range uris {
		if u == uri {
			return true, nil
		}
	}
	return false, nil
}

// authorizationRequest is the validated request, carried into code issuance so
// the session path stores exactly what the callback path stores.
type authorizationRequest struct {
	clientID            string
	redirectURI         string
	codeChallenge       string
	codeChallengeMethod string
	scope               string
	state               string
}

// sessionIdentity is a resource owner this instance has already authenticated.
type sessionIdentity struct {
	agentID   string
	accountID string
}

// resolveSession reports who the request is, or nil if it is nobody.
//
// The cookie alone is not the answer. GetHTTPSession decodes it and returns
// whatever it holds — including ExpiresAt, which it does not check — so a
// session that expired hours ago decodes perfectly well. ValidateSession is
// what asks the store whether the session is still live, and it is the same
// pair RequireAuth uses everywhere else in the service. Issuing an
// authorization code on the strength of the cookie alone would make signing
// out meaningless for exactly the rail where it matters most.
//
// Every failure is "not signed in", deliberately: an unreadable cookie, an
// expired session and a revoked one are all reasons to authenticate the person
// again, not reasons to hand the client a server error. Someone whose only
// mistake was leaving a tab open overnight gets a sign-in page.
func resolveSession(
	c echo.Context,
	sessionManager session.SessionManager,
	authService authapp.AuthenticationService,
) *sessionIdentity {
	if sessionManager == nil {
		return nil
	}
	ctx := c.Request().Context()
	data, err := sessionManager.GetHTTPSession(c.Request())
	if err != nil || data == nil || data.SessionID == "" {
		return nil
	}
	info, err := authService.ValidateSession(ctx, data.SessionID)
	if err != nil || info == nil || info.AgentID == "" {
		return nil
	}

	// A session that names no account is treated as no session at all.
	//
	// There used to be a fallback here that looked memberships up and took the
	// first one. It cannot be right: someone in several accounts gets whichever
	// the query returns first, not the one they signed in to, and the token
	// minted from this identity then acts somewhere they never chose.
	//
	// Refusing rather than guessing is not automatic, because ValidateSession
	// does NOT reject an unscoped session — it only runs its membership and
	// deactivation checks when the session names an account, so an unscoped
	// session validates cleanly and arrives here looking healthy. RequireAuth
	// is what refuses it on the ordinary API, with unscoped_session. Without
	// this check, a cookie that every admin page rejects would still mint an
	// authorization code bound to no account, and the access token exchanged
	// from it would act nowhere — the very symptom this change exists to
	// remove, moved onto the connector rail. Taking the same stance sends the
	// person to sign in, which is what produces a scoped session.
	if info.AccountID == "" {
		return nil
	}
	return &sessionIdentity{agentID: info.AgentID, accountID: info.AccountID}
}

// issueCodeForSession mints an authorization code already bound to an identity
// and sends the person back to the client with it.
//
// This is the callback's tail without the round trip: the same row, in the same
// issued state, carrying the same proof key and scope. Nothing is written to
// the OAuth flow session, because there is no flow to resume — the code is
// complete when it is created.
func issueCodeForSession(
	c echo.Context,
	codeRepo AuthCodeRepository,
	logger entities.Logger,
	identity *sessionIdentity,
	req authorizationRequest,
) error {
	ctx := c.Request().Context()

	codeValue, err := generateRandomCode()
	if err != nil {
		logger.Error(ctx, "oauth authorize: code generation failed", "error", err)
		return redirectAuthError(c, req.redirectURI, "server_error", "", req.state)
	}

	authCode := &OAuthAuthorizationCode{
		Code:                codeValue,
		ClientID:            req.clientID,
		RedirectURI:         req.redirectURI,
		CodeChallenge:       req.codeChallenge,
		CodeChallengeMethod: req.codeChallengeMethod,
		Scope:               req.scope,
		State:               req.state,
		AgentID:             identity.agentID,
		AccountID:           identity.accountID,
		Status:              StatusPending,
	}
	if err := codeRepo.Create(ctx, authCode); err != nil {
		logger.Error(ctx, "oauth authorize: code persistence failed", "error", err)
		return redirectAuthError(c, req.redirectURI, "server_error", "", req.state)
	}

	// Bind the identity through the same call the callback uses, which is also
	// what moves the row from pending to issued. The token endpoint only
	// accepts issued codes, so a row left pending would be minted, handed over,
	// and then refused at exchange.
	if err := codeRepo.UpdateIdentity(ctx, authCode.Code, identity.agentID, identity.accountID); err != nil {
		logger.Error(ctx, "oauth authorize: identity binding failed",
			"code_hash", MaskCode(authCode.Code), "error", err)
		return redirectAuthError(c, req.redirectURI, "server_error", "", req.state)
	}

	logger.Info(ctx, "oauth authorization code issued from session",
		"agent", identity.agentID, "client", req.clientID)

	redirectURL, err := url.Parse(req.redirectURI)
	if err != nil {
		return redirectAuthError(c, req.redirectURI, "server_error", "", req.state)
	}
	q := redirectURL.Query()
	q.Set("code", authCode.Code)
	if req.state != "" {
		q.Set("state", req.state)
	}
	redirectURL.RawQuery = q.Encode()

	return c.Redirect(http.StatusFound, redirectURL.String())
}

// sessionAllowed applies the instance's identity allowlist to a session-based
// authorization. An empty allowlist admits everyone, exactly as it does on the
// Google path.
func sessionAllowed(
	c echo.Context,
	credentialRepo authrepos.CredentialRepository,
	identity *sessionIdentity,
	allowedEmails []string,
	logger entities.Logger,
) bool {
	if len(allowedEmails) == 0 {
		return true
	}
	ctx := c.Request().Context()
	credentials, err := credentialRepo.FindByAgent(ctx, identity.agentID)
	if err != nil {
		// Refuse rather than guess: an allowlist that cannot be checked is not
		// an allowlist that passes.
		logger.Error(ctx, "oauth authorize: could not read credentials for allowlist check",
			"agent", identity.agentID, "error", err)
		return false
	}
	for _, credential := range credentials {
		if emailAllowed(strings.ToLower(credential.Email()), allowedEmails) {
			return true
		}
	}
	logger.Warn(ctx, "oauth authorize: session refused by the identity allowlist",
		"agent", identity.agentID)
	return false
}

// signInURL points at this instance's own sign-in, carrying the authorization
// it interrupted so the person can be returned to it once they are known.
//
// Relative on purpose: it is the same origin the person is already on, and
// building it from a configured base URL would send them somewhere else the
// moment that base URL is wrong.
func signInURL(authorizeURL *url.URL) string {
	returnTo := authorizeURL.RequestURI()
	return signInPath + "?" + returnToParam + "=" + url.QueryEscape(returnTo)
}
