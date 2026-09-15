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
	"net/http"
	"testing"
	"time"

	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// wm-3dgs0. A renewal whose answer is lost is sent again with the same refresh
// token. Inside the grace window the repeat gets the successor the first
// renewal made; outside it, the repeat is reuse and ends the session. No
// failure message prints a token or a refresh token.

// ageNativeRotation moves the time a spent refresh token was rotated back by
// age, as if the repeat came that much later.
func ageNativeRotation(t *testing.T, db *gorm.DB, raw string, age time.Duration) {
	t.Helper()
	result := db.Model(&weosoauth.OAuthRefreshToken{}).
		Where("token_hash = ?", weosoauth.HashToken(raw)).
		Update("rotated_at", time.Now().Add(-age))
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("age the rotation: %d rows, error %v", result.RowsAffected, result.Error)
	}
}

func TestServe_ARenewalRepeatedInsideTheGraceWindowGetsTheSameRefreshToken(t *testing.T) {
	door := newBootDoor(t)
	var db *gorm.DB
	srv := bootServe(t, trustedIssuerConfig(door), fx.Populate(&db))

	t.Run("a repeat inside the window gets the same refresh token and a working token", func(t *testing.T) {
		app := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		first := decodeNativeSession(t, renewNativeSession(t, srv, app.refreshToken), "the renewal whose answer was lost")
		repeat := decodeNativeSession(t, renewNativeSession(t, srv, app.refreshToken), "the same renewal sent again at once")
		if repeat.refreshToken != first.refreshToken {
			t.Fatal("the repeat got a refresh token other than the successor the first renewal made")
		}
		if got := serveRequest(t, srv, http.MethodGet, "/api/resource-types", "", repeat.token, nil); got.status != http.StatusOK {
			t.Fatalf("GET /api/resource-types with the repeat's token answered %d code %q, want 200", got.status, refusalCode(got.body))
		}
		next := decodeNativeSession(t, renewNativeSession(t, srv, repeat.refreshToken), "a renewal with the successor the repeat got")

		// The successor is spent now, so the first refresh token is reuse however
		// soon it comes, and ends the session.
		requireRenewalRefused(t, renewNativeSession(t, srv, app.refreshToken), "invalid_refresh_token",
			"the first refresh token after its successor was spent")
		requireRenewalRefused(t, renewNativeSession(t, srv, next.refreshToken), "invalid_refresh_token",
			"the session's newest refresh token after reuse was detected")
	})

	t.Run("a repeat outside the window ends the session", func(t *testing.T) {
		app := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		first := decodeNativeSession(t, renewNativeSession(t, srv, app.refreshToken), "the renewal")
		ageNativeRotation(t, db, app.refreshToken, weosoauth.NativeRefreshGraceWindow+time.Second)
		requireRenewalRefused(t, renewNativeSession(t, srv, app.refreshToken), "invalid_refresh_token",
			"a repeat outside the grace window")
		requireRenewalRefused(t, renewNativeSession(t, srv, first.refreshToken), "invalid_refresh_token",
			"the successor after a repeat outside the grace window")
	})

	t.Run("a repeat for a session that signed out gets nothing", func(t *testing.T) {
		app := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		first := decodeNativeSession(t, renewNativeSession(t, srv, app.refreshToken), "the renewal")
		signOutNatively(t, srv, "", map[string]any{"refresh_token": first.refreshToken})
		requireRenewalRefused(t, renewNativeSession(t, srv, app.refreshToken), "invalid_refresh_token",
			"a repeat inside the grace window for a session that signed out")
	})
}
