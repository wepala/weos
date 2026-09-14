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
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
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
// and serves them on a test listener. extra is handed to buildServer, so a
// test can observe what serve's own graph provides.
func bootServe(t *testing.T, cfg config.Config, extra ...fx.Option) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	cfg.DatabaseDSN = filepath.Join(dir, "weos.db")
	cfg.Storage.LocalPath = filepath.Join(dir, "uploads")
	cfg.LogLevel = "error"

	e, app, err := buildServer(cfg, extra...)
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
	return serveSend(t, req)
}

// serveCallWithToken sends method path carrying only an Authorization header
// with token and no cookie — the request an app in a native shell makes, whose
// web view holds no cookie for the instance.
func serveCallWithToken(t *testing.T, srv *httptest.Server, method, path, token string) serveAnswer {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return serveSend(t, req)
}

func serveSend(t *testing.T, req *http.Request) serveAnswer {
	t.Helper()
	method, path := req.Method, req.URL.Path
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

// wm-hg3xf. The door relays the assertion's answer to an app whose web view
// holds no cookie for the instance, so the app reads who is signed in with the
// token that answer carried. The identity read must answer that token with the
// body the session gets, and refuse a token the instance did not sign. No
// failure message here prints the sign-in's answer: it holds the token.
func TestServe_IdentityReadAnswersTheTokenTheAssertionIssued(t *testing.T) {
	door := newBootDoor(t)
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.TrustedIssuer = door.settings()
	srv := bootServe(t, cfg)

	signIn := serveCall(t, srv, http.MethodPost, "/api/auth/assert", door.assertionBody(t, bootOwnerEmail), nil)
	if signIn.status != http.StatusOK {
		t.Fatalf("POST /api/auth/assert answered %d, want 200", signIn.status)
	}
	var answer struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(signIn.body), &answer); err != nil || answer.Data.Token == "" {
		t.Fatalf("the asserted sign-in's answer carries no token (decode error: %v)", err)
	}

	byToken := serveCallWithToken(t, srv, http.MethodGet, "/api/auth/me", answer.Data.Token)
	if byToken.status != http.StatusOK || !strings.Contains(byToken.body, bootOwnerEmail) {
		t.Fatalf("GET /api/auth/me with only the token answered %d %s, want 200 naming %s",
			byToken.status, byToken.body, bootOwnerEmail)
	}
	bySession := serveCall(t, srv, http.MethodGet, "/api/auth/me", "", signIn.cookies)
	if byToken.body != bySession.body {
		t.Fatalf("GET /api/auth/me answered the token with %s and the session with %s", byToken.body, bySession.body)
	}

	tampered := serveCallWithToken(t, srv, http.MethodGet, "/api/auth/me", answer.Data.Token+"x")
	if tampered.status != http.StatusUnauthorized || strings.Contains(tampered.body, bootOwnerEmail) {
		t.Fatalf("GET /api/auth/me with a tampered token answered %d %s, want 401", tampered.status, tampered.body)
	}
}

// wm-6nfzq. Sign-out ends the session. It does not end the bearer token the
// same sign-in handed back: that token is a stateless access token, and no
// record on the instance says it was signed out, so it still reads the
// identity until it expires, one hour after it was issued. This pins what
// happens today, so a change to it is a decision made on purpose. An app that
// signs out must delete the token it holds. No failure message prints the
// sign-in's answer: it holds the token.
func TestServe_SignOutDoesNotEndTheTokenTheSignInIssued(t *testing.T) {
	door := newBootDoor(t)
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.TrustedIssuer = door.settings()
	srv := bootServe(t, cfg)

	signIn := serveCall(t, srv, http.MethodPost, "/api/auth/assert", door.assertionBody(t, bootOwnerEmail), nil)
	if signIn.status != http.StatusOK {
		t.Fatalf("POST /api/auth/assert answered %d, want 200", signIn.status)
	}
	var answer struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(signIn.body), &answer); err != nil || answer.Data.Token == "" {
		t.Fatalf("the asserted sign-in's answer carries no token (decode error: %v)", err)
	}
	if before := serveCallWithToken(t, srv, http.MethodGet, "/api/auth/me", answer.Data.Token); before.status != http.StatusOK {
		t.Fatalf("GET /api/auth/me with the token before sign-out answered %d %s, want 200", before.status, before.body)
	}

	signOut := serveCall(t, srv, http.MethodPost, "/api/auth/logout", "", signIn.cookies)
	if signOut.status != http.StatusOK {
		t.Fatalf("POST /api/auth/logout answered %d %s, want 200", signOut.status, signOut.body)
	}
	if bySession := serveCall(t, srv, http.MethodGet, "/api/auth/me", "", signIn.cookies); bySession.status != http.StatusUnauthorized {
		t.Fatalf("GET /api/auth/me with the signed-out session answered %d %s, want 401", bySession.status, bySession.body)
	}

	after := serveCallWithToken(t, srv, http.MethodGet, "/api/auth/me", answer.Data.Token)
	if after.status != http.StatusOK || !strings.Contains(after.body, bootOwnerEmail) {
		t.Fatalf("GET /api/auth/me with the token after sign-out answered %d %s; today sign-out does not end a token, "+
			"so it should still answer 200 naming %s. If sign-out now ends tokens, that is a decision: update this test and the release notes",
			after.status, after.body, bootOwnerEmail)
	}
}

// bootLogCapture records every line serve's graph logs while it boots.
type bootLogCapture struct {
	mu    sync.Mutex
	lines []string
}

func (l *bootLogCapture) add(level, msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf("%s: %s %v", level, msg, fields))
}

func (l *bootLogCapture) Debug(_ context.Context, m string, f ...any) { l.add("debug", m, f) }
func (l *bootLogCapture) Info(_ context.Context, m string, f ...any)  { l.add("info", m, f) }
func (l *bootLogCapture) Warn(_ context.Context, m string, f ...any)  { l.add("warn", m, f) }
func (l *bootLogCapture) Error(_ context.Context, m string, f ...any) { l.add("error", m, f) }

// mentioning is every recorded line, at any level, that contains s.
func (l *bootLogCapture) mentioning(s string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range l.lines {
		if strings.Contains(line, s) {
			out = append(out, line)
		}
	}
	return out
}

func (l *bootLogCapture) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func bootSigningKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate the signing key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// wm-a6xb6. With no JWT_SIGNING_KEY, every boot signs tokens with a key made
// at boot, so a restart or a deploy ends every bearer token a native app
// holds and signs the app out. An operator who turns on a trusted issuer is
// told so at boot: one warning that names JWT_SIGNING_KEY and says the tokens
// will not survive a restart. The key itself is never logged.
func TestServe_WarnsAtBootWhenATrustedIssuersTokensWillNotSurviveARestart(t *testing.T) {
	door := newBootDoor(t)
	key := bootSigningKeyPEM(t)
	cases := map[string]struct {
		issuer bool
		key    string
		warns  int
	}{
		"a trusted issuer with no signing key":   {issuer: true, key: "", warns: 1},
		"a trusted issuer with a signing key":    {issuer: true, key: key, warns: 0},
		"no trusted issuer and no signing key":   {issuer: false, key: "", warns: 0},
		"a trusted issuer with an ephemeral key": {issuer: true, key: "auto", warns: 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			cfg.SessionSecret = bootOwnSecret
			if c.issuer {
				cfg.TrustedIssuer = door.settings()
			}
			cfg.OAuth.JWTSigningKey = c.key
			logs := &bootLogCapture{}
			bootServe(t, cfg, fx.Decorate(func(entities.Logger) entities.Logger { return logs }))

			lines := logs.mentioning("JWT_SIGNING_KEY")
			if len(lines) != c.warns {
				t.Fatalf("got %d boot lines naming JWT_SIGNING_KEY, want %d:\n%s", len(lines), c.warns, logs.text())
			}
			if c.warns == 1 {
				if !strings.HasPrefix(lines[0], "warn: ") {
					t.Fatalf("the boot line %q is not a warning", lines[0])
				}
				if !strings.Contains(lines[0], "bearer token") || !strings.Contains(lines[0], "restart") {
					t.Fatalf("the warning %q does not say bearer tokens will not survive a restart", lines[0])
				}
			}
			logged := logs.text()
			if strings.Contains(logged, "PRIVATE KEY") {
				t.Fatalf("the boot log carries the signing key")
			}
			if c.key != "" && c.key != "auto" {
				body := strings.Split(strings.TrimSpace(c.key), "\n")[1]
				if strings.Contains(logged, body) {
					t.Fatalf("the boot log carries the signing key")
				}
			}
		})
	}
}
