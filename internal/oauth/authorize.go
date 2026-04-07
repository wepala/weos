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
	"strings"

	"weos/domain/entities"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

const oauthSessionName = "weos-oauth-flow"

// SupportedScopes is the set of scopes the server will issue tokens for.
// Must stay in sync with discovery.go's scopes_supported field.
var SupportedScopes = map[string]bool{
	"mcp:read":  true,
	"mcp:write": true,
	"mcp:admin": true,
}

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

// Authorize returns a handler for the OAuth authorization endpoint.
// GET /oauth/authorize
//
// This endpoint validates the MCP client's OAuth parameters, creates a pending
// authorization code, and redirects the user to Google OAuth for authentication.
// After Google auth completes, the callback handler resolves the identity and
// redirects back to the MCP client's redirect_uri with an authorization code.
func Authorize(
	authService authapp.AuthenticationService,
	sessionStore sessions.Store,
	clientRepo ClientRepository,
	codeRepo AuthCodeRepository,
	logger entities.Logger,
	baseURL string,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		responseType := c.QueryParam("response_type")
		clientID := c.QueryParam("client_id")
		redirectURI := c.QueryParam("redirect_uri")
		codeChallenge := c.QueryParam("code_challenge")
		codeChallengeMethod := c.QueryParam("code_challenge_method")
		state := c.QueryParam("state")
		scope := c.QueryParam("scope")

		if responseType != "code" {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "unsupported_response_type"})
		}
		if clientID == "" || redirectURI == "" || codeChallenge == "" {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid_request",
					"error_description": "client_id, redirect_uri, and code_challenge are required"})
		}
		if codeChallengeMethod != "S256" {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid_request",
					"error_description": "code_challenge_method must be S256"})
		}
		if err := validateScope(scope); err != nil {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid_scope",
					"error_description": err.Error()})
		}

		// Validate client exists and redirect_uri is registered.
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

		// Create pending authorization code.
		authCode := &OAuthAuthorizationCode{
			ClientID:            clientID,
			RedirectURI:         redirectURI,
			CodeChallenge:       codeChallenge,
			CodeChallengeMethod: "S256",
			Scope:               scope,
			State:               state,
			Status:              StatusPending,
		}
		if err := codeRepo.Create(c.Request().Context(), authCode); err != nil {
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}

		// Initiate Google OAuth via pericarp's auth flow.
		callbackURL := baseURL + "/oauth/callback"
		authReq, err := authService.InitiateAuthFlow(
			c.Request().Context(), "google", callbackURL)
		if err != nil {
			logger.Error(c.Request().Context(), "oauth authorize: initiate flow failed",
				"error", err)
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}

		// Store auth code ID + PKCE verifier in session for the callback.
		sess, err := sessionStore.Get(c.Request(), oauthSessionName)
		if err != nil {
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "session_error"})
		}
		sess.Values["oauth_code"] = authCode.Code
		sess.Values["oauth_code_verifier"] = authReq.CodeVerifier
		sess.Values["oauth_state"] = authReq.State
		if err := sess.Save(c.Request(), c.Response()); err != nil {
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "session_error"})
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
