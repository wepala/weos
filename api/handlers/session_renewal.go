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
// else holds a copy. The one exception is an exact repeat inside
// weosoauth.NativeRefreshGraceWindow (30 seconds) of a token whose successor is
// still live — a renewal whose answer was lost, or two renewals at once — which
// is answered that same successor and a fresh access token (wm-3dgs0). A store that cannot be read answers 503 with Retry-After,
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
	// presented is the row the refresh token names. stored is the row every
	// check below applies to: presented itself, or — when presented was spent
	// moments ago and is repeated inside the grace window — the successor its
	// renewal made, which is answered again (wm-3dgs0).
	presented := stored
	var graceRefresh weosoauth.NativeRefreshToken
	now := time.Now()
	if stored.Revoked {
		successor, refresh, inGrace, err := weosoauth.NativeRefreshSuccessorInGrace(ctx, tokens, stored, raw,
			h.cfg.RefreshSuccessorKey, now, weosoauth.NativeRefreshGraceWindow)
		if err != nil {
			h.cfg.Logger.Error(ctx, "session renewal: successor lookup failed", "token", stored.ID, "error", err)
			return renewalUnavailable(c, "could not read the refresh token")
		}
		if !inGrace {
			return h.refuseReusedRefreshToken(c, stored)
		}
		h.cfg.Logger.Info(ctx, "session renewal: a refresh token spent moments ago was repeated inside the grace window — answering its successor",
			"token", presented.ID, "family", presented.FamilyID)
		stored, graceRefresh = successor, refresh
	}
	if now.After(stored.ExpiresAt) {
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

	next := graceRefresh
	if next.Raw == "" {
		rotated, err := weosoauth.RotateNativeRefreshToken(ctx, tokens, presented, raw, h.cfg.RefreshSuccessorKey)
		if err == nil {
			next = rotated
			h.purgeNativeRefreshTokens(ctx)
		} else {
			// The rotation did not spend the token for this renewal: another
			// renewal with the same token spent it a moment ago, or the store
			// failed — a busy or locked database — and rolled back. The token is
			// read again to tell which, and the answer is never 500, which an app
			// would retry with a token it cannot tell was spent (wm-tu180).
			again, lookupErr := tokens.FindByTokenHash(ctx, weosoauth.HashToken(raw))
			switch {
			case errors.Is(lookupErr, weosoauth.ErrNotFound):
				return refuseRenewal(c, CodeInvalidRefreshToken)
			case lookupErr == nil && !again.Revoked:
				// Nobody spent it, so nothing was spent: the same renewal can be
				// sent again.
				h.cfg.Logger.Error(ctx, "session renewal: rotation failed and nothing was spent",
					"token", presented.ID, "error", err)
				return renewalUnavailable(c, "could not renew the session")
			}
			// Another renewal spent it. Inside the grace window that renewal's
			// successor is this one's too. Otherwise — the successor was made
			// under another process's key, or was already spent — the token was
			// reused, exactly as a spent token read above was, and its family is
			// revoked. A renewal that lost a race in this process always finds
			// the successor, because a rotation spends the token and saves its
			// successor in one transaction.
			inGrace := false
			if lookupErr == nil {
				_, next, inGrace, lookupErr = weosoauth.NativeRefreshSuccessorInGrace(ctx, tokens, again, raw,
					h.cfg.RefreshSuccessorKey, now, weosoauth.NativeRefreshGraceWindow)
			}
			if lookupErr != nil {
				h.cfg.Logger.Error(ctx, "session renewal: reading the refresh token after its rotation failed did not work",
					"token", presented.ID, "error", lookupErr)
				return renewalUnavailable(c, "could not read the refresh token")
			}
			if !inGrace {
				return h.refuseReusedRefreshToken(c, again)
			}
		}
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

// refuseReusedRefreshToken refuses spent, a native refresh token presented
// again outside the grace window, and revokes its whole family: someone else
// holds a copy.
func (h *PasswordAuthHandler) refuseReusedRefreshToken(c echo.Context, spent *weosoauth.OAuthRefreshToken) error {
	ctx := c.Request().Context()
	h.cfg.Logger.Warn(ctx, "session renewal: a spent refresh token was presented again — revoking its family",
		"token", spent.ID, "family", spent.FamilyID, "agent", spent.AgentID)
	if err := h.cfg.RefreshTokens.RevokeFamily(ctx, spent.FamilyID); err != nil {
		// The presented token is refused either way; what could not be revoked
		// is the newest token of the family, which the next reuse tries again.
		h.cfg.Logger.Error(ctx, "session renewal: family revocation failed",
			"family", spent.FamilyID, "error", err)
	}
	return refuseRenewal(c, CodeInvalidRefreshToken)
}

// nativeSessionBody is what a renewal's or a native sign-out's JSON body may
// carry. Everywhere is read by sign-out only.
type nativeSessionBody struct {
	RefreshToken string `json:"refresh_token"`
	Everywhere   bool   `json:"everywhere"`
}

// Codes a native sign-out's answer carries when it did not do all it was asked
// (wm-ehtnq). The sign-out itself still succeeded for the browser session.
const (
	// CodeAppSessionNotIdentified: the request presented a credential, but no
	// native session could be identified from it, so none was ended. An app
	// holding a refresh token signs out again with it in the body.
	CodeAppSessionNotIdentified = "app_session_not_identified"
	// CodeSignOutEverywhereRefused: "everywhere":true was asked without a live
	// credential. The device's own session was still ended.
	CodeSignOutEverywhereRefused = "sign_out_everywhere_refused"
	// CodeAppSessionNotEnded: the refresh token store could not be read or
	// written, so no app session was ended (wm-5rziu). The browser session was.
	// The app keeps its refresh token and signs out again.
	CodeAppSessionNotEnded = "app_session_not_ended"
)

// What a native sign-out's answer says, in app_session, it did to the app's
// sessions.
const (
	appSessionEnded           = "ended"
	appSessionEndedEverywhere = "ended_everywhere"
	appSessionNotIdentified   = "not_identified"
	appSessionNotEnded        = "not_ended"
)

// nativeSignOut is what a native sign-out did, as its answer says it. The zero
// value is a sign-out that presented no app credential — a browser's — whose
// answer says nothing about app sessions.
type nativeSignOut struct {
	AppSession string
	Code       string
}

// endNativeSessions ends the native sessions a sign-out names (wm-utb5c).
//
// By default that is one session, the device's own: the refresh token family
// of the refresh token in the JSON body, when it is a native one, and the
// family the bearer token names, when this instance signed it and it is not a
// connector's — expired or not (wm-ehtnq). A refresh token that can no longer
// renew — spent, revoked or expired — still ends its own family, as a renewal
// with a spent one does, and nothing else.
//
// {"everywhere":true} ends every native session of the person, on every device
// and in every account, and only with a live credential: a refresh token that
// can still renew, or a bearer token that validates now. Otherwise it ends only
// what the default would, and says everywhere was refused. A request that
// presents no credential — a browser's, with only its cookie — ends nothing and
// says nothing.
func (h *PasswordAuthHandler) endNativeSessions(c echo.Context) (nativeSignOut, error) {
	body, _ := readNativeSessionBody(c)
	bearer := bearerToken(c.Request())
	if h.cfg.RefreshTokens == nil || (bearer == "" && body.RefreshToken == "" && !body.Everywhere) {
		return nativeSignOut{}, nil
	}
	ctx := c.Request().Context()
	families := map[string]struct{}{}
	everywhere := map[string]struct{}{}

	if bearer != "" {
		if claims, live := h.signOutBearer(c, bearer); claims != nil {
			if family := weosoauth.NativeSessionOf(claims); family != "" {
				families[family] = struct{}{}
			}
			if body.Everywhere && live {
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
			return nativeSignOut{}, err
		}
	}

	for agentID := range everywhere {
		if err := h.cfg.RefreshTokens.RevokeForAgent(ctx, agentID, weosoauth.NativeClientID); err != nil {
			return nativeSignOut{}, err
		}
	}
	for family := range families {
		if err := h.cfg.RefreshTokens.RevokeFamily(ctx, family); err != nil {
			return nativeSignOut{}, err
		}
	}

	var did nativeSignOut
	switch {
	case len(everywhere) > 0:
		did.AppSession = appSessionEndedEverywhere
	case len(families) > 0:
		did.AppSession = appSessionEnded
	default:
		did.AppSession, did.Code = appSessionNotIdentified, CodeAppSessionNotIdentified
	}
	if body.Everywhere && len(everywhere) == 0 && did.Code == "" {
		did.Code = CodeSignOutEverywhereRefused
	}
	return did, nil
}

// signOutBearer reads the bearer token a sign-out presents: its claims, and
// whether it is live, validating now. A token this instance signed whose hour
// has passed still names its session, and is not live. A connector's token, a
// token whose signature does not verify, and a token naming nobody name
// nothing.
func (h *PasswordAuthHandler) signOutBearer(c echo.Context, token string) (_ *authapp.PericarpClaims, live bool) {
	if h.cfg.JWTService == nil {
		return nil, false
	}
	claims, err := h.cfg.JWTService.ValidateToken(c.Request().Context(), token)
	live = err == nil
	if errors.Is(err, authapp.ErrTokenExpired) {
		claims, err = weosoauth.SignedClaimsIgnoringExpiry(h.cfg.JWTService, token)
	}
	if err != nil || claims == nil || claims.AgentID == "" || weosoauth.IssuedToConnector(claims) {
		return nil, false
	}
	return claims, live
}

// signOutRecorder holds pericarp's sign-out answer so what happened to the
// app's session can be added to it. Headers — the cookies the sign-out clears
// — go straight to the response.
type signOutRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *signOutRecorder) Header() http.Header { return r.header }

func (r *signOutRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *signOutRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(p)
}

// answerNativeSignOut sends pericarp's recorded sign-out answer with app_session
// and code added. The route's answer is pericarp's flat object, not the
// envelope, as it has always been; the fields are added to that object so the
// route keeps one shape. An answer that is not a 200 JSON object is sent as it
// was.
func answerNativeSignOut(c echo.Context, recorded *signOutRecorder, did nativeSignOut) error {
	status := recorded.status
	if status == 0 {
		status = http.StatusOK
	}
	answer := map[string]any{}
	if recorded.body.Len() > 0 {
		if err := json.Unmarshal(recorded.body.Bytes(), &answer); err != nil {
			answer = nil
		}
	}
	if status != http.StatusOK || answer == nil {
		c.Response().WriteHeader(status)
		_, err := c.Response().Write(recorded.body.Bytes())
		return err
	}
	answer["app_session"] = did.AppSession
	if did.Code != "" {
		answer["code"] = did.Code
	}
	return c.JSON(status, answer)
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
