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
	"context"
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
// session, and reports whether it did. serve mounts it whenever renewal is
// possible, whichever sign-ins are on (wm-xbd5h): a downstream binary's own
// sign-in may start native sessions with password sign-in and the trusted
// issuer both off. Mounting it makes no sign-in reachable — the password
// routes are mounted by MountPasswordAuth alone — and it renews only refresh
// tokens a native sign-in issued.
func MountSessionRenewal(g *echo.Group, h *PasswordAuthHandler) bool {
	if !h.renews() {
		return false
	}
	g.POST("/auth/refresh", h.Refresh)
	return true
}

// renews reports whether the handler has everything a renewal reads, which is
// what its native session issuer has. A sign-in hands back a refresh token only
// when it does, so no app holds one that cannot be used.
func (h *PasswordAuthHandler) renews() bool {
	return h.cfg.NativeSessions.CanRenew()
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
	// The family's cap, read from the rest of the family when this row carries
	// none: a renewal a binary from before the cap served copied no cap onto
	// its successor (wm-5u9q7). A family whose cap has passed renews nothing,
	// whatever the row's own expiry says.
	familyCap, err := weosoauth.NativeFamilyCap(ctx, tokens, stored)
	if err != nil {
		h.cfg.Logger.Error(ctx, "session renewal: the refresh token family's cap could not be read",
			"token", stored.ID, "family", stored.FamilyID, "error", err)
		return renewalUnavailable(c, "could not read the refresh token")
	}
	var capAt time.Time
	if familyCap != nil {
		capAt = *familyCap
		if !now.Before(capAt) {
			return refuseRenewal(c, CodeInvalidRefreshToken)
		}
	}

	standing, err := h.sessionStandingOf(c, stored.AgentID, stored.AccountID)
	if err != nil {
		h.cfg.Logger.Error(ctx, "session renewal: the person's place in the account could not be read",
			"agent", stored.AgentID, "account", stored.AccountID, "error", err)
		return renewalUnavailable(c, standingUnreadable(err))
	}
	switch {
	case standing.accountGone:
		// A finished deletion removes the account's refresh tokens with it; one
		// that is left names nothing any more.
		if err := tokens.Revoke(ctx, stored.ID); err != nil {
			h.cfg.Logger.Warn(ctx, "session renewal: could not revoke a refresh token for an account that is gone",
				"token", stored.ID, "error", err)
		}
		return refuseRenewal(c, CodeInvalidRefreshToken)
	case standing.removed:
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
	case standing.refusal != "":
		return refuseRenewal(c, standing.refusal)
	}
	account, agent, erasurePending := standing.account, standing.agent, standing.erasurePending

	// AccountAlreadyVerified is safe here for the reason it is safe at sign-in:
	// the membership and the account's state were read from the store in this
	// same request, just above. The token is issued exactly as a sign-in issues
	// one, so it carries no connector mark and the protected API takes it, and
	// it names the session it renews, so a sign-out with it ends that session.
	// A capped session's access token ends at the cap when the cap comes
	// before its hour (wm-vdax2).
	issueCtx := weosoauth.WithNativeSession(ctx, stored.FamilyID)
	if !capAt.IsZero() {
		issueCtx = weosoauth.WithAccessTokenNotAfter(issueCtx, capAt)
	}
	token, err := h.cfg.AuthService.IssueIdentityToken(issueCtx, agent, stored.AccountID, authapp.AccountAlreadyVerified())
	if err != nil || token == "" {
		h.cfg.Logger.Error(ctx, "session renewal: access token issuance failed", "agent", stored.AgentID, "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to renew the session")
	}
	tokenExpiresAt := tokenExpiry(token)
	if accessTokenOutlivesCap(tokenExpiresAt, capAt) {
		// The token service did not end the token at the cap. Refused before
		// the rotation, so nothing is spent; handing the token back would let
		// the session last past its fixed end.
		h.cfg.Logger.Error(ctx, "session renewal: the access token would outlive the session's cap; the token service did not end it there, so nothing was renewed",
			"agent", stored.AgentID, "family", stored.FamilyID, "token_expires_at", tokenExpiresAt, "cap", capAt)
		return respondError(c, http.StatusInternalServerError, "failed to renew the session")
	}
	if !capAt.IsZero() && !time.Now().Before(capAt) {
		// The cap passed while the access token was signed. That token ends at
		// the cap, and so would any successor: the renewal is refused as one
		// after the cap is, before the rotation, so nothing is spent (PR 576
		// review).
		return refuseRenewal(c, CodeInvalidRefreshToken)
	}

	next := graceRefresh
	if next.Raw != "" {
		next = h.capGraceSuccessor(ctx, stored, next, familyCap)
	} else {
		// A row that lost its family's cap is rotated with the cap set again,
		// so the successor carries it and every later renewal keeps to it
		// (wm-5u9q7).
		rotateFrom := presented
		if presented.FamilyExpiresAt == nil && familyCap != nil {
			healed := *presented
			healed.FamilyExpiresAt = familyCap
			rotateFrom = &healed
			h.cfg.Logger.Warn(ctx, "session renewal: a refresh token carried no cap though its family has one — a binary from before the cap renewed it; the cap is written back on its successor",
				"token", presented.ID, "family", presented.FamilyID)
		}
		rotated, err := weosoauth.RotateNativeRefreshToken(ctx, tokens, rotateFrom, raw, h.cfg.RefreshSuccessorKey)
		switch {
		case err == nil:
			next = rotated
			h.purgeNativeRefreshTokens(ctx)
		case errors.Is(err, weosoauth.ErrNativeRefreshCapPassed):
			// The cap passed between the check above and the rotation, which
			// wrote nothing: refused as a renewal after the cap is.
			return refuseRenewal(c, CodeInvalidRefreshToken)
		default:
			// The rotation did not spend the token for this renewal: another
			// renewal with the same token spent it a moment ago, the token
			// expired while the rotation waited, or the store failed — a busy or
			// locked database — and rolled back. The token is read again to tell
			// which, and the answer is never 500, which an app would retry with a
			// token it cannot tell was spent (wm-tu180).
			again, lookupErr := tokens.FindByTokenHash(ctx, weosoauth.HashToken(raw))
			switch {
			case errors.Is(lookupErr, weosoauth.ErrNotFound):
				return refuseRenewal(c, CodeInvalidRefreshToken)
			case lookupErr == nil && !again.Revoked && !time.Now().Before(again.ExpiresAt):
				// Nothing was spent, and the token will never renew: sending the
				// same renewal again cannot help.
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
			var successor *weosoauth.OAuthRefreshToken
			if lookupErr == nil {
				successor, next, inGrace, lookupErr = weosoauth.NativeRefreshSuccessorInGrace(ctx, tokens, again, raw,
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
			next = h.capGraceSuccessor(ctx, successor, next, familyCap)
		}
	}

	h.cfg.Logger.Info(ctx, "native session renewed", "agent", stored.AgentID, "account", stored.AccountID)
	answer := sessionRenewalResponse{
		Account:               authAccountResponse{ID: account.GetID(), Name: account.Name()},
		Token:                 token,
		TokenExpiresAt:        tokenExpiresAt,
		RefreshToken:          next.Raw,
		RefreshTokenExpiresAt: next.ExpiresAt,
	}
	if erasurePending {
		answer.ErasurePending = true
		answer.Code = apimw.CodeAccountErasurePending
	}
	return respond(c, http.StatusOK, answer)
}

// capGraceSuccessor keeps a successor answered inside the grace window to its
// family's cap (familyCap; nil caps nothing). The rotation that made the
// successor may have been served by a binary from before the cap, which wrote it
// with no cap and the full 30 days: the answer then ends it at the cap, and the
// cap is written on the row, so the row ends there too and every later renewal
// reads the cap from it (wm-5u9q7). A write that fails leaves the answer capped
// and is logged: a renewal of the successor still reads the cap from the family.
func (h *PasswordAuthHandler) capGraceSuccessor(
	ctx context.Context, successor *weosoauth.OAuthRefreshToken, next weosoauth.NativeRefreshToken, familyCap *time.Time,
) weosoauth.NativeRefreshToken {
	if familyCap == nil {
		return next
	}
	if next.ExpiresAt.After(*familyCap) {
		next.ExpiresAt = *familyCap
	}
	if successor == nil || (successor.FamilyExpiresAt != nil && !successor.FamilyExpiresAt.After(*familyCap) &&
		!successor.ExpiresAt.After(*familyCap)) {
		return next
	}
	h.cfg.Logger.Warn(ctx, "session renewal: the successor answered inside the grace window carried no cap, or a later one, though its family has one — a binary from before the cap renewed it; the cap is written on it",
		"token", successor.ID, "family", successor.FamilyID)
	if err := h.cfg.RefreshTokens.CapExpiry(ctx, successor.ID, *familyCap); err != nil {
		h.cfg.Logger.Warn(ctx, "session renewal: could not write the family's cap on the grace successor; a renewal of it still reads the cap from the family",
			"token", successor.ID, "family", successor.FamilyID, "error", err)
	}
	return next
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

// sessionStanding is what the store says now of a person's place in the account
// a native credential names: what a renewal refuses, and what ending every
// native session of the person needs (wm-lnimb).
type sessionStanding struct {
	account        *authentities.Account
	agent          *authentities.Agent
	erasurePending bool
	// refusal is the code a renewal is refused with, "" when the person may
	// renew. accountGone and removed are the refusals whose refresh token a
	// renewal also revokes.
	refusal     string
	accountGone bool
	removed     bool
}

// standingReadError is a store that could not be read for a sessionStanding.
// what is what a 503 answer says could not be read.
type standingReadError struct {
	what string
	err  error
}

func (e *standingReadError) Error() string { return e.what + ": " + e.err.Error() }

func (e *standingReadError) Unwrap() error { return e.err }

// standingUnreadable is what a 503 answer says could not be read, for an error
// from sessionStandingOf.
func standingUnreadable(err error) string {
	var readErr *standingReadError
	if errors.As(err, &readErr) {
		return readErr.what
	}
	return "could not read the account's state"
}

// sessionStandingOf reads what a renewal checks of agentID in accountID, in the
// order a renewal refuses on: the account, the membership, the account's state,
// then the person. It stops at the first refusal.
func (h *PasswordAuthHandler) sessionStandingOf(c echo.Context, agentID, accountID string) (sessionStanding, error) {
	ctx := c.Request().Context()
	account, err := h.cfg.AccountRepo.FindByID(ctx, accountID)
	if err != nil {
		return sessionStanding{}, &standingReadError{what: "could not read the account's state", err: err}
	}
	if account == nil {
		return sessionStanding{refusal: CodeInvalidRefreshToken, accountGone: true}, nil
	}
	role, err := h.cfg.AccountRepo.FindMemberRole(ctx, accountID, agentID)
	if err != nil {
		return sessionStanding{}, &standingReadError{what: "could not read the account's state", err: err}
	}
	if role == "" {
		return sessionStanding{account: account, refusal: apimw.CodeAccountAccessRevoked, removed: true}, nil
	}

	standing := sessionStanding{account: account}
	if !account.Active() {
		locked, err := h.cfg.ErasureLocks.IsLocked(ctx, accountID)
		if err != nil {
			return sessionStanding{}, &standingReadError{what: "could not read the account's state", err: err}
		}
		if !locked {
			standing.refusal = apimw.CodeAccountDeactivated
			return standing, nil
		}
		// The rule a sign-in to a locked account applies (apimw.LockedAccountFor):
		// only a person who may finish the deletion keeps a token for it.
		if role != authentities.RoleOwner && role != authentities.RoleAdmin {
			standing.refusal = apimw.CodeAccountErasurePending
			return standing, nil
		}
		standing.erasurePending = true
	}

	agent, err := h.cfg.AgentRepo.FindByID(ctx, agentID)
	if err != nil {
		return sessionStanding{}, &standingReadError{what: "could not read the person", err: err}
	}
	if agent == nil {
		standing.refusal = CodeInvalidRefreshToken
		return standing, nil
	}
	standing.agent = agent
	return standing, nil
}

// mayStillRenew reports whether a renewal for agentID in accountID would go
// ahead now. A handler that cannot renew answers false.
func (h *PasswordAuthHandler) mayStillRenew(c echo.Context, agentID, accountID string) (bool, error) {
	if !h.renews() || agentID == "" || accountID == "" {
		return false, nil
	}
	standing, err := h.sessionStandingOf(c, agentID, accountID)
	if err != nil {
		return false, err
	}
	return standing.refusal == "", nil
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
// can still renew, or a bearer token that validates now. Either one counts only
// while a renewal for its person in its account would go ahead, so a removed
// member's or a suspended account's credential does not (wm-lnimb). Otherwise
// it ends only what the default would, and says everywhere was refused. A
// request that presents no credential — a browser's, with only its cookie —
// ends nothing and says nothing.
func (h *PasswordAuthHandler) endNativeSessions(c echo.Context) (nativeSignOut, error) {
	body, _ := readNativeSessionBody(c)
	bearer := bearerToken(c.Request())
	if h.cfg.RefreshTokens == nil || (bearer == "" && body.RefreshToken == "" && !body.Everywhere) {
		return nativeSignOut{}, nil
	}
	ctx := c.Request().Context()
	families := map[string]struct{}{}
	everywhere := map[string]struct{}{}
	endEverywhereIfTheyMayRenew := func(agentID, accountID string) error {
		mayRenew, err := h.mayStillRenew(c, agentID, accountID)
		if mayRenew {
			everywhere[agentID] = struct{}{}
		}
		return err
	}

	if bearer != "" {
		if claims, live := h.signOutBearer(c, bearer); claims != nil {
			if family := weosoauth.NativeSessionOf(claims); family != "" {
				families[family] = struct{}{}
			}
			if body.Everywhere && live {
				if err := endEverywhereIfTheyMayRenew(claims.AgentID, claims.ActiveAccountID); err != nil {
					return nativeSignOut{}, err
				}
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
					if err := endEverywhereIfTheyMayRenew(stored.AgentID, stored.AccountID); err != nil {
						return nativeSignOut{}, err
					}
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
