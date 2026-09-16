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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// assertionFor is a request body carrying a fresh assertion the door signed
// for email: purpose is the one it asks for ("" for a login assertion, which
// says nothing), and session is the sign-in's "session" field ("" for a
// browser's).
func (d *bootDoor) assertionFor(t *testing.T, email, purpose, session string) string {
	t.Helper()
	now := time.Now()
	claims := gojwt.MapClaims{
		"iss":            bootDoorIssuer,
		"aud":            bootDoorAudience,
		"sub":            "108234567890",
		"email":          email,
		"email_verified": true,
		"provider":       "google",
		"name":           "Dana Whitfield",
		"jti":            fmt.Sprintf("boot-%d", now.UnixNano()),
		"iat":            now.Unix(),
		"exp":            now.Add(60 * time.Second).Unix(),
	}
	if purpose != "" {
		claims["purpose"] = purpose
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodES256, claims)
	token.Header["kid"] = bootDoorKeyID
	signed, err := token.SignedString(d.key)
	if err != nil {
		t.Fatalf("sign the assertion: %v", err)
	}
	if session == "" {
		return fmt.Sprintf(`{"assertion":%q}`, signed)
	}
	return fmt.Sprintf(`{"assertion":%q,"session":%q}`, signed, session)
}

// refreshTokenIn is the refresh token a sign-in or a renewal handed back.
func refreshTokenIn(t *testing.T, answer serveAnswer, what string) string {
	t.Helper()
	var envelope struct {
		Data struct {
			RefreshToken string `json:"refresh_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &envelope); err != nil {
		t.Fatalf("decode %s: %v (%s)", what, err, answer.body)
	}
	if envelope.Data.RefreshToken == "" {
		t.Fatalf("%s handed back no refresh token: %s", what, answer.body)
	}
	return envelope.Data.RefreshToken
}

func renew(t *testing.T, srv *httptest.Server, token string) serveAnswer {
	t.Helper()
	return serveCall(t, srv, http.MethodPost, "/api/auth/refresh",
		fmt.Sprintf(`{"refresh_token":%q}`, token), nil)
}

// A password reset at the door ends the person's token access on the instance:
// the app's refresh token stops renewing, so nothing issued under the old
// password outlives it (wm-fcpzx). This is the whole path serve mounts —
// sign in through the door, renew, revoke, fail to renew.
func TestServe_ADoorRevocationEndsThePersonsTokenAccess(t *testing.T) {
	door := newBootDoor(t)
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.TrustedIssuer = door.settings()
	srv := bootServe(t, cfg)

	signIn := serveCall(t, srv, http.MethodPost, "/api/auth/assert",
		door.assertionFor(t, bootOwnerEmail, "", "native"), nil)
	if signIn.status != http.StatusOK {
		t.Fatalf("the native sign-in answered %d %s", signIn.status, signIn.body)
	}
	issued := refreshTokenIn(t, signIn, "the native sign-in")

	renewed := renew(t, srv, issued)
	if renewed.status != http.StatusOK {
		t.Fatalf("the renewal answered %d %s, want the session renewed", renewed.status, renewed.body)
	}
	held := refreshTokenIn(t, renewed, "the renewal")

	revoked := serveCall(t, srv, http.MethodPost, "/api/auth/revoke-tokens",
		door.assertionFor(t, bootOwnerEmail, trustedissuer.PurposeRevokeTokens, ""), nil)
	if revoked.status != http.StatusNoContent || revoked.body != "" {
		t.Fatalf("the revocation answered %d %q, want 204 and no body", revoked.status, revoked.body)
	}

	after := renew(t, srv, held)
	if after.status != http.StatusUnauthorized {
		t.Fatalf("the refresh token still renews: %d %s", after.status, after.body)
	}

	// Idempotent, and it says the same thing about a person it has already
	// revoked for as about one it has never heard of.
	for name, email := range map[string]string{
		"the same person again":          bootOwnerEmail,
		"a person it has never heard of": "nobody@harborlegal.example",
	} {
		t.Run(name, func(t *testing.T) {
			again := serveCall(t, srv, http.MethodPost, "/api/auth/revoke-tokens",
				door.assertionFor(t, email, trustedissuer.PurposeRevokeTokens, ""), nil)
			if again.status != http.StatusNoContent || again.body != "" {
				t.Fatalf("answered %d %q, want 204 and no body", again.status, again.body)
			}
		})
	}
}

// One issuer, one key list and one audience serve both routes, so each refuses
// the other's assertion. An assertion captured on its way to the sign-in must
// not end a person's token access, and one captured on its way to the
// revocation must not sign its holder in.
func TestServe_EachAssertionRouteRefusesTheOthersAssertion(t *testing.T) {
	door := newBootDoor(t)
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.TrustedIssuer = door.settings()
	srv := bootServe(t, cfg)

	cases := map[string]struct {
		path    string
		purpose string
	}{
		"a login assertion at the revocation":   {"/api/auth/revoke-tokens", ""},
		"a revocation assertion at the sign-in": {"/api/auth/assert", trustedissuer.PurposeRevokeTokens},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			answer := serveCall(t, srv, http.MethodPost, c.path, door.assertionFor(t, bootOwnerEmail, c.purpose, ""), nil)
			if answer.status != http.StatusUnauthorized {
				t.Fatalf("%s answered %d %s, want 401", c.path, answer.status, answer.body)
			}
			var refusal struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal([]byte(answer.body), &refusal); err != nil {
				t.Fatalf("decode the refusal: %v (%s)", err, answer.body)
			}
			if refusal.Code != string(trustedissuer.ReasonClaims) {
				t.Fatalf("refusal code = %q, want %q", refusal.Code, trustedissuer.ReasonClaims)
			}
			if len(answer.cookies) != 0 {
				t.Fatalf("a refused assertion set cookies: %v", answer.cookies)
			}
		})
	}
}

// An instance outside any fleet never had the route, so it answers exactly
// like a path this server has never had.
func TestServe_AnInstanceWithNoTrustedIssuerHasNoRevocationRoute(t *testing.T) {
	srv := bootServe(t, config.Default())

	revoke := serveCall(t, srv, http.MethodPost, "/api/auth/revoke-tokens", `{"assertion":"whatever"}`, nil)
	never := serveCall(t, srv, http.MethodPost, "/api/auth/enroll", `{"assertion":"whatever"}`, nil)

	if revoke.status != never.status || revoke.body != never.body {
		t.Fatalf("the unmounted route answered %d %q, a never-mounted path %d %q",
			revoke.status, revoke.body, never.status, never.body)
	}
}
