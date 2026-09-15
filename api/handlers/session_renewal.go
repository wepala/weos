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
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	apimw "github.com/wepala/weos/v3/api/middleware"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

// CodeInvalidRefreshToken is the code a renewal is refused with when the
// refresh token cannot renew anything: unknown, expired, already used, revoked,
// or not a native sign-in's. The app signs in again.
const CodeInvalidRefreshToken = "invalid_refresh_token"

// RefreshBodyLimit is the largest request body POST /auth/refresh reads, 16 KiB.
// A renewal is a few dozen bytes; the route is public.
const RefreshBodyLimit = 16 << 10

// renewalRetryAfter is the Retry-After, in seconds, on a 503 for a store that
// could not be read or written — the bearer path's own back-off.
const renewalRetryAfter = "5"

// sessionRenewalResponse is what a renewal answers with: the new access token,
// the refresh token that replaces the one presented, when each expires, and the
// account they are scoped to. ErasurePending and Code say, as a sign-in's answer
// does, that the account's deletion is unfinished and the token serves only
// DELETE /api/account.
type sessionRenewalResponse struct {
	Account               authAccountResponse `json:"account"`
	Token                 string              `json:"token"`
	TokenExpiresAt        time.Time           `json:"token_expires_at,omitzero"`
	RefreshToken          string              `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time           `json:"refresh_token_expires_at"`
	ErasurePending        bool                `json:"erasure_pending,omitempty"`
	Code                  string              `json:"code,omitempty"`
}

// MountSessionRenewal registers POST /auth/refresh when h can renew a native
// session, and reports whether it did. serve mounts it wherever a native
// sign-in is mounted.
func MountSessionRenewal(g *echo.Group, h *PasswordAuthHandler) bool {
	if !h.renews() {
		return false
	}
	g.POST("/auth/refresh", h.Refresh)
	return true
}

// renews reports whether the handler has everything a renewal reads. A sign-in
// hands back a refresh token only when it does, so no app holds one that
// cannot be used.
func (h *PasswordAuthHandler) renews() bool {
	return h.cfg.RefreshTokens != nil && h.cfg.AgentRepo != nil &&
		h.cfg.AccountRepo != nil && h.cfg.ErasureLocks != nil
}

// Refresh renews a native session: POST /auth/refresh with the body
// {"refresh_token":"..."}. It answers a new access token and a new refresh
// token, and the one presented is spent.
//
// It refuses what the bearer path refuses, with the code that path gives: a
// person no longer in the account (account_access_revoked, and the refresh
// token is revoked so adding them back does not revive it), a suspended account
// (account_deactivated), and an account that is gone. An account whose deletion
// is unfinished renews only for an owner or admin, whose new token serves the
// deletion alone, as a sign-in to it does (account_erasure_pending otherwise).
// A refresh token that was already rotated revokes its whole family: someone
// else holds a copy. A store that cannot be read answers 503 with Retry-After,
// and spends nothing.
func (h *PasswordAuthHandler) Refresh(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	ctx := c.Request().Context()

	raw, tooLarge := readRefreshToken(c)
	if tooLarge {
		return respondError(c, http.StatusRequestEntityTooLarge, "the request body is larger than a renewal can be")
	}
	if raw == "" {
		return respondErrorCode(c, http.StatusBadRequest, "refresh_token is required", "invalid_request")
	}

	tokens := h.cfg.RefreshTokens
	stored, err := tokens.FindByTokenHash(ctx, weosoauth.HashToken(raw))
	if err != nil && !errors.Is(err, weosoauth.ErrNotFound) {
		h.cfg.Logger.Error(ctx, "session renewal: refresh token lookup failed",
			"token_hash", weosoauth.MaskCode(raw), "error", err)
		return renewalUnavailable(c, "could not read the refresh token")
	}
	if err != nil || !weosoauth.IsNativeRefreshToken(stored) {
		return refuseRenewal(c, CodeInvalidRefreshToken)
	}
	if stored.Revoked {
		h.cfg.Logger.Warn(ctx, "session renewal: a spent refresh token was presented again — revoking its family",
			"token", stored.ID, "family", stored.FamilyID, "agent", stored.AgentID)
		if err := tokens.RevokeFamily(ctx, stored.FamilyID); err != nil {
			// The presented token is refused either way; what could not be
			// revoked is the newest token of the family, which the next reuse
			// tries again.
			h.cfg.Logger.Error(ctx, "session renewal: family revocation failed",
				"family", stored.FamilyID, "error", err)
		}
		return refuseRenewal(c, CodeInvalidRefreshToken)
	}
	if time.Now().After(stored.ExpiresAt) {
		return refuseRenewal(c, CodeInvalidRefreshToken)
	}

	account, err := h.cfg.AccountRepo.FindByID(ctx, stored.AccountID)
	if err != nil {
		h.cfg.Logger.Error(ctx, "session renewal: account lookup failed", "account", stored.AccountID, "error", err)
		return renewalUnavailable(c, "could not read the account's state")
	}
	if account == nil {
		// A finished deletion removes the account's refresh tokens with it; one
		// that is left names nothing any more.
		if err := tokens.Revoke(ctx, stored.ID); err != nil {
			h.cfg.Logger.Warn(ctx, "session renewal: could not revoke a refresh token for an account that is gone",
				"token", stored.ID, "error", err)
		}
		return refuseRenewal(c, CodeInvalidRefreshToken)
	}

	role, err := h.cfg.AccountRepo.FindMemberRole(ctx, stored.AccountID, stored.AgentID)
	if err != nil {
		h.cfg.Logger.Error(ctx, "session renewal: membership lookup failed",
			"agent", stored.AgentID, "account", stored.AccountID, "error", err)
		return renewalUnavailable(c, "could not read the account's state")
	}
	if role == "" {
		// Revoked before the refusal is final, as the refresh grant does: a
		// token left live would renew again if the person were added back.
		if err := tokens.Revoke(ctx, stored.ID); err != nil {
			h.cfg.Logger.Error(ctx, "session renewal: revoking a removed member's refresh token failed",
				"token", stored.ID, "error", err)
			return renewalUnavailable(c, "could not end the session")
		}
		h.cfg.Logger.Warn(ctx, "session renewal: the person is no longer a member of the account — refresh token revoked",
			"agent", stored.AgentID, "account", stored.AccountID)
		return refuseRenewal(c, apimw.CodeAccountAccessRevoked)
	}

	erasurePending := false
	if !account.Active() {
		locked, err := h.cfg.ErasureLocks.IsLocked(ctx, stored.AccountID)
		if err != nil {
			h.cfg.Logger.Error(ctx, "session renewal: erasure lock lookup failed", "account", stored.AccountID, "error", err)
			return renewalUnavailable(c, "could not read the account's state")
		}
		if !locked {
			return refuseRenewal(c, apimw.CodeAccountDeactivated)
		}
		// The rule a sign-in to a locked account applies (apimw.LockedAccountFor):
		// only a person who may finish the deletion keeps a token for it.
		if role != authentities.RoleOwner && role != authentities.RoleAdmin {
			return refuseRenewal(c, apimw.CodeAccountErasurePending)
		}
		erasurePending = true
	}

	agent, err := h.cfg.AgentRepo.FindByID(ctx, stored.AgentID)
	if err != nil {
		h.cfg.Logger.Error(ctx, "session renewal: agent lookup failed", "agent", stored.AgentID, "error", err)
		return renewalUnavailable(c, "could not read the person")
	}
	if agent == nil {
		return refuseRenewal(c, CodeInvalidRefreshToken)
	}

	// AccountAlreadyVerified is safe here for the reason it is safe at sign-in:
	// the membership and the account's state were read from the store in this
	// same request, just above. The token is issued exactly as a sign-in issues
	// one, so it carries no connector mark and the protected API takes it, and
	// it names the session it renews, so a sign-out with it ends that session.
	token, err := h.cfg.AuthService.IssueIdentityToken(weosoauth.WithNativeSession(ctx, stored.FamilyID),
		agent, stored.AccountID, authapp.AccountAlreadyVerified())
	if err != nil || token == "" {
		h.cfg.Logger.Error(ctx, "session renewal: access token issuance failed", "agent", stored.AgentID, "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to renew the session")
	}

	next, err := weosoauth.RotateNativeRefreshToken(ctx, tokens, stored)
	if err != nil {
		if errors.Is(err, weosoauth.ErrNotFound) {
			// Another renewal spent the token a moment ago.
			return refuseRenewal(c, CodeInvalidRefreshToken)
		}
		h.cfg.Logger.Error(ctx, "session renewal: rotation failed", "token", stored.ID, "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to renew the session")
	}

	h.cfg.Logger.Info(ctx, "native session renewed", "agent", stored.AgentID, "account", stored.AccountID)
	answer := sessionRenewalResponse{
		Account:               authAccountResponse{ID: account.GetID(), Name: account.Name()},
		Token:                 token,
		TokenExpiresAt:        tokenExpiry(token),
		RefreshToken:          next.Raw,
		RefreshTokenExpiresAt: next.ExpiresAt,
	}
	if erasurePending {
		answer.ErasurePending = true
		answer.Code = apimw.CodeAccountErasurePending
	}
	return respond(c, http.StatusOK, answer)
}

// nativeSessionBody is what a renewal's or a native sign-out's JSON body may
// carry. Everywhere is read by sign-out only.
type nativeSessionBody struct {
	RefreshToken string `json:"refresh_token"`
	Everywhere   bool   `json:"everywhere"`
}

// endNativeSessions ends the native sessions a sign-out names (wm-utb5c).
//
// By default that is one session, the device's own: the refresh token family
// of the refresh token in the JSON body, when it is a native one, and the
// family the bearer token names, when it validates and is not a connector's.
// A refresh token that can no longer renew — spent, revoked or expired — still
// ends its own family, as a renewal with a spent one does, and nothing else.
//
// {"everywhere":true} ends every native session of the person, on every device
// and in every account, and only with a live credential: a refresh token that
// can still renew, or a bearer token that validates. Otherwise it ends only
// what the default would. A request that names nothing — a browser's, with
// only its cookie — ends nothing.
func (h *PasswordAuthHandler) endNativeSessions(c echo.Context) error {
	if h.cfg.RefreshTokens == nil {
		return nil
	}
	ctx := c.Request().Context()
	body, _ := readNativeSessionBody(c)
	families := map[string]struct{}{}
	everywhere := map[string]struct{}{}

	if token := bearerToken(c.Request()); token != "" && h.cfg.JWTService != nil {
		// A token that does not validate names nothing here, and sign-out still
		// ends what it can. A connector's token is not the app's: it does not
		// sign the person out of their app.
		claims, err := h.cfg.JWTService.ValidateToken(ctx, token)
		if err == nil && claims.AgentID != "" && !weosoauth.IssuedToConnector(claims) {
			if family := weosoauth.NativeSessionOf(claims); family != "" {
				families[family] = struct{}{}
			}
			if body.Everywhere {
				everywhere[claims.AgentID] = struct{}{}
			}
		}
	}
	if body.RefreshToken != "" {
		stored, err := h.cfg.RefreshTokens.FindByTokenHash(ctx, weosoauth.HashToken(body.RefreshToken))
		switch {
		case err == nil:
			if weosoauth.IsNativeRefreshToken(stored) {
				families[stored.FamilyID] = struct{}{}
				if body.Everywhere && !stored.Revoked && time.Now().Before(stored.ExpiresAt) {
					everywhere[stored.AgentID] = struct{}{}
				}
			}
		case !errors.Is(err, weosoauth.ErrNotFound):
			return err
		}
	}

	for agentID := range everywhere {
		if err := h.cfg.RefreshTokens.RevokeForAgent(ctx, agentID, weosoauth.NativeClientID); err != nil {
			return err
		}
	}
	for family := range families {
		if err := h.cfg.RefreshTokens.RevokeFamily(ctx, family); err != nil {
			return err
		}
	}
	return nil
}

// readRefreshToken reads refresh_token from a JSON request body of at most
// RefreshBodyLimit bytes. It answers "" for a body that carries none, and
// tooLarge for a body over the limit.
func readRefreshToken(c echo.Context) (raw string, tooLarge bool) {
	body, tooLarge := readNativeSessionBody(c)
	return body.RefreshToken, tooLarge
}

// readNativeSessionBody reads a JSON request body of at most RefreshBodyLimit
// bytes. A body that is empty or not JSON carries nothing; tooLarge reports a
// body over the limit.
func readNativeSessionBody(c echo.Context) (_ nativeSessionBody, tooLarge bool) {
	r := c.Request()
	if r.Body == nil {
		return nativeSessionBody{}, false
	}
	if r.ContentLength > RefreshBodyLimit {
		return nativeSessionBody{}, true
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Response(), r.Body, RefreshBodyLimit))
	if err != nil {
		var over *http.MaxBytesError
		return nativeSessionBody{}, errors.As(err, &over)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nativeSessionBody{}, false
	}
	var body nativeSessionBody
	if err := json.Unmarshal(raw, &body); err != nil {
		// A body that is not JSON carries nothing.
		return nativeSessionBody{}, false
	}
	body.RefreshToken = strings.TrimSpace(body.RefreshToken)
	return body, false
}

// bearerToken is the token in the request's Authorization header, or "".
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}

// tokenExpiry is when an access token this process just issued expires, or the
// zero time when it cannot be read. The token is parsed without checking its
// signature: it was signed a moment ago, in this request, and only its exp is
// read, to tell the app when to renew.
func tokenExpiry(token string) time.Time {
	if token == "" {
		return time.Time{}
	}
	claims := &gojwt.RegisteredClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(token, claims); err != nil || claims.ExpiresAt == nil {
		return time.Time{}
	}
	return claims.ExpiresAt.Time
}

// refuseRenewal answers a renewal that cannot be made: 401 with the code that
// says why. The app signs in again.
func refuseRenewal(c echo.Context, code string) error {
	return respondErrorCode(c, http.StatusUnauthorized, "the session cannot be renewed; sign in again", code)
}

// renewalUnavailable answers a request whose store could not be read or
// written: 503 with Retry-After. Nothing was spent, so the same request can be
// made again.
func renewalUnavailable(c echo.Context, msg string) error {
	c.Response().Header().Set("Retry-After", renewalRetryAfter)
	return respondError(c, http.StatusServiceUnavailable, msg)
}
