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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/internal/config"

	gojwt "github.com/golang-jwt/jwt/v5"
	"go.uber.org/fx"
)

// These tests boot what serve boots — buildServer, the function runServe
// calls — rather than a hand-copied route layout, because the property under
// test is serve's own choice of auth middleware for each route group.

const (
	bootDoorIssuer   = "https://door.example"
	bootDoorAudience = "instance-under-boot-test"
	bootDoorKeyID    = "boot-door-key"
	bootOwnSecret    = "a-session-secret-of-this-instance-alone"
	bootOwnerEmail   = "dana.whitfield@harborlegal.example"
)

// bootDoor is a fleet front door: one ES256 key, published on a loopback key
// list the instance may read over plain http.
type bootDoor struct {
	key    *ecdsa.PrivateKey
	server *httptest.Server
}

func newBootDoor(t *testing.T) *bootDoor {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate the door's key: %v", err)
	}
	raw, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("encode the door's key: %v", err)
	}
	keyList, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": bootDoorKeyID,
		"x": base64.RawURLEncoding.EncodeToString(raw[1:33]),
		"y": base64.RawURLEncoding.EncodeToString(raw[33:65]),
	}}})
	if err != nil {
		t.Fatalf("encode the door's key list: %v", err)
	}
	d := &bootDoor{key: key}
	d.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(keyList) // A failed write fails the key read, which the test reports.
	}))
	t.Cleanup(d.server.Close)
	return d
}

func (d *bootDoor) settings() config.TrustedIssuerConfig {
	return config.TrustedIssuerConfig{
		Issuer:   bootDoorIssuer,
		JWKSURL:  d.server.URL + "/door/jwks.json",
		Audience: bootDoorAudience,
	}
}

// assertionBody is a request body carrying a fresh assertion the door signed
// for email.
func (d *bootDoor) assertionBody(t *testing.T, email string) string {
	t.Helper()
	now := time.Now()
	token := gojwt.NewWithClaims(gojwt.SigningMethodES256, gojwt.MapClaims{
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
	})
	token.Header["kid"] = bootDoorKeyID
	signed, err := token.SignedString(d.key)
	if err != nil {
		t.Fatalf("sign the assertion: %v", err)
	}
	return fmt.Sprintf(`{"assertion":%q}`, signed)
}

// bootServe starts serve's application and routes for cfg on a fresh database
// and serves them on a test listener.
func bootServe(t *testing.T, cfg config.Config) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	cfg.DatabaseDSN = filepath.Join(dir, "weos.db")
	cfg.Storage.LocalPath = filepath.Join(dir, "uploads")
	cfg.LogLevel = "error"

	e, app, err := buildServer(cfg)
	if err != nil {
		t.Fatalf("build the server: %v", err)
	}
	srv := httptest.NewServer(e)
	t.Cleanup(func() {
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		if err := app.Stop(ctx); err != nil {
			t.Logf("stop the application: %v", err)
		}
	})
	return srv
}

type serveAnswer struct {
	status  int
	body    string
	cookies []*http.Cookie
}

func serveCall(t *testing.T, srv *httptest.Server, method, path, body string, cookies []*http.Cookie) serveAnswer {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, path, err)
	}
	return serveAnswer{status: resp.StatusCode, body: strings.TrimSpace(string(raw)), cookies: resp.Cookies()}
}

// offersTheDoor reports whether GET /api/auth/providers lists the issuer.
func offersTheDoor(t *testing.T, srv *httptest.Server) bool {
	t.Helper()
	answer := serveCall(t, srv, http.MethodGet, "/api/auth/providers", "", nil)
	if answer.status != http.StatusOK {
		t.Fatalf("GET /api/auth/providers answered %d %s", answer.status, answer.body)
	}
	var envelope struct {
		Data struct {
			Providers []struct {
				Name string `json:"name"`
			} `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &envelope); err != nil {
		t.Fatalf("decode the providers: %v (%s)", err, answer.body)
	}
	for _, p := range envelope.Data.Providers {
		if p.Name == "issuer" {
			return true
		}
	}
	return false
}

// requireUnmountedAssert checks that POST /api/auth/assert, given a valid
// assertion, answers exactly what a path the server has never had answers.
func requireUnmountedAssert(t *testing.T, srv *httptest.Server, body string) {
	t.Helper()
	assert := serveCall(t, srv, http.MethodPost, "/api/auth/assert", body, nil)
	never := serveCall(t, srv, http.MethodPost, "/api/auth/enroll", body, nil)
	if assert.status != never.status || assert.body != never.body {
		t.Fatalf("POST /api/auth/assert answered %d %q; a never-mounted path answered %d %q",
			assert.status, assert.body, never.status, never.body)
	}
	if len(assert.cookies) != 0 {
		t.Fatalf("an unmounted assertion route set cookies: %v", assert.cookies)
	}
}

// probesWithoutSession are one route from each group whose auth serve
// chooses by configuration: the protected group, /api/auth/me, and the MCP
// group.
var probesWithoutSession = []struct{ method, path string }{
	{http.MethodGet, "/api/resource-types"},
	{http.MethodGet, "/api/auth/me"},
	{http.MethodPost, "/api/mcp"},
}

// A trusted issuer is the instance's only sign-in: no OAuth provider and no
// password sign-in. The API must refuse a caller with no session, and admit
// the session the door's assertion issues.
func TestServe_TrustedIssuerOnlyRequiresTheAssertedSession(t *testing.T) {
	door := newBootDoor(t)
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.TrustedIssuer = door.settings()
	if cfg.OAuthEnabled() || cfg.PasswordAuthEnabled {
		t.Fatalf("the instance must have no OAuth provider and no password sign-in")
	}
	srv := bootServe(t, cfg)

	for _, p := range probesWithoutSession {
		if got := serveCall(t, srv, p.method, p.path, "", nil); got.status != http.StatusUnauthorized {
			t.Fatalf("%s %s with no session answered %d %s, want 401", p.method, p.path, got.status, got.body)
		}
	}
	if !offersTheDoor(t, srv) {
		t.Fatalf("the sign-in screen does not offer the door")
	}

	signIn := serveCall(t, srv, http.MethodPost, "/api/auth/assert", door.assertionBody(t, bootOwnerEmail), nil)
	if signIn.status != http.StatusOK {
		t.Fatalf("POST /api/auth/assert answered %d %s, want 200", signIn.status, signIn.body)
	}
	if len(signIn.cookies) == 0 {
		t.Fatalf("the asserted sign-in set no session cookie")
	}

	if got := serveCall(t, srv, http.MethodGet, "/api/resource-types", "", signIn.cookies); got.status != http.StatusOK {
		t.Fatalf("GET /api/resource-types with the asserted session answered %d %s, want 200", got.status, got.body)
	}
	me := serveCall(t, srv, http.MethodGet, "/api/auth/me", "", signIn.cookies)
	if me.status != http.StatusOK || !strings.Contains(me.body, bootOwnerEmail) {
		t.Fatalf("GET /api/auth/me with the asserted session answered %d %s, want 200 naming %s",
			me.status, me.body, bootOwnerEmail)
	}
}

// A trusted issuer that cannot be used must lock the API, never leave it in
// dev mode: the route is not mounted, the door is not offered, and a caller
// with no session is refused.
func TestServe_UnusableTrustedIssuerLocksTheAPI(t *testing.T) {
	door := newBootDoor(t)
	cases := map[string]func(*config.Config){
		"under the default session secret": func(cfg *config.Config) {
			cfg.SessionSecret = config.DefaultSessionSecret
			cfg.TrustedIssuer = door.settings()
		},
		"missing its audience": func(cfg *config.Config) {
			cfg.SessionSecret = bootOwnSecret
			cfg.TrustedIssuer = door.settings()
			cfg.TrustedIssuer.Audience = ""
		},
		"with an issuer that is not https": func(cfg *config.Config) {
			cfg.SessionSecret = bootOwnSecret
			cfg.TrustedIssuer = door.settings()
			cfg.TrustedIssuer.Issuer = "http://door.example"
		},
	}
	for name, configure := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			configure(&cfg)
			srv := bootServe(t, cfg)

			requireUnmountedAssert(t, srv, door.assertionBody(t, bootOwnerEmail))
			if offersTheDoor(t, srv) {
				t.Fatalf("the sign-in screen offers a door the instance cannot take an assertion from")
			}
			for _, p := range probesWithoutSession {
				if got := serveCall(t, srv, p.method, p.path, "", nil); got.status != http.StatusUnauthorized {
					t.Fatalf("%s %s with no session answered %d %s, want 401", p.method, p.path, got.status, got.body)
				}
			}
		})
	}
}

// An instance with nothing configured is local development and stays exactly
// that: no assertion route, no door, and the protected API answers without a
// session.
func TestServe_NothingConfiguredKeepsDevMode(t *testing.T) {
	door := newBootDoor(t)
	cfg := config.Default()
	if cfg.AuthEnabled() {
		t.Fatalf("the default configuration must have no sign-in")
	}
	srv := bootServe(t, cfg)

	requireUnmountedAssert(t, srv, door.assertionBody(t, bootOwnerEmail))
	if offersTheDoor(t, srv) {
		t.Fatalf("an instance with no trusted issuer offers a door")
	}
	if got := serveCall(t, srv, http.MethodGet, "/api/resource-types", "", nil); got.status != http.StatusOK {
		t.Fatalf("GET /api/resource-types in dev mode answered %d %s, want 200", got.status, got.body)
	}
	if got := serveCall(t, srv, http.MethodPost, "/api/mcp", "", nil); got.status == http.StatusUnauthorized {
		t.Fatalf("POST /api/mcp in dev mode answered 401 %s; dev mode has no sign-in to ask for", got.body)
	}
}
