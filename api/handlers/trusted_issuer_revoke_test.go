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

package handlers_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

// fakeTokenRevocation stands in for application.AssertedTokenRevocation: it
// records the identity the handler asked it about.
type fakeTokenRevocation struct {
	got    []application.AssertedIdentity
	result application.TokenRevocationResult
	err    error
}

func (f *fakeTokenRevocation) Revoke(_ context.Context, id application.AssertedIdentity) (
	application.TokenRevocationResult, error,
) {
	f.got = append(f.got, id)
	return f.result, f.err
}

func newRevocationHandler(
	verifier handlers.AssertionVerifier, revoke handlers.TokenRevocationService, logs *assertionLogCapture,
) *handlers.TokenRevocationHandler {
	return handlers.NewTokenRevocationHandler(handlers.TokenRevocationHandlerConfig{
		Verifier: verifier,
		Revoke:   revoke,
		Logger:   logs,
	})
}

func postRevocation(t *testing.T, h *handlers.TokenRevocationHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(newJSONRequest(http.MethodPost, "/api/auth/revoke-tokens", body), rec)
	if err := h.RevokeTokens(c); err != nil {
		t.Fatalf("RevokeTokens returned an error instead of answering: %v", err)
	}
	return rec
}

// The door learns only that the person now holds no refresh token here. An
// answer that differed when the instance had never heard of the person would
// tell the caller whether an address has an account on this instance.
func TestRevokeTokensAnswersTheSameWhateverItFound(t *testing.T) {
	results := map[string]application.TokenRevocationResult{
		"a person with tokens":    {People: []string{"agent-dana"}, Tokens: 3},
		"a person with none left": {People: []string{"agent-dana"}},
		"nobody the instance has": {},
	}
	for name, result := range results {
		t.Run(name, func(t *testing.T) {
			logs := &assertionLogCapture{}
			revoke := &fakeTokenRevocation{result: result}
			verifier := &fakeAssertionVerifier{identity: trustedissuer.Identity{
				Provider: "door", Subject: "door-108234567890", Email: "ops@harborlegal.example",
			}}
			h := newRevocationHandler(verifier, revoke, logs)

			rec := postRevocation(t, h, assertionBody(presentedAssertion))

			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204 (%s)", rec.Code, rec.Body.String())
			}
			if body := strings.TrimSpace(rec.Body.String()); body != "" {
				t.Fatalf("the answer carries a body: %s", body)
			}
			if len(revoke.got) != 1 {
				t.Fatalf("the revocation was asked %d times, want once", len(revoke.got))
			}
			asked := revoke.got[0]
			if asked.Provider != "door" || asked.Subject != "door-108234567890" || asked.Email != "ops@harborlegal.example" {
				t.Fatalf("the revocation was asked about %+v, want the person the assertion names", asked)
			}
			requireNoAssertionLogged(t, logs)
		})
	}
}

// Every refusal the verifier makes is answered the way the sign-in answers
// one: 401, the reason as the code, one warning, and nothing revoked. A
// revocation assertion presented to a route that cannot trust it must never
// end anybody's access.
func TestRevokeTokensRefusesWithTheReasonTheVerifierGave(t *testing.T) {
	reasons := []trustedissuer.Reason{
		trustedissuer.ReasonSignature, trustedissuer.ReasonKidMiss, trustedissuer.ReasonIssuer,
		trustedissuer.ReasonAudience, trustedissuer.ReasonExpired, trustedissuer.ReasonWindow,
		trustedissuer.ReasonReplay, trustedissuer.ReasonClaims, trustedissuer.ReasonKeysUnreachable,
		trustedissuer.ReasonAllowlist,
	}
	for _, reason := range reasons {
		t.Run(string(reason), func(t *testing.T) {
			logs := &assertionLogCapture{}
			revoke := &fakeTokenRevocation{}
			verifier := &fakeAssertionVerifier{err: &trustedissuer.Refusal{Reason: reason, Detail: "because"}}
			h := newRevocationHandler(verifier, revoke, logs)

			rec := postRevocation(t, h, assertionBody(presentedAssertion))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body.String())
			}
			if code := readAssertAnswer(t, rec).Code; code != string(reason) {
				t.Fatalf("answer code = %q, want %q", code, reason)
			}
			if len(revoke.got) != 0 {
				t.Fatalf("a refused assertion revoked tokens: %+v", revoke.got)
			}
			warns := logs.atLevel("warn")
			if len(warns) != 1 {
				t.Fatalf("expected one warning for the refusal, got:\n%s", logs.text())
			}
			if got, _ := field(warns[0], "reason"); got != string(reason) {
				t.Fatalf("log line reason = %v, want %q", got, reason)
			}
			requireNoAssertionLogged(t, logs)
		})
	}
}

// An error that is not a refusal is still a refusal: the least specific reason
// there is, never an accepted assertion.
func TestRevokeTokensTreatsAnUnexpectedVerifierErrorAsASignatureRefusal(t *testing.T) {
	revoke := &fakeTokenRevocation{}
	h := newRevocationHandler(&fakeAssertionVerifier{err: errors.New("boom")}, revoke, &assertionLogCapture{})

	rec := postRevocation(t, h, assertionBody(presentedAssertion))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if code := readAssertAnswer(t, rec).Code; code != string(trustedissuer.ReasonSignature) {
		t.Fatalf("answer code = %q, want signature", code)
	}
	if len(revoke.got) != 0 {
		t.Fatalf("an unverified assertion revoked tokens")
	}
}

// A store that could not be written leaves the tokens renewing. Answering 204
// there would tell the door an eviction happened that did not.
func TestRevokeTokensAsksTheDoorToTryAgainWhenNothingCouldBeRevoked(t *testing.T) {
	logs := &assertionLogCapture{}
	verifier := &fakeAssertionVerifier{identity: trustedissuer.Identity{
		Provider: "door", Subject: "door-1", Email: "ops@harborlegal.example",
	}}
	h := newRevocationHandler(verifier, &fakeTokenRevocation{err: errors.New("the database is locked")}, logs)

	rec := postRevocation(t, h, assertionBody(presentedAssertion))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	if retry := rec.Header().Get("Retry-After"); retry == "" {
		t.Fatal("a 503 with no Retry-After leaves the door guessing when to ask again")
	}
	if answer := readAssertAnswer(t, rec); !strings.Contains(answer.Error, "ask again") {
		t.Fatalf("answer error = %q, want it to tell the door to ask again", answer.Error)
	}
	// The store's own words never reach the caller.
	if strings.Contains(rec.Body.String(), "database is locked") {
		t.Fatalf("the answer carries the store's error: %s", rec.Body.String())
	}
}

// A page on another site that holds a revocation assertion must not be able to
// spend it from a victim's browser. The body is not read, so the jti survives.
func TestRevokeTokensRefusesARequestFromAnotherSiteBeforeItIsRead(t *testing.T) {
	logs := &assertionLogCapture{}
	verifier := &fakeAssertionVerifier{}
	revoke := &fakeTokenRevocation{}
	h := handlers.NewTokenRevocationHandler(handlers.TokenRevocationHandlerConfig{
		Verifier: verifier, Revoke: revoke, Logger: logs,
		AllowedOrigins: []string{"https://money.weos.cloud"},
	})

	for name, headers := range map[string]map[string]string{
		"a page on another site":         {"Origin": "https://attacker.example", "Sec-Fetch-Site": "cross-site"},
		"another origin, no fetch marks": {"Origin": "https://attacker.example"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := newJSONRequest(http.MethodPost, "/api/auth/revoke-tokens", assertionBody(presentedAssertion))
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			if err := h.RevokeTokens(echo.New().NewContext(req, rec)); err != nil {
				t.Fatalf("RevokeTokens returned an error: %v", err)
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
			}
			if code := readAssertAnswer(t, rec).Code; code != handlers.CodeCrossSite {
				t.Fatalf("answer code = %q, want %q", code, handlers.CodeCrossSite)
			}
			if len(verifier.got) != 0 || len(revoke.got) != 0 {
				t.Fatalf("a cross-site request was read: verified %d, revoked %d", len(verifier.got), len(revoke.got))
			}
		})
	}
}

// The door calls this one server-side, with no Origin at all, which is not a
// request another site can drive.
func TestRevokeTokensTakesAServerSideCallFromTheDoor(t *testing.T) {
	revoke := &fakeTokenRevocation{}
	h := handlers.NewTokenRevocationHandler(handlers.TokenRevocationHandlerConfig{
		Verifier:       &fakeAssertionVerifier{identity: trustedissuer.Identity{Provider: "door", Subject: "door-1", Email: "ops@harborlegal.example"}},
		Revoke:         revoke,
		Logger:         &assertionLogCapture{},
		AllowedOrigins: []string{"https://money.weos.cloud"},
	})

	rec := postRevocation(t, h, assertionBody(presentedAssertion))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", rec.Code, rec.Body.String())
	}
	if len(revoke.got) != 1 {
		t.Fatalf("the door's own call was not acted on")
	}
}

func TestRevokeTokensAnswersAnOversizedBodyWithoutReadingAnAssertion(t *testing.T) {
	logs := &assertionLogCapture{}
	verifier := &fakeAssertionVerifier{}
	h := newRevocationHandler(verifier, &fakeTokenRevocation{}, logs)

	rec := postRevocation(t, h, assertionBody(strings.Repeat("a", handlers.RevokeTokensBodyLimit+1)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (%s)", rec.Code, rec.Body.String())
	}
	if len(verifier.got) != 0 {
		t.Fatalf("an oversized body reached the verifier")
	}
	if code := readAssertAnswer(t, rec).Code; code != "" {
		t.Fatalf("an oversized body named the refusal reason %q; it is not an assertion refusal", code)
	}
}

// --- the constructor serve.go builds the route with ---

// revocationDoor is a trusted issuer: one ES256 key, published on a key list,
// signing whatever claims a case asks for.
type revocationDoor struct {
	key    *ecdsa.PrivateKey
	server *httptest.Server
}

const revocationDoorKeyID = "door-2026-09"

func newRevocationDoor(t *testing.T) *revocationDoor {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate the door's key: %v", err)
	}
	raw, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("encode the door's key: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": revocationDoorKeyID,
			"x": base64.RawURLEncoding.EncodeToString(raw[1:33]),
			"y": base64.RawURLEncoding.EncodeToString(raw[33:65]),
		}}})
	}))
	t.Cleanup(server.Close)
	return &revocationDoor{key: key, server: server}
}

func (d *revocationDoor) settings() config.TrustedIssuerConfig {
	return config.TrustedIssuerConfig{
		Issuer:   "https://money.weos.cloud",
		JWKSURL:  d.server.URL + "/door/jwks.json",
		Audience: "a1b2c3d4",
	}
}

func (d *revocationDoor) sign(t *testing.T, now time.Time, purpose string) string {
	t.Helper()
	claims := gojwt.MapClaims{
		"iss": "https://money.weos.cloud", "aud": "a1b2c3d4",
		"sub": "door-108234567890", "email": "ops@harborlegal.example", "email_verified": true,
		"provider": "door", "jti": "revocation-" + purpose + now.String(),
		"iat": now.Unix(), "exp": now.Add(45 * time.Second).Unix(),
	}
	if purpose != "" {
		claims["purpose"] = purpose
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodES256, claims)
	token.Header["kid"] = revocationDoorKeyID
	signed, err := token.SignedString(d.key)
	if err != nil {
		t.Fatalf("sign the assertion: %v", err)
	}
	return signed
}

// The route serve.go builds takes an assertion that asks to revoke tokens, and
// only that one: a login assertion presented here is refused, so an assertion
// captured on its way to the sign-in cannot end anybody's token access.
func TestNewTrustedIssuerRevocationHandlerTakesOnlyARevocationAssertion(t *testing.T) {
	door := newRevocationDoor(t)
	// Far from the real clock: an assertion good only by this one proves the
	// constructor hands deps.Now to the verifier.
	now := time.Unix(1_789_000_000, 0)

	cases := map[string]struct {
		purpose    string
		wantStatus int
		wantCode   string
	}{
		"an assertion that asks to revoke tokens": {trustedissuer.PurposeRevokeTokens, http.StatusNoContent, ""},
		"a login assertion, which says nothing":   {"", http.StatusUnauthorized, string(trustedissuer.ReasonClaims)},
		"an assertion that asks to sign in":       {trustedissuer.PurposeLogin, http.StatusUnauthorized, string(trustedissuer.ReasonClaims)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			revoke := &fakeTokenRevocation{}
			h := handlers.NewTrustedIssuerRevocationHandler(door.settings(),
				[]string{"ops@harborlegal.example"},
				handlers.TrustedIssuerRevocationDeps{
					Revoke: revoke, Logger: &assertionLogCapture{},
					Now: func() time.Time { return now },
				})

			rec := postRevocation(t, h, assertionBody(door.sign(t, now, c.purpose)))

			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, c.wantStatus, rec.Body.String())
			}
			if c.wantCode == "" {
				if len(revoke.got) != 1 {
					t.Fatalf("an accepted revocation assertion revoked nothing")
				}
				return
			}
			if code := readAssertAnswer(t, rec).Code; code != c.wantCode {
				t.Fatalf("answer code = %q, want %q", code, c.wantCode)
			}
			if len(revoke.got) != 0 {
				t.Fatalf("a refused assertion revoked tokens")
			}
		})
	}
}

// The allowlist is enforced here as it is at the sign-in: the door may not end
// the token access of an address this instance never admitted.
func TestNewTrustedIssuerRevocationHandlerEnforcesTheAllowlist(t *testing.T) {
	door := newRevocationDoor(t)
	now := time.Unix(1_789_000_000, 0)
	revoke := &fakeTokenRevocation{}
	h := handlers.NewTrustedIssuerRevocationHandler(door.settings(),
		[]string{"someone.else@harborlegal.example"},
		handlers.TrustedIssuerRevocationDeps{
			Revoke: revoke, Logger: &assertionLogCapture{},
			Now: func() time.Time { return now },
		})

	rec := postRevocation(t, h, assertionBody(door.sign(t, now, trustedissuer.PurposeRevokeTokens)))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	if code := readAssertAnswer(t, rec).Code; code != string(trustedissuer.ReasonAllowlist) {
		t.Fatalf("answer code = %q, want allowlist", code)
	}
	if len(revoke.got) != 0 {
		t.Fatalf("an email the allowlist does not name had its tokens revoked")
	}
}

// --- mount or not ---

// The revocation mounts exactly where the sign-in does. An instance that
// cannot take an assertion cannot take a revocation either, and one that takes
// sign-ins must give the door a way to end what they handed out.
func TestMountTrustedIssuerRevocation(t *testing.T) {
	const (
		iss = "https://money.weos.cloud"
		url = "https://money.weos.cloud/door/jwks.json"
		aud = "a1b2c3d4"
	)
	all3 := config.TrustedIssuerConfig{Issuer: iss, JWKSURL: url, Audience: aud}
	cases := map[string]struct {
		settings    config.TrustedIssuerConfig
		secret      string
		wantMounted bool
	}{
		"all three":                         {settings: all3, wantMounted: true},
		"nothing configured":                {settings: config.TrustedIssuerConfig{}},
		"no audience":                       {settings: config.TrustedIssuerConfig{Issuer: iss, JWKSURL: url}},
		"not a key-list URL":                {settings: config.TrustedIssuerConfig{Issuer: iss, JWKSURL: "jwks.json", Audience: aud}},
		"all three, default session secret": {settings: all3, secret: config.DefaultSessionSecret},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			logs := &assertionLogCapture{}
			builds := 0
			e := echo.New()
			api := e.Group("/api")
			cfg := config.Config{SessionSecret: instanceSessionSecret, TrustedIssuer: c.settings}
			if c.secret != "" {
				cfg.SessionSecret = c.secret
			}

			mounted := handlers.MountTrustedIssuerRevocation(context.Background(), api, cfg, logs,
				func() *handlers.TokenRevocationHandler {
					builds++
					return newRevocationHandler(
						&fakeAssertionVerifier{err: &trustedissuer.Refusal{Reason: trustedissuer.ReasonSignature}},
						&fakeTokenRevocation{}, &assertionLogCapture{})
				})

			if mounted != c.wantMounted {
				t.Fatalf("mounted = %v, want %v", mounted, c.wantMounted)
			}
			if want := map[bool]int{true: 1, false: 0}[c.wantMounted]; builds != want {
				t.Fatalf("handler built %d times, want %d", builds, want)
			}

			serve := func(path string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, newJSONRequest(http.MethodPost, path, assertionBody(presentedAssertion)))
				return rec
			}
			revoke, never := serve("/api/auth/revoke-tokens"), serve("/api/auth/enroll")
			if c.wantMounted {
				if revoke.Code != http.StatusUnauthorized {
					t.Fatalf("the mounted route answered %d, want the handler's 401", revoke.Code)
				}
				return
			}
			// An unmounted route is never registered, so it answers exactly
			// like a path this server has never had.
			if revoke.Code != never.Code || revoke.Body.String() != never.Body.String() {
				t.Fatalf("the unmounted route answered %d %q, a never-mounted path %d %q",
					revoke.Code, revoke.Body.String(), never.Code, never.Body.String())
			}
			// The assertion route has already said what is wrong with the same
			// settings; a second line would double every boot warning.
			if len(logs.atLevel("warn")) != 0 || len(logs.atLevel("error")) != 0 {
				t.Fatalf("the revocation mount logged a second line about the same settings:\n%s", logs.text())
			}
		})
	}
}
