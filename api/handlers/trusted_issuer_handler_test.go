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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/labstack/echo/v4"
)

// A token-shaped value. The handler never parses it itself; the tests only
// need something whose every segment can be looked for in the log.
const presentedAssertion = "eyJhbGciOiJFUzI1NiIsImtpZCI6ImRvb3ItMjAyNi0wOSJ9." +
	"eyJzdWIiOiIxMDgyMzQ1Njc4OTAiLCJlbWFpbCI6Im9wc0BoYXJib3JsZWdhbC5leGFtcGxlIn0." +
	"c2lnbmF0dXJlLWJ5dGVzLW9mLXRoZS1kb29y"

type fakeAssertionVerifier struct {
	identity trustedissuer.Identity
	err      error
	got      []string
}

func (f *fakeAssertionVerifier) Verify(_ context.Context, assertion string) (trustedissuer.Identity, error) {
	f.got = append(f.got, assertion)
	return f.identity, f.err
}

type assertAuthService struct {
	authapp.AuthenticationService

	agent   *authentities.Agent
	cred    *authentities.Credential
	account *authentities.Account
	findErr error
	session *authentities.AuthSession

	findCalls   int
	gotUserInfo authapp.UserInfo
}

func (f *assertAuthService) FindOrCreateAgent(_ context.Context, info authapp.UserInfo) (
	*authentities.Agent, *authentities.Credential, *authentities.Account, error,
) {
	f.findCalls++
	f.gotUserInfo = info
	return f.agent, f.cred, f.account, f.findErr
}

func (f *assertAuthService) CreateSession(
	_ context.Context, _, _, _, _, _ string, _ time.Duration, _ ...authapp.AccountTrustOption,
) (*authentities.AuthSession, error) {
	return f.session, nil
}

func (f *assertAuthService) IssueIdentityToken(
	_ context.Context, _ *authentities.Agent, _ string, _ ...authapp.AccountTrustOption,
) (string, error) {
	return "session-jwt", nil
}

type assertionLogLine struct {
	level, msg string
	fields     []any
}

type assertionLogCapture struct {
	mu    sync.Mutex
	lines []assertionLogLine
}

func (l *assertionLogCapture) add(level, msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, assertionLogLine{level, msg, fields})
}

func (l *assertionLogCapture) Debug(_ context.Context, m string, f ...any) { l.add("debug", m, f) }
func (l *assertionLogCapture) Info(_ context.Context, m string, f ...any)  { l.add("info", m, f) }
func (l *assertionLogCapture) Warn(_ context.Context, m string, f ...any)  { l.add("warn", m, f) }
func (l *assertionLogCapture) Error(_ context.Context, m string, f ...any) { l.add("error", m, f) }

func (l *assertionLogCapture) atLevel(level string) []assertionLogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []assertionLogLine
	for _, line := range l.lines {
		if line.level == level {
			out = append(out, line)
		}
	}
	return out
}

func (l *assertionLogCapture) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	for _, line := range l.lines {
		fmt.Fprintf(&b, "%s %s %v\n", line.level, line.msg, line.fields)
	}
	return b.String()
}

func field(line assertionLogLine, key string) (any, bool) {
	for i := 0; i+1 < len(line.fields); i += 2 {
		if k, ok := line.fields[i].(string); ok && k == key {
			return line.fields[i+1], true
		}
	}
	return nil, false
}

func requireNoAssertionLogged(t *testing.T, logs *assertionLogCapture) {
	t.Helper()
	logged := logs.text()
	for i, part := range strings.Split(presentedAssertion, ".") {
		if strings.Contains(logged, part) {
			t.Fatalf("the log carries segment %d of the assertion:\n%s", i, logged)
		}
	}
}

func newTrustedIssuerHandler(
	verifier handlers.AssertionVerifier, auth authapp.AuthenticationService, logs *assertionLogCapture,
) (*handlers.TrustedIssuerHandler, *fakeSessionManager) {
	sm := &fakeSessionManager{}
	sessions := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    auth,
		SessionManager: sm,
		Logger:         logs,
	})
	return handlers.NewTrustedIssuerHandler(handlers.TrustedIssuerHandlerConfig{
		Verifier:    verifier,
		AuthService: auth,
		Sessions:    sessions,
		Logger:      logs,
	}), sm
}

func postAssertion(t *testing.T, h *handlers.TrustedIssuerHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(newJSONRequest(http.MethodPost, "/api/auth/assert", body), rec)
	if err := h.Assert(c); err != nil {
		t.Fatalf("Assert returned an error instead of answering: %v", err)
	}
	return rec
}

type assertAnswer struct {
	Error string          `json:"error"`
	Code  string          `json:"code"`
	Data  json.RawMessage `json:"data"`
}

func readAssertAnswer(t *testing.T, rec *httptest.ResponseRecorder) assertAnswer {
	t.Helper()
	var a assertAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("answer is not JSON: %v (%s)", err, rec.Body.String())
	}
	return a
}

func assertionBody(assertion string) string {
	b, _ := json.Marshal(map[string]string{"assertion": assertion})
	return string(b)
}

// --- refusals ---

func TestAssertRefusesWithTheReasonTheVerifierGave(t *testing.T) {
	reasons := []trustedissuer.Reason{
		trustedissuer.ReasonSignature, trustedissuer.ReasonKidMiss, trustedissuer.ReasonIssuer,
		trustedissuer.ReasonAudience, trustedissuer.ReasonExpired, trustedissuer.ReasonWindow,
		trustedissuer.ReasonReplay, trustedissuer.ReasonClaims,
	}
	for _, reason := range reasons {
		t.Run(string(reason), func(t *testing.T) {
			logs := &assertionLogCapture{}
			verifier := &fakeAssertionVerifier{err: &trustedissuer.Refusal{Reason: reason, Detail: "because"}}
			auth := &assertAuthService{}
			h, sm := newTrustedIssuerHandler(verifier, auth, logs)

			rec := postAssertion(t, h, assertionBody(presentedAssertion))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body.String())
			}
			answer := readAssertAnswer(t, rec)
			if answer.Code != string(reason) {
				t.Fatalf("answer code = %q, want %q", answer.Code, reason)
			}
			if answer.Error == "" {
				t.Fatalf("expected a human-readable error beside the code")
			}
			if len(verifier.got) != 1 || verifier.got[0] != presentedAssertion {
				t.Fatalf("verifier saw %v, want the presented assertion once", verifier.got)
			}
			if auth.findCalls != 0 || sm.createCalls != 0 {
				t.Fatalf("a refused assertion reached sign-in: find=%d session=%d", auth.findCalls, sm.createCalls)
			}
			if cookies := rec.Header().Values("Set-Cookie"); len(cookies) != 0 {
				t.Fatalf("a refusal set cookies: %v", cookies)
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

func TestAssertRefusesARequestCarryingNoUsableAssertion(t *testing.T) {
	bodies := map[string]string{
		"not JSON":           `{"assertion": "trunc`,
		"no assertion field": `{}`,
		"assertion a number": `{"assertion": 42}`,
		"empty body":         ``,
		"empty assertion":    `{"assertion": ""}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			logs := &assertionLogCapture{}
			verifier := trustedissuer.NewVerifier(trustedissuer.Config{
				Issuer: "https://money.weos.cloud", JWKSURL: "https://money.weos.cloud/door/jwks.json",
				Audience: "a1b2c3d4", Providers: []string{"google"},
			})
			h, _ := newTrustedIssuerHandler(verifier, &assertAuthService{}, logs)
			rec := postAssertion(t, h, body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body.String())
			}
			if code := readAssertAnswer(t, rec).Code; code != string(trustedissuer.ReasonSignature) {
				t.Fatalf("answer code = %q, want signature", code)
			}
		})
	}
}

func TestAssertTreatsAnUnexpectedVerifierErrorAsASignatureRefusal(t *testing.T) {
	logs := &assertionLogCapture{}
	h, _ := newTrustedIssuerHandler(&fakeAssertionVerifier{err: errors.New("boom")}, &assertAuthService{}, logs)
	rec := postAssertion(t, h, assertionBody(presentedAssertion))
	if rec.Code != http.StatusUnauthorized || readAssertAnswer(t, rec).Code != string(trustedissuer.ReasonSignature) {
		t.Fatalf("expected a 401 signature refusal, got %d %s", rec.Code, rec.Body.String())
	}
}

// --- an accepted assertion ---

func acceptedIdentity() trustedissuer.Identity {
	return trustedissuer.Identity{
		Subject: "108234567890", Email: "ops@harborlegal.example", Provider: "google",
		Name: "Harbor Ops", JTI: "3f9c0a",
	}
}

func signedInAuthService(t *testing.T) *assertAuthService {
	return &assertAuthService{
		agent:   newAgent(t, "agent-ops", "Harbor Ops"),
		cred:    newCredential(t),
		account: newAccount(t, "account-ops", "Harbor Ops"),
		session: newAuthSession(t),
	}
}

func TestAssertSignsInThePersonTheAssertionNames(t *testing.T) {
	logs := &assertionLogCapture{}
	auth := signedInAuthService(t)
	h, sm := newTrustedIssuerHandler(&fakeAssertionVerifier{identity: acceptedIdentity()}, auth, logs)

	rec := postAssertion(t, h, assertionBody(presentedAssertion))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	want := authapp.UserInfo{
		ProviderUserID: "108234567890",
		Email:          "ops@harborlegal.example",
		DisplayName:    "Harbor Ops",
		Provider:       "google",
	}
	if auth.findCalls != 1 || auth.gotUserInfo != want {
		t.Fatalf("FindOrCreateAgent got %+v (%d calls), want %+v", auth.gotUserInfo, auth.findCalls, want)
	}
	if sm.createCalls != 1 {
		t.Fatalf("expected one HTTP session, got %d", sm.createCalls)
	}
	answer := readAssertAnswer(t, rec)
	if answer.Code != "" || answer.Error != "" {
		t.Fatalf("an accepted assertion names a refusal: %s", rec.Body.String())
	}
	var data struct {
		Agent struct {
			Email string `json:"email"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(answer.Data, &data); err != nil || data.Agent.Email != "ops@harborlegal.example" {
		t.Fatalf("answer does not name the person: %s", rec.Body.String())
	}
	requireNoAssertionLogged(t, logs)
}

func TestAssertNamesAnUnnamedPersonAfterTheirEmail(t *testing.T) {
	auth := signedInAuthService(t)
	identity := acceptedIdentity()
	identity.Name = ""
	h, _ := newTrustedIssuerHandler(&fakeAssertionVerifier{identity: identity}, auth, &assertionLogCapture{})
	if rec := postAssertion(t, h, assertionBody(presentedAssertion)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if auth.gotUserInfo.DisplayName != "ops" {
		t.Fatalf("DisplayName = %q, want the email's local part", auth.gotUserInfo.DisplayName)
	}
}

func TestAssertAnswersAFailureWhenTheAccountCannotBeResolved(t *testing.T) {
	logs := &assertionLogCapture{}
	auth := &assertAuthService{findErr: errors.New("database unavailable")}
	h, sm := newTrustedIssuerHandler(&fakeAssertionVerifier{identity: acceptedIdentity()}, auth, logs)

	rec := postAssertion(t, h, assertionBody(presentedAssertion))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (%s)", rec.Code, rec.Body.String())
	}
	if sm.createCalls != 0 || len(rec.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("a failed sign-in created a session")
	}
	if len(logs.atLevel("error")) != 1 {
		t.Fatalf("expected the failure to be logged once, got:\n%s", logs.text())
	}
	requireNoAssertionLogged(t, logs)
}

// --- mount or not ---

func TestMountTrustedIssuerAssertion(t *testing.T) {
	const (
		iss = "https://money.weos.cloud"
		url = "https://money.weos.cloud/door/jwks.json"
		aud = "a1b2c3d4"
	)
	cases := map[string]struct {
		settings    config.TrustedIssuerConfig
		wantMounted bool
		wantWarning string // the missing keys the one warning names; "" for no warning
	}{
		"nothing configured":  {config.TrustedIssuerConfig{}, false, ""},
		"all three":           {config.TrustedIssuerConfig{Issuer: iss, JWKSURL: url, Audience: aud}, true, ""},
		"no audience":         {config.TrustedIssuerConfig{Issuer: iss, JWKSURL: url}, false, "TRUSTED_ISSUER_AUDIENCE"},
		"no key list":         {config.TrustedIssuerConfig{Issuer: iss, Audience: aud}, false, "TRUSTED_ISSUER_JWKS_URL"},
		"no issuer":           {config.TrustedIssuerConfig{JWKSURL: url, Audience: aud}, false, "TRUSTED_ISSUER"},
		"only the issuer":     {config.TrustedIssuerConfig{Issuer: iss}, false, "TRUSTED_ISSUER_JWKS_URL, TRUSTED_ISSUER_AUDIENCE"},
		"only the key list":   {config.TrustedIssuerConfig{JWKSURL: url}, false, "TRUSTED_ISSUER, TRUSTED_ISSUER_AUDIENCE"},
		"only the audience":   {config.TrustedIssuerConfig{Audience: aud}, false, "TRUSTED_ISSUER, TRUSTED_ISSUER_JWKS_URL"},
		"key list over http":  {config.TrustedIssuerConfig{Issuer: iss, JWKSURL: "http://money.weos.cloud/door/jwks.json", Audience: aud}, false, "-"},
		"loopback key list":   {config.TrustedIssuerConfig{Issuer: iss, JWKSURL: "http://127.0.0.1:9000/jwks.json", Audience: aud}, true, ""},
		"not a key-list URL":  {config.TrustedIssuerConfig{Issuer: iss, JWKSURL: "jwks.json", Audience: aud}, false, "-"},
		"whitespace audience": {config.TrustedIssuerConfig{Issuer: iss, JWKSURL: url, Audience: "  "}, false, "TRUSTED_ISSUER_AUDIENCE"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			logs := &assertionLogCapture{}
			builds := 0
			e := echo.New()
			api := e.Group("/api")
			mounted := handlers.MountTrustedIssuerAssertion(context.Background(), api, c.settings, logs,
				func() *handlers.TrustedIssuerHandler {
					builds++
					h, _ := newTrustedIssuerHandler(
						&fakeAssertionVerifier{err: &trustedissuer.Refusal{Reason: trustedissuer.ReasonSignature}},
						// Its own log: a request served below would otherwise
						// add the handler's refusal line to the boot warnings.
						&assertAuthService{}, &assertionLogCapture{})
					return h
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
			assert, never := serve("/api/auth/assert"), serve("/api/auth/enroll")
			if c.wantMounted {
				if assert.Code != http.StatusUnauthorized {
					t.Fatalf("mounted route answered %d, want the handler's 401", assert.Code)
				}
			} else if assert.Code != never.Code || assert.Body.String() != never.Body.String() {
				t.Fatalf("unmounted route answered %d %q, a never-mounted path %d %q",
					assert.Code, assert.Body.String(), never.Code, never.Body.String())
			}

			warns := logs.atLevel("warn")
			switch c.wantWarning {
			case "":
				if len(warns) != 0 {
					t.Fatalf("expected no boot warning, got:\n%s", logs.text())
				}
			case "-":
				if len(warns) != 1 {
					t.Fatalf("expected one boot warning, got:\n%s", logs.text())
				}
			default:
				if len(warns) != 1 {
					t.Fatalf("expected one boot warning, got:\n%s", logs.text())
				}
				if got, _ := field(warns[0], "missing"); got != c.wantWarning {
					t.Fatalf("warning names missing %v, want %q", got, c.wantWarning)
				}
			}
		})
	}
}
