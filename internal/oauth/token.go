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
	"time"

	"weos/domain/entities"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

// accessTokenExpiresIn is the access token TTL in seconds, derived from
// the JWT provider's configured TTL.
var accessTokenExpiresIn = int(defaultAccessTokenTTL.Seconds())

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type tokenErrorResponse struct {
	Error       string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

// Token returns a handler for the OAuth token endpoint.
// POST /oauth/token
func Token(
	jwtService authapp.JWTService,
	codeRepo AuthCodeRepository,
	refreshRepo RefreshTokenRepository,
	agentRepo authrepos.AgentRepository,
	accountRepo authrepos.AccountRepository,
	logger entities.Logger,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		// OAuth 2.0/2.1 §5.1: token responses must not be cached.
		c.Response().Header().Set("Cache-Control", "no-store")
		c.Response().Header().Set("Pragma", "no-cache")

		grantType := c.FormValue("grant_type")
		switch grantType {
		case "authorization_code":
			return handleAuthCodeGrant(c, jwtService, codeRepo, agentRepo, accountRepo, refreshRepo, logger)
		case "refresh_token":
			return handleRefreshGrant(c, jwtService, refreshRepo, agentRepo, accountRepo, logger)
		default:
			return c.JSON(http.StatusBadRequest, tokenErrorResponse{
				Error: "unsupported_grant_type",
			})
		}
	}
}

func handleAuthCodeGrant(
	c echo.Context,
	jwtService authapp.JWTService,
	codeRepo AuthCodeRepository,
	agentRepo authrepos.AgentRepository,
	accountRepo authrepos.AccountRepository,
	refreshRepo RefreshTokenRepository,
	logger entities.Logger,
) error {
	ctx := c.Request().Context()
	code := c.FormValue("code")
	codeVerifier := c.FormValue("code_verifier")
	clientID := c.FormValue("client_id")
	redirectURI := c.FormValue("redirect_uri")

	if code == "" || codeVerifier == "" || clientID == "" || redirectURI == "" {
		return c.JSON(http.StatusBadRequest, tokenErrorResponse{
			Error:       "invalid_request",
			Description: "code, code_verifier, client_id, and redirect_uri are required",
		})
	}

	authCode, err := codeRepo.FindByCode(ctx, code)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.JSON(http.StatusBadRequest, tokenErrorResponse{
				Error: "invalid_grant",
			})
		}
		logger.Error(ctx, "oauth token: code lookup failed",
			"code_hash", MaskCode(code), "error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}
	if authCode.Status != StatusIssued {
		logger.Warn(ctx, "oauth token: code not in issued state",
			"status", authCode.Status, "client", clientID)
		return c.JSON(http.StatusBadRequest, tokenErrorResponse{
			Error: "invalid_grant",
		})
	}
	if time.Now().After(authCode.ExpiresAt) {
		return c.JSON(http.StatusBadRequest, tokenErrorResponse{
			Error: "invalid_grant",
		})
	}
	// Verify client_id matches the code's client (OAuth 2.1 §4.1.3).
	if clientID != authCode.ClientID {
		logger.Warn(ctx, "oauth token: client_id mismatch",
			"expected", authCode.ClientID, "got", clientID)
		return c.JSON(http.StatusBadRequest, tokenErrorResponse{
			Error: "invalid_grant",
		})
	}
	if redirectURI != authCode.RedirectURI {
		return c.JSON(http.StatusBadRequest, tokenErrorResponse{
			Error: "invalid_grant",
		})
	}

	// Verify PKCE method matches what was stored at authorize-time.
	if authCode.CodeChallengeMethod != "S256" {
		logger.Warn(ctx, "oauth token: unsupported code_challenge_method on stored code",
			"client", authCode.ClientID, "method", authCode.CodeChallengeMethod)
		return c.JSON(http.StatusBadRequest, tokenErrorResponse{
			Error: "invalid_grant",
		})
	}
	challenge := authapp.GenerateCodeChallenge(codeVerifier)
	if challenge != authCode.CodeChallenge {
		logger.Warn(ctx, "oauth token: PKCE verification failed",
			"client", authCode.ClientID)
		return c.JSON(http.StatusBadRequest, tokenErrorResponse{
			Error: "invalid_grant",
		})
	}

	// Atomically claim the code (single-use enforcement). This must happen
	// before any heavy work (agent lookup, refresh token creation) so
	// concurrent requests for the same code cannot create duplicate refresh
	// tokens before one of them flips the status.
	if err := codeRepo.MarkExchanged(ctx, code); err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.JSON(http.StatusBadRequest, tokenErrorResponse{
				Error: "invalid_grant",
			})
		}
		logger.Error(ctx, "oauth token: mark exchanged failed",
			"code_hash", MaskCode(code), "error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}

	agent, err := agentRepo.FindByID(ctx, authCode.AgentID)
	if err != nil {
		logger.Error(ctx, "oauth token: agent lookup failed",
			"agent", authCode.AgentID, "error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}
	accounts, err := accountRepo.FindByMember(ctx, agent.GetID())
	if err != nil {
		logger.Error(ctx, "oauth token: account lookup failed",
			"agent", agent.GetID(), "error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}

	accessToken, err := jwtService.IssueToken(
		ctx, agent, accounts, authCode.AccountID)
	if err != nil {
		logger.Error(ctx, "oauth token: JWT issuance failed", "error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}

	rawRefresh, err := GenerateRefreshToken()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}
	refreshToken := &OAuthRefreshToken{
		AgentID:   authCode.AgentID,
		AccountID: authCode.AccountID,
		ClientID:  authCode.ClientID,
		Scope:     authCode.Scope,
	}
	if err := refreshRepo.Create(ctx, refreshToken, rawRefresh); err != nil {
		logger.Error(ctx, "oauth token: refresh token creation failed",
			"error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}

	logger.Info(ctx, "oauth access token issued",
		"agent", agent.GetID(), "client", authCode.ClientID)

	return c.JSON(http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    accessTokenExpiresIn,
		RefreshToken: rawRefresh,
		Scope:        authCode.Scope,
	})
}

func handleRefreshGrant(
	c echo.Context,
	jwtService authapp.JWTService,
	refreshRepo RefreshTokenRepository,
	agentRepo authrepos.AgentRepository,
	accountRepo authrepos.AccountRepository,
	logger entities.Logger,
) error {
	ctx := c.Request().Context()
	rawToken := c.FormValue("refresh_token")
	clientID := c.FormValue("client_id")
	if rawToken == "" || clientID == "" {
		return c.JSON(http.StatusBadRequest, tokenErrorResponse{
			Error:       "invalid_request",
			Description: "refresh_token and client_id are required",
		})
	}

	tokenHash := HashToken(rawToken)
	stored, err := refreshRepo.FindByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.JSON(http.StatusBadRequest, tokenErrorResponse{
				Error: "invalid_grant",
			})
		}
		logger.Error(ctx, "oauth refresh: token lookup failed", "error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}
	if stored.Revoked || time.Now().After(stored.ExpiresAt) {
		return c.JSON(http.StatusBadRequest, tokenErrorResponse{
			Error: "invalid_grant",
		})
	}
	// Verify client_id matches the refresh token's client (RFC 6749 §6).
	if clientID != stored.ClientID {
		logger.Warn(ctx, "oauth refresh: client_id mismatch",
			"expected", stored.ClientID, "got", clientID)
		return c.JSON(http.StatusBadRequest, tokenErrorResponse{
			Error: "invalid_grant",
		})
	}

	agent, err := agentRepo.FindByID(ctx, stored.AgentID)
	if err != nil {
		logger.Error(ctx, "oauth refresh: agent lookup failed",
			"agent", stored.AgentID, "error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}
	accounts, err := accountRepo.FindByMember(ctx, agent.GetID())
	if err != nil {
		logger.Error(ctx, "oauth refresh: account lookup failed",
			"agent", agent.GetID(), "error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}

	accessToken, err := jwtService.IssueToken(
		ctx, agent, accounts, stored.AccountID)
	if err != nil {
		logger.Error(ctx, "oauth refresh: JWT issuance failed", "error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}

	newRawRefresh, err := GenerateRefreshToken()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}
	newRefresh := &OAuthRefreshToken{
		AgentID:   stored.AgentID,
		AccountID: stored.AccountID,
		ClientID:  stored.ClientID,
		Scope:     stored.Scope,
	}
	// Atomic rotation: revoke old + create new in a single DB transaction.
	// RevokeIfActive (WHERE revoked = false) prevents concurrent rotation:
	// only one request flips the row, the other gets ErrNotFound (treated
	// as token reuse). If new token creation fails, the revocation is
	// rolled back so the client retains its valid refresh token.
	if err := refreshRepo.Rotate(ctx, stored.ID, newRefresh, newRawRefresh); err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.JSON(http.StatusBadRequest, tokenErrorResponse{
				Error: "invalid_grant",
			})
		}
		logger.Error(ctx, "oauth refresh: rotation failed",
			"token", stored.ID, "error", err)
		return c.JSON(http.StatusInternalServerError, tokenErrorResponse{
			Error: "server_error",
		})
	}

	logger.Info(ctx, "oauth access token refreshed",
		"agent", agent.GetID(), "client", stored.ClientID)

	return c.JSON(http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    accessTokenExpiresIn,
		RefreshToken: newRawRefresh,
		Scope:        stored.Scope,
	})
}
