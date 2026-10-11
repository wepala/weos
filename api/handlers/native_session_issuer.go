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
	"fmt"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
)

// A native session is what a sign-in hands an app in a native shell, which
// holds no cookie: an access token that lasts an hour and names its session,
// and a refresh token that renews it at POST /api/auth/refresh (wm-lnimb,
// wm-utb5c). NativeSessionIssuer starts one. Password sign-in and the trusted
// issuer's sign-in start theirs through it, and so may a downstream binary's
// own sign-in (wm-xbd5h): serve provides the issuer in its fx graph as
// *NativeSessionIssuer, and mounts the renewal route whenever it CanRenew.

var (
	// ErrNativeSessionUnavailable is what IssueNativeSession answers when the
	// issuer cannot renew (see CanRenew). Nothing was issued.
	ErrNativeSessionUnavailable = errors.New("native session: this instance cannot renew a native session")
	// ErrNativeRefreshTokenNotIssued is what IssueNativeSession answers, wrapped
	// around the cause, when the access token was issued and the refresh token
	// was not. The answer still carries the access token, which is good for its
	// hour: a caller may hand it back alone, and the app signs in again when it
	// expires.
	ErrNativeRefreshTokenNotIssued = errors.New("native session: the refresh token was not issued")
	// ErrNativeRefreshCapPassed is what IssueNativeSession answers for a
	// WithRefreshExpiresAt time that is not in the future. Nothing was issued.
	ErrNativeRefreshCapPassed = weosoauth.ErrNativeRefreshCapPassed
)

// NativeSessionIssuerConfig is what a NativeSessionIssuer is built from. A
// downstream binary does not build one: it takes serve's from the fx graph.
type NativeSessionIssuerConfig struct {
	AuthService authapp.AuthenticationService
	// RefreshTokens is the store a native refresh token is kept, rotated and
	// revoked in.
	RefreshTokens weosoauth.RefreshTokenRepository
	// AgentRepo, AccountRepo and ErasureLocks are what a renewal reads to
	// decide whether the person may still renew. An issuer without them, or
	// without RefreshTokens, cannot renew, and issues nothing.
	AgentRepo    authrepos.AgentRepository
	AccountRepo  authrepos.AccountRepository
	ErasureLocks repositories.AccountErasureLocks
	Logger       entities.Logger
}

// NativeSessionIssuer starts native sessions. It is safe for concurrent use.
type NativeSessionIssuer struct {
	cfg NativeSessionIssuerConfig
	// purger removes native refresh token rows past the purge horizon when a
	// native refresh token is written (wm-sa7wv) — by a sign-in here, or by a
	// renewal. One per issuer, so one per process in serve. Nil without
	// RefreshTokens.
	purger *weosoauth.NativeRefreshTokenPurger
}

// NewNativeSessionIssuer builds an issuer from cfg.
func NewNativeSessionIssuer(cfg NativeSessionIssuerConfig) *NativeSessionIssuer {
	i := &NativeSessionIssuer{cfg: cfg}
	if cfg.RefreshTokens != nil {
		i.purger = weosoauth.NewNativeRefreshTokenPurger(cfg.RefreshTokens, nil)
	}
	return i
}

// CanRenew reports whether a native session this issuer starts can be renewed:
// it has the refresh token store and everything a renewal reads. A sign-in
// asks before it offers a native session, so no app is handed a refresh token
// it cannot use. A nil issuer cannot renew.
func (i *NativeSessionIssuer) CanRenew() bool {
	return i != nil && i.cfg.RefreshTokens != nil && i.cfg.AgentRepo != nil &&
		i.cfg.AccountRepo != nil && i.cfg.ErasureLocks != nil && i.cfg.AuthService != nil
}

// NativeSessionTokens is a native session as a sign-in hands it back. Its
// fields are the ones password sign-in answers with: token, token_expires_at,
// refresh_token and refresh_token_expires_at.
type NativeSessionTokens struct {
	// Token is the access token. It lasts an hour and names the session, so a
	// sign-out with it ends this session and no other.
	Token string
	// TokenExpiresAt is when Token stops being accepted, so the app knows when
	// to renew. Zero when it could not be read from Token.
	TokenExpiresAt time.Time
	// RefreshToken renews Token at POST /api/auth/refresh, once: each renewal
	// hands back the next one.
	RefreshToken string
	// RefreshTokenExpiresAt is when RefreshToken stops renewing.
	RefreshTokenExpiresAt time.Time
}

// NativeSessionOption changes how IssueNativeSession starts a session.
type NativeSessionOption func(*nativeSessionOptions)

type nativeSessionOptions struct {
	refreshCap time.Time
	trust      []authapp.AccountTrustOption
}

// WithRefreshExpiresAt caps the session: its refresh token expires at t, or
// after the full 30-day lifetime when that comes first, and no renewal hands
// back a refresh token that outlives t. A sign-in whose session has a fixed
// end passes that end, so renewing keeps the hour-long access token alive only
// inside the session. A zero t caps nothing; a t that is not in the future
// issues nothing (ErrNativeRefreshCapPassed).
func WithRefreshExpiresAt(t time.Time) NativeSessionOption {
	return func(o *nativeSessionOptions) { o.refreshCap = t }
}

// WithAccountTrust passes opts to the access token's issuance. Pass
// authapp.AccountAlreadyVerified() only when the account came back from the
// sign-in in this same request — VerifyPassword, FindOrCreateAgent — so the
// membership is established and the account is active by construction. Never
// pass it for an account a client named: it disables the membership and
// deactivation checks.
func WithAccountTrust(opts ...authapp.AccountTrustOption) NativeSessionOption {
	return func(o *nativeSessionOptions) { o.trust = append(o.trust, opts...) }
}

// IssueNativeSession starts a native session for agent in accountID: it picks
// the session's id, issues the access token naming it, and starts the session's
// refresh token family. The caller has signed the person in and settled the
// account; this hands back the credentials and sets no cookie.
//
// It answers ErrNativeSessionUnavailable when the issuer cannot renew, and an
// error with empty tokens when the access token could not be issued: nothing
// was issued then. When the access token was issued and the refresh token was
// not, it answers the access token alone and an error wrapping
// ErrNativeRefreshTokenNotIssued.
func (i *NativeSessionIssuer) IssueNativeSession(
	ctx context.Context, agent *authentities.Agent, accountID string, opts ...NativeSessionOption,
) (NativeSessionTokens, error) {
	if !i.CanRenew() {
		return NativeSessionTokens{}, ErrNativeSessionUnavailable
	}
	if agent == nil || accountID == "" {
		return NativeSessionTokens{}, errors.New("native session: a person and an account are required")
	}
	var o nativeSessionOptions
	for _, opt := range opts {
		opt(&o)
	}
	// Checked before anything is issued, so a cap that has passed issues
	// neither token.
	if !o.refreshCap.IsZero() && !o.refreshCap.After(time.Now()) {
		return NativeSessionTokens{}, ErrNativeRefreshCapPassed
	}

	// The session's id is chosen before its token is issued, so the token
	// names the session (weosoauth.NativeSessionClaim) and a sign-out with
	// only the token can end that session and no other (wm-utb5c).
	sessionID := weosoauth.NewNativeSessionID()
	token, err := i.cfg.AuthService.IssueIdentityToken(weosoauth.WithNativeSession(ctx, sessionID),
		agent, accountID, o.trust...)
	if err != nil {
		return NativeSessionTokens{}, fmt.Errorf("native session: issue the access token: %w", err)
	}
	if token == "" {
		return NativeSessionTokens{}, errors.New("native session: the authentication service issued no access token")
	}
	tokens := NativeSessionTokens{Token: token, TokenExpiresAt: tokenExpiry(token)}

	refresh, err := weosoauth.IssueNativeRefreshTokenUntil(ctx, i.cfg.RefreshTokens, agent.GetID(), accountID,
		sessionID, o.refreshCap)
	if err != nil {
		return tokens, fmt.Errorf("%w: %w", ErrNativeRefreshTokenNotIssued, err)
	}
	i.purge(ctx)
	tokens.RefreshToken, tokens.RefreshTokenExpiresAt = refresh.Raw, refresh.ExpiresAt
	return tokens, nil
}

// purge runs the native refresh token purge when it is due. It never fails the
// request that runs it.
func (i *NativeSessionIssuer) purge(ctx context.Context) {
	if i == nil {
		return
	}
	ran, purged, err := i.purger.MaybePurge(ctx)
	if i.cfg.Logger == nil {
		return
	}
	if err != nil {
		i.cfg.Logger.Warn(ctx, "native refresh tokens: purge failed; it runs again after the interval", "error", err)
		return
	}
	if ran && purged > 0 {
		i.cfg.Logger.Info(ctx, "native refresh tokens: purged rows past the horizon", "rows", purged)
	}
}
