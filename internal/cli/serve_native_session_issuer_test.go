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
	"net/http"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"go.uber.org/fx"
)

// wm-xbd5h. A downstream binary's own sign-in — crossdeck's Mastodon and
// Bluesky, on an instance whose only core sign-in is Google — must hand back a
// native session the way password sign-in does. serve's fx graph provides the
// issuer, so a binary reaches it from an fx.Invoke it registers, and serve
// mounts POST /api/auth/refresh whenever the issuer can renew, with password
// sign-in and the trusted issuer both off. Nothing else about password sign-in
// becomes reachable. These tests boot what serve boots. No failure message
// prints a token or a refresh token.

// googleOnlyConfig is an instance whose only core sign-in is Google: password
// sign-in off, no trusted issuer. crossdeck's production instance has this
// shape, with its own sign-ins added by the binary.
func googleOnlyConfig() config.Config {
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.OAuth.GoogleClientID = "harbor-legal.apps.googleusercontent.example"
	cfg.OAuth.GoogleClientSecret = "a-google-client-secret-for-this-test"
	return cfg
}

// A binary's fx.Invoke — what weoscli.RegisterFxOptions merges into serve's
// graph — receives serve's issuer, ready to renew.
func TestServe_TheGraphProvidesANativeSessionIssuerThatCanRenew(t *testing.T) {
	var captured *handlers.NativeSessionIssuer
	bootServe(t, googleOnlyConfig(), fx.Invoke(func(issuer *handlers.NativeSessionIssuer) {
		captured = issuer
	}))
	if captured == nil {
		t.Fatal("an fx.Invoke merged into serve's graph received no *handlers.NativeSessionIssuer")
	}
	if !captured.CanRenew() {
		t.Fatal("serve's native session issuer says it cannot renew")
	}
}

// With password sign-in and the trusted issuer off, the renewal route is
// mounted, and neither password route is: both answer exactly like a path the
// instance has never had.
func TestServe_TheRenewalRouteIsMountedWithPasswordSignInOff(t *testing.T) {
	srv := bootServe(t, googleOnlyConfig())

	missing := serveCall(t, srv, http.MethodPost, "/api/auth/refresh", `{}`, nil)
	if missing.status != http.StatusBadRequest || refusalCode(missing.body) != "invalid_request" {
		t.Fatalf("POST /api/auth/refresh with no refresh token answered %d code %q, want 400 invalid_request: the route is not mounted",
			missing.status, refusalCode(missing.body))
	}
	requireRenewalRefused(t, renewNativeSession(t, srv, "not-a-refresh-token-this-instance-issued"),
		"invalid_refresh_token", "a renewal with a refresh token the instance never issued")

	credentials := `{"email":"` + bootPasswordEmail + `","password":"` + bootPasswordSecret + `"}`
	never := serveCall(t, srv, http.MethodPost, "/api/auth/enroll", credentials, nil)
	for _, path := range []string{"/api/auth/password-login", "/api/auth/register"} {
		got := serveCall(t, srv, http.MethodPost, path, credentials, nil)
		if got.status != never.status || got.body != never.body ||
			got.header.Get("WWW-Authenticate") != never.header.Get("WWW-Authenticate") {
			t.Fatalf("POST %s answered %d, a path the instance never had %d: with password sign-in off it must not be mounted",
				path, got.status, never.status)
		}
	}
}

// A downstream sign-in's native session, capped to its 24-hour session, renews
// at serve's route with password sign-in off, and no renewal outlives the cap.
// The renewed access token is a native one: it reads the account it names.
func TestServe_ADownstreamSignInsCappedSessionRenewsOnlyInsideTheSession(t *testing.T) {
	var issuer *handlers.NativeSessionIssuer
	var auth authapp.AuthenticationService
	srv := bootServe(t, googleOnlyConfig(), fx.Populate(&issuer, &auth))
	ctx := context.Background()

	agent, _, account, err := auth.FindOrCreateAgent(ctx, authapp.UserInfo{
		ProviderUserID: "109876543210", Provider: "mastodon", DisplayName: "Dana Whitfield",
	})
	if err != nil || agent == nil || account == nil {
		t.Fatalf("sign the person in as a downstream binary does: agent %v account %v err %v", agent != nil, account != nil, err)
	}
	capAt := time.Now().Add(24 * time.Hour)
	tokens, err := issuer.IssueNativeSession(ctx, agent, account.GetID(),
		handlers.WithRefreshExpiresAt(capAt), handlers.WithAccountTrust(authapp.AccountAlreadyVerified()))
	if err != nil {
		t.Fatalf("issue the downstream sign-in's native session: %v", err)
	}
	if tokens.Token == "" || tokens.RefreshToken == "" {
		t.Fatalf("the native session carries token %v and refresh token %v, want both", tokens.Token != "", tokens.RefreshToken != "")
	}
	if !nearly(tokens.RefreshTokenExpiresAt, capAt) {
		t.Fatalf("the refresh token expires at %v, want the session's end %v", tokens.RefreshTokenExpiresAt, capAt)
	}

	refresh := tokens.RefreshToken
	for i := range 2 {
		renewed := decodeNativeSession(t, renewNativeSession(t, srv, refresh), "a renewal of the downstream sign-in's session")
		if renewed.refreshToken == "" || renewed.refreshToken == refresh {
			t.Fatalf("renewal %d handed back no new refresh token", i+1)
		}
		if !nearly(renewed.refreshExpiresAt, capAt) {
			t.Fatalf("renewal %d handed back a refresh token expiring at %v, want the session's end %v, not 30 days on",
				i+1, renewed.refreshExpiresAt, capAt)
		}
		if renewed.accountID != account.GetID() {
			t.Fatalf("renewal %d names another account than the sign-in", i+1)
		}
		if carriesTheConnectorMark(t, renewed.token) {
			t.Fatalf("renewal %d handed back a token carrying the connector mark", i+1)
		}
		if me := serveCallWithToken(t, srv, http.MethodGet, "/api/auth/me", renewed.token); me.status != http.StatusOK {
			t.Fatalf("GET /api/auth/me with the token of renewal %d answered %d code %q, want 200",
				i+1, me.status, refusalCode(me.body))
		}
		refresh = renewed.refreshToken
	}
}

// nearly reports whether got is want give or take two seconds: SQLite may round
// what it keeps.
func nearly(got, want time.Time) bool {
	d := got.Sub(want)
	return d > -2*time.Second && d < 2*time.Second
}
