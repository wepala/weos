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
	"errors"
	"net/http"
	"net/url"
	"time"

	"weos/domain/entities"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

// Callback returns a handler for the OAuth callback after Google authentication.
// GET /oauth/callback
//
// This handler completes the Google OAuth exchange, resolves the user identity,
// updates the pending authorization code with the agent/account IDs, and
// redirects back to the MCP client's redirect_uri with the authorization code.
func Callback(
	authService authapp.AuthenticationService,
	sessionStore sessions.Store,
	codeRepo AuthCodeRepository,
	accountRepo authrepos.AccountRepository,
	logger entities.Logger,
	baseURL string,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		// Retrieve all session values before modifying.
		sess, err := sessionStore.Get(c.Request(), oauthSessionName)
		if err != nil {
			logger.Error(ctx, "oauth callback: invalid session", "error", err)
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid_request"})
		}
		codeStr, ok := sess.Values["oauth_code"].(string)
		if !ok || codeStr == "" {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid_request",
					"error_description": "no pending authorization"})
		}
		codeVerifier, ok := sess.Values["oauth_code_verifier"].(string)
		if !ok || codeVerifier == "" {
			logger.Error(ctx, "oauth callback: missing PKCE verifier in session")
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "server_error"})
		}
		expectedState, _ := sess.Values["oauth_state"].(string)

		// Verify state BEFORE clearing the session, so that a malicious or
		// stale callback hit doesn't wipe an in-progress flow.
		if expectedState == "" || c.QueryParam("state") != expectedState {
			logger.Warn(ctx, "oauth callback: state mismatch",
				"expected", expectedState, "got", c.QueryParam("state"))
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid_request",
					"error_description": "state mismatch"})
		}

		// Validate the authorization code (also before clearing session).
		authCode, err := codeRepo.FindByCode(ctx, codeStr)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return c.JSON(http.StatusBadRequest,
					map[string]string{"error": "invalid_grant"})
			}
			logger.Error(ctx, "oauth callback: code lookup failed",
				"code", codeStr, "error", err)
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}
		if authCode.Status != StatusPending || time.Now().After(authCode.ExpiresAt) {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid_grant"})
		}

		// Validate provider auth code is present before clearing session.
		googleCode := c.QueryParam("code")
		if googleCode == "" {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid_request",
					"error_description": "missing provider auth code"})
		}

		// All validations passed — now clear the session.
		delete(sess.Values, "oauth_code")
		delete(sess.Values, "oauth_code_verifier")
		delete(sess.Values, "oauth_state")
		if err := sess.Save(c.Request(), c.Response()); err != nil {
			logger.Error(ctx, "oauth callback: session save failed", "error", err)
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}

		callbackURL := baseURL + "/oauth/callback"
		authResult, err := authService.ExchangeCode(
			ctx, googleCode, codeVerifier, "google", callbackURL)
		if err != nil {
			logger.Error(ctx, "oauth callback: code exchange failed", "error", err)
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}

		agent, _, account, err := authService.FindOrCreateAgent(
			ctx, authResult.UserInfo)
		if err != nil {
			logger.Error(ctx, "oauth callback: identity resolution failed",
				"error", err)
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}

		accountID := ""
		if account != nil {
			accountID = account.GetID()
		} else {
			accounts, lookupErr := accountRepo.FindByMember(ctx, agent.GetID())
			if lookupErr != nil {
				logger.Error(ctx, "oauth callback: account lookup failed",
					"agent", agent.GetID(), "error", lookupErr)
				return c.JSON(http.StatusInternalServerError,
					map[string]string{"error": "server_error"})
			}
			if len(accounts) > 0 {
				accountID = accounts[0].GetID()
			}
		}

		// Update the authorization code with identity info.
		if err := codeRepo.UpdateIdentity(
			ctx, authCode.Code, agent.GetID(), accountID); err != nil {
			logger.Error(ctx, "oauth callback: identity update failed",
				"code", authCode.Code, "error", err)
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}

		logger.Info(ctx, "oauth authorization code issued",
			"agent", agent.GetID(), "client", authCode.ClientID)

		// Redirect back to the MCP client with the authorization code.
		redirectURL, err := url.Parse(authCode.RedirectURI)
		if err != nil {
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}
		q := redirectURL.Query()
		q.Set("code", authCode.Code)
		if authCode.State != "" {
			q.Set("state", authCode.State)
		}
		redirectURL.RawQuery = q.Encode()

		return c.Redirect(http.StatusFound, redirectURL.String())
	}
}
