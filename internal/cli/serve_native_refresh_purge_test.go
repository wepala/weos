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

package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// wm-sa7wv. Native refresh token rows more than the purge horizon past their
// expiry are purged where native refresh tokens are written: at a native
// sign-in, and at a renewal. A row inside the horizon stays.

// putAgedNativeRefreshTokens stores two native refresh tokens a long-gone
// session left: one past its expiry by more than the purge horizon, one inside
// it.
func putAgedNativeRefreshTokens(t *testing.T, repo weosoauth.RefreshTokenRepository) {
	t.Helper()
	now := time.Now()
	for raw, expiresAt := range map[string]time.Time{
		"left-by-a-session-long-over": now.Add(-weosoauth.NativeRefreshTokenPurgeHorizon - time.Hour),
		"expired-inside-the-horizon":  now.Add(-weosoauth.NativeRefreshTokenPurgeHorizon + time.Hour),
	} {
		token := &weosoauth.OAuthRefreshToken{
			AgentID: "agent-gone", AccountID: "acct-gone", ClientID: weosoauth.NativeClientID, ExpiresAt: expiresAt,
		}
		if err := repo.Create(context.Background(), token, raw); err != nil {
			t.Fatalf("store %s: %v", raw, err)
		}
	}
}

// requireAgedNativeRefreshTokensPurged checks that the row past the horizon is
// gone and the one inside it is not.
func requireAgedNativeRefreshTokensPurged(t *testing.T, repo weosoauth.RefreshTokenRepository, what string) {
	t.Helper()
	for raw, wantKept := range map[string]bool{"left-by-a-session-long-over": false, "expired-inside-the-horizon": true} {
		_, err := repo.FindByTokenHash(context.Background(), weosoauth.HashToken(raw))
		if err != nil && !errors.Is(err, weosoauth.ErrNotFound) {
			t.Fatalf("read %s: %v", raw, err)
		}
		if kept := err == nil; kept != wantKept {
			t.Errorf("after %s, %s kept = %v, want %v", what, raw, kept, wantKept)
		}
	}
}

func TestServe_NativeRefreshTokensPastTheHorizonArePurgedWhereTokensAreWritten(t *testing.T) {
	t.Run("at a native sign-in", func(t *testing.T) {
		door := newBootDoor(t)
		var db *gorm.DB
		srv := bootServe(t, trustedIssuerConfig(door), fx.Populate(&db))
		repo := weosoauth.NewRefreshTokenRepository(db)
		putAgedNativeRefreshTokens(t, repo)

		app := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		requireAgedNativeRefreshTokensPurged(t, repo, "a native sign-in")
		decodeNativeSession(t, renewNativeSession(t, srv, app.refreshToken), "the new session's renewal")
	})

	t.Run("at a renewal", func(t *testing.T) {
		door := newBootDoor(t)
		var db *gorm.DB
		srv := bootServe(t, trustedIssuerConfig(door), fx.Populate(&db))
		repo := weosoauth.NewRefreshTokenRepository(db)

		// A browser sign-in writes no native refresh token, so the first native
		// refresh token written on this instance is the renewal's.
		browser := decodeNativeSession(t, serveCall(t, srv, "POST", "/api/auth/assert",
			door.assertionBodyFor(t, bootOwnerEmail, bootOwnerSubject, bootOwnerName), nil), "the browser sign-in")
		issued, err := weosoauth.IssueNativeRefreshToken(context.Background(), repo, browser.agentID, browser.accountID, "")
		if err != nil {
			t.Fatalf("issue a native refresh token for the person: %v", err)
		}
		putAgedNativeRefreshTokens(t, repo)

		decodeNativeSession(t, renewNativeSession(t, srv, issued.Raw), "the renewal")
		requireAgedNativeRefreshTokensPurged(t, repo, "a renewal")
	})
}
