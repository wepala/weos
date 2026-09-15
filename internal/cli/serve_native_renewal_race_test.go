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
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// wm-tu180. An app that fires several renewals as its hour ends sends the same
// refresh token several times at once. Eight at once, on serve's own routes and
// database: none answers 500, every answer carries the one successor the
// winning renewal made (the others repeat inside the grace window, wm-3dgs0),
// that successor renews, and the session's family is left with one live token.
// Run it under -race. No failure message prints a token or a refresh token.
func TestServe_EightRenewalsAtOnceWithOneRefreshTokenMakeOneSuccessor(t *testing.T) {
	const renewals = 8
	door := newBootDoor(t)
	var db *gorm.DB
	srv := bootServe(t, trustedIssuerConfig(door), fx.Populate(&db))
	app := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)

	body, err := json.Marshal(map[string]string{"refresh_token": app.refreshToken})
	if err != nil {
		t.Fatalf("encode the renewal: %v", err)
	}
	start := make(chan struct{})
	answers := make([]serveAnswer, renewals)
	var wg sync.WaitGroup
	for i := range renewals {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			answers[i] = serveCall(t, srv, http.MethodPost, "/api/auth/refresh", string(body), nil)
		}()
	}
	close(start)
	wg.Wait()

	successors := map[string]struct{}{}
	for i, answer := range answers {
		if answer.status != http.StatusOK {
			t.Errorf("renewal %d of %d answered %d code %q, want 200 with the one successor",
				i+1, renewals, answer.status, refusalCode(answer.body))
			continue
		}
		successors[decodeNativeSession(t, answer, "a renewal in the race").refreshToken] = struct{}{}
	}
	if t.Failed() {
		t.FailNow()
	}
	if len(successors) != 1 {
		t.Fatalf("the race handed out %d different refresh tokens, want 1", len(successors))
	}

	var live int64
	if err := db.Model(&weosoauth.OAuthRefreshToken{}).
		Where("token_hash = ? AND revoked = ?", weosoauth.HashToken(app.refreshToken), false).Count(&live).Error; err != nil {
		t.Fatalf("read the presented refresh token: %v", err)
	}
	if live != 0 {
		t.Fatal("the presented refresh token is still live after the race")
	}
	for successor := range successors {
		decodeNativeSession(t, renewNativeSession(t, srv, successor), "a renewal with the one successor the race made")
	}
}
