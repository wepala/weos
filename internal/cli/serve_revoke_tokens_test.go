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
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// assertionFor is a request body carrying a fresh assertion the door signed
// for the owner's Google identity with email: purpose is the one it asks for ("" for a login assertion, which
// says nothing), and session is the sign-in's "session" field ("" for a
// browser's).
func (d *bootDoor) assertionFor(t *testing.T, email, purpose, session string) string {
	t.Helper()
	return d.assertionOf(t, bootOwnerSubject, email, purpose, session)
}

// assertionOf is assertionFor for any subject.
func (d *bootDoor) assertionOf(t *testing.T, subject, email, purpose, session string) string {
	t.Helper()
	now := time.Now()
	claims := gojwt.MapClaims{
		"iss":            bootDoorIssuer,
		"aud":            bootDoorAudience,
		"sub":            subject,
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
	// revoked for as about an identity it has never seen — even one that
	// names the owner's email, because the revocation never reaches by email.
	for name, id := range map[string][2]string{
		"the same person again":              {bootOwnerSubject, bootOwnerEmail},
		"an identity it has never seen":      {"google-999", "nobody@harborlegal.example"},
		"an unseen identity with that email": {"google-998", bootOwnerEmail},
	} {
		t.Run(name, func(t *testing.T) {
			again := serveCall(t, srv, http.MethodPost, "/api/auth/revoke-tokens",
				door.assertionOf(t, id[0], id[1], trustedissuer.PurposeRevokeTokens, ""), nil)
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

// registerConnector registers a connector and answers its client id.
func registerConnector(t *testing.T, srv *httptest.Server, name string) string {
	t.Helper()
	registered := serveCall(t, srv, http.MethodPost, "/oauth/register",
		`{"client_name":"`+name+`","redirect_uris":["`+connectorRedirectURI+`"],"grant_types":["authorization_code","refresh_token"]}`, nil)
	var client struct {
		ClientID string `json:"client_id"`
	}
	if registered.status != http.StatusCreated || json.Unmarshal([]byte(registered.body), &client) != nil || client.ClientID == "" {
		t.Fatalf("POST /oauth/register answered %d %s, want 201 with a client id", registered.status, registered.body)
	}
	return client.ClientID
}

// authorizeWithSession asks for an authorization code with nothing but the
// browser session in cookies — the request an intruder holding the device
// makes. It answers the code, or "" when the instance would not hand one over.
func authorizeWithSession(t *testing.T, srv *httptest.Server, clientID string, cookies []*http.Cookie) string {
	t.Helper()
	sum := sha256.Sum256([]byte(connectorVerifier))
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", connectorRedirectURI)
	q.Set("response_type", "code")
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("state", "st-4ke17")
	q.Set("scope", "mcp:read mcp:write")
	answer := serveCall(t, srv, http.MethodGet, "/oauth/authorize?"+q.Encode(), "", cookies)
	location, err := url.Parse(answer.header.Get("Location"))
	if err != nil {
		return ""
	}
	return location.Query().Get("code")
}

// exchangeCode presents an authorization code at the token endpoint.
func exchangeCode(t *testing.T, srv *httptest.Server, clientID, code string) serveAnswer {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", connectorRedirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", connectorVerifier)
	return postTokenForm(t, srv, form)
}

// wm-4ke17. The reset is asked for because somebody else holds the device, and
// what they hold is the browser cookie. Ending the refresh tokens and leaving
// that cookie alive is undone by one request: GET /oauth/authorize takes the
// session, mints an authorization code with no re-authentication, and
// POST /oauth/token buys a fresh 30-day refresh token with it. So the
// revocation ends the session too, and voids the codes already handed out.
//
// This drives the whole path serve mounts: sign in through the door, connect a
// connector, take a second code and hold it, revoke — then none of the three
// works.
func TestServe_ADoorRevocationEndsTheSessionThatWouldMintTokensAgain(t *testing.T) {
	door := newBootDoor(t)
	srv := bootServe(t, connectorConfig(door))
	owner := signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
	connector := connectThroughOAuth(t, srv, owner.cookies)

	// The connector renews before the reset, which rotates its token; the
	// rotated one is what the revocation has to reach.
	rotated := decodeTokenAnswer(t, refreshConnector(t, srv, connector), "the refresh grant before the reset")
	rotated.clientID = connector.clientID

	// A code minted before the reset and not yet redeemed, held the way a
	// client holds one between the redirect and the exchange.
	held := authorizeWithSession(t, srv, connector.clientID, owner.cookies)
	if held == "" {
		t.Fatal("the session minted no authorization code before the reset, so the test proves nothing")
	}

	revoked := serveCall(t, srv, http.MethodPost, "/api/auth/revoke-tokens",
		door.assertionFor(t, bootOwnerEmail, trustedissuer.PurposeRevokeTokens, ""), nil)
	if revoked.status != http.StatusNoContent || revoked.body != "" {
		t.Fatalf("the revocation answered %d %q, want 204 and no body", revoked.status, revoked.body)
	}

	// 1. The connector's token is dead.
	refused := refreshConnector(t, srv, rotated)
	if refused.status != http.StatusBadRequest || oauthError(refused.body) != "invalid_grant" {
		t.Errorf("the connector's refresh token still renews: %d %q", refused.status, oauthError(refused.body))
	}

	// 2. The code it was already handed buys nothing.
	exchanged := exchangeCode(t, srv, connector.clientID, held)
	if exchanged.status == http.StatusOK {
		t.Error("an authorization code minted before the reset was still exchanged for a token")
	} else if oauthError(exchanged.body) != "invalid_grant" {
		t.Errorf("exchanging the held code answered %d %q, want invalid_grant", exchanged.status, oauthError(exchanged.body))
	}

	// 3. And the cookie mints no new one, which is what would have undone all
	// of it.
	if again := authorizeWithSession(t, srv, connector.clientID, owner.cookies); again != "" {
		t.Error("the browser session survived the reset and minted a fresh authorization code")
	}

	// The session is ended for every route, not only the OAuth one.
	reached := serveRequest(t, srv, http.MethodGet, "/api/resource-types", "", "", owner.cookies)
	if reached.status != http.StatusUnauthorized {
		t.Errorf("the revoked session still reaches the protected API: %d", reached.status)
	}
}

// Another person signing in at the same instance keeps their session and their
// connector: the reset reaches the one person the assertion names. A
// SESSION_SECRET rotation, the only lever there used to be on a session, would
// have signed this person out too.
func TestServe_ADoorRevocationLeavesAnotherPersonsSessionAlone(t *testing.T) {
	door := newBootDoor(t)
	srv := bootServe(t, connectorConfig(door))
	owner := signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
	other := signInThroughTheDoor(t, srv, door, bootMemberEmail, bootMemberSubject, bootMemberName)
	clientID := registerConnector(t, srv, "Harbor Notes")

	revoked := serveCall(t, srv, http.MethodPost, "/api/auth/revoke-tokens",
		door.assertionOf(t, bootOwnerSubject, bootOwnerEmail, trustedissuer.PurposeRevokeTokens, ""), nil)
	if revoked.status != http.StatusNoContent {
		t.Fatalf("the revocation answered %d %s, want 204", revoked.status, revoked.body)
	}

	if authorizeWithSession(t, srv, clientID, owner.cookies) != "" {
		t.Fatal("the reset person's session still mints authorization codes")
	}
	if authorizeWithSession(t, srv, clientID, other.cookies) == "" {
		t.Fatal("another person's session was ended by a reset that does not name them")
	}
	reached := serveRequest(t, srv, http.MethodGet, "/api/resource-types", "", "", other.cookies)
	if reached.status != http.StatusOK {
		t.Fatalf("another person's session answered %d on the protected API, want 200", reached.status)
	}
}
