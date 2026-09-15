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
	"context"
	"fmt"
	"time"
)

// A native sign-in — password or the trusted issuer's assertion — hands an app
// in a native shell a one-hour access token and no cookie it can use, so on its
// own the app is signed out every hour (wm-lnimb). The sign-in therefore hands
// back a refresh token as well, and the app renews at POST /api/auth/refresh.
//
// The refresh token is kept in the same store as a connector's: hashed, rotated
// on every use, and revoked with its whole family when a rotated one is
// presented again. It is told apart by NativeClientID, a client id no
// registration can have (registration mints a KSUID, which has no hyphen). The
// token endpoint refuses a native refresh token, and the native route refuses a
// connector's, so neither path can mint the other's kind of access token.
const (
	NativeClientID = "weos-native"
	// NativeRefreshTokenTTL is how long a native refresh token lasts. Each
	// renewal hands back a new one with the full lifetime, so a person who opens
	// the app at least once a month stays signed in, and a device left unused
	// for longer signs in again. It is the store's lifetime for a connector's
	// refresh token too.
	NativeRefreshTokenTTL = 30 * 24 * time.Hour
)

// NativeRefreshToken is a refresh token handed to a native app: the raw value,
// which the store never keeps, and when it stops renewing.
type NativeRefreshToken struct {
	Raw       string
	ExpiresAt time.Time
}

// IsNativeRefreshToken reports whether a stored refresh token was handed back by
// a native sign-in.
func IsNativeRefreshToken(token *OAuthRefreshToken) bool {
	return token != nil && token.ClientID == NativeClientID
}

// IssueNativeRefreshToken starts a new family of native refresh tokens for the
// person in the account.
func IssueNativeRefreshToken(
	ctx context.Context, repo RefreshTokenRepository, agentID, accountID string,
) (NativeRefreshToken, error) {
	raw, err := GenerateRefreshToken()
	if err != nil {
		return NativeRefreshToken{}, fmt.Errorf("native refresh token: generate: %w", err)
	}
	token := &OAuthRefreshToken{
		AgentID:   agentID,
		AccountID: accountID,
		ClientID:  NativeClientID,
		ExpiresAt: time.Now().Add(NativeRefreshTokenTTL),
	}
	if err := repo.Create(ctx, token, raw); err != nil {
		return NativeRefreshToken{}, fmt.Errorf("native refresh token: store: %w", err)
	}
	return NativeRefreshToken{Raw: raw, ExpiresAt: token.ExpiresAt}, nil
}

// RotateNativeRefreshToken replaces stored with a new refresh token in the same
// family, with the full lifetime, in one transaction. It answers ErrNotFound
// when stored was already rotated or revoked, which a concurrent renewal with
// the same token also gets.
func RotateNativeRefreshToken(
	ctx context.Context, repo RefreshTokenRepository, stored *OAuthRefreshToken,
) (NativeRefreshToken, error) {
	if !IsNativeRefreshToken(stored) {
		return NativeRefreshToken{}, fmt.Errorf("native refresh token: %w: not a native refresh token", ErrNotFound)
	}
	raw, err := GenerateRefreshToken()
	if err != nil {
		return NativeRefreshToken{}, fmt.Errorf("native refresh token: generate: %w", err)
	}
	next := &OAuthRefreshToken{
		AgentID:   stored.AgentID,
		AccountID: stored.AccountID,
		ClientID:  NativeClientID,
		FamilyID:  stored.FamilyID,
		ExpiresAt: time.Now().Add(NativeRefreshTokenTTL),
	}
	if err := repo.Rotate(ctx, stored.ID, next, raw); err != nil {
		return NativeRefreshToken{}, err
	}
	return NativeRefreshToken{Raw: raw, ExpiresAt: next.ExpiresAt}, nil
}
