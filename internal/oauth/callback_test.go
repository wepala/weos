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

package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

// fakeGateAuthService stubs only the two AuthenticationService methods the
// callback reaches on the OAuth path: ExchangeCode (returns a fixed identity)
// and FindOrCreateAgent (records that it ran, then errors so the handler stops
// right after the gate without needing real entities). FindOrCreateAgent is
// the only call on this path that writes a credential, so a zero call count
// means nothing was written. Every other interface method is inherited from
// the nil embedded interface and is never called here.
type fakeGateAuthService struct {
	authapp.AuthenticationService
	userInfo          authapp.UserInfo
	findOrCreateCalls int
}

func (f *fakeGateAuthService) ExchangeCode(ctx context.Context, code, codeVerifier, provider, redirectURI string) (*authapp.AuthResult, error) {
	return &authapp.AuthResult{UserInfo: f.userInfo}, nil
}

func (f *fakeGateAuthService) FindOrCreateAgent(ctx context.Context, userInfo authapp.UserInfo) (*authentities.Agent, *authentities.Credential, *authentities.Account, error) {
	f.findOrCreateCalls++
	return nil, nil, nil, errors.New("stop after gate")
}

// gateSessionStore returns a session pre-populated with a valid pending-flow
// state so the callback reaches the allowlist gate.
type gateSessionStore struct{ code, state string }

func (s *gateSessionStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	sess := sessions.NewSession(s, name)
	sess.Values["oauth_code"] = s.code
	sess.Values["oauth_code_verifier"] = "verifier"
	sess.Values["oauth_state"] = s.state
	return sess, nil
}

func (s *gateSessionStore) New(r *http.Request, name string) (*sessions.Session, error) {
	return s.Get(r, name)
}

func (s *gateSessionStore) Save(r *http.Request, w http.ResponseWriter, sess *sessions.Session) error {
	return nil
}

// newGateHandler wires a Callback handler to in-memory fakes with a real pending
// authorization code, and returns the handler plus the handles a test asserts on.
// The identity is a Google profile whose email Google has verified.
func newGateHandler(t *testing.T, email string, allowlist []string) (echo.HandlerFunc, *fakeGateAuthService, AuthCodeRepository, string, string) {
	t.Helper()
	h, auth, codeRepo, code, state := newGateHandlerFor(t, authapp.UserInfo{
		Provider:       "google",
		ProviderUserID: "google-sub-1",
		Email:          email,
		EmailVerified:  true,
	}, allowlist, noopLogger{})
	return h, auth, codeRepo, code, state
}

// newGateHandlerFor is newGateHandler for a caller that sets the whole profile
// the provider returns, and the logger the handler writes to.
func newGateHandlerFor(t *testing.T, userInfo authapp.UserInfo, allowlist []string, logger entities.Logger) (echo.HandlerFunc, *fakeGateAuthService, AuthCodeRepository, string, string) {
	t.Helper()
	db := setupTestDB(t)
	codeRepo := NewAuthCodeRepository(db)
	code := &OAuthAuthorizationCode{
		ClientID:            "client-1",
		RedirectURI:         "https://client.example/cb",
		CodeChallenge:       "challenge",
		CodeChallengeMethod: "S256",
		Status:              StatusPending,
	}
	mustNoErr(t, codeRepo.Create(context.Background(), code), "create pending code")

	state := "state-123"
	auth := &fakeGateAuthService{userInfo: userInfo}
	store := &gateSessionStore{code: code.Code, state: state}
	h := Callback(auth, store, codeRepo, nil, logger, "https://twin.example", allowlist)
	return h, auth, codeRepo, code.Code, state
}

// capturedLog is one line a handler logged.
type capturedLog struct {
	level, msg string
	fields     []any
}

// captureLogger records every line so a test can check what a refusal logs.
type captureLogger struct {
	mu    sync.Mutex
	lines []capturedLog
}

func (l *captureLogger) record(level, msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, capturedLog{level: level, msg: msg, fields: fields})
}

func (l *captureLogger) Debug(_ context.Context, msg string, f ...any) { l.record("debug", msg, f) }
func (l *captureLogger) Info(_ context.Context, msg string, f ...any)  { l.record("info", msg, f) }
func (l *captureLogger) Warn(_ context.Context, msg string, f ...any)  { l.record("warn", msg, f) }
func (l *captureLogger) Error(_ context.Context, msg string, f ...any) { l.record("error", msg, f) }

// text flattens every logged line into one string.
func (l *captureLogger) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	for _, line := range l.lines {
		b.WriteString(line.level + " " + line.msg + " " + fmt.Sprint(line.fields...) + "\n")
	}
	return b.String()
}

func runCallback(t *testing.T, h echo.HandlerFunc, state string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state="+state+"&code=google-code", nil)
	rec := httptest.NewRecorder()
	if err := h(echo.New().NewContext(req, rec)); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return rec
}

func TestCallback_Allowlist_DeniesUnlistedEmail(t *testing.T) {
	t.Parallel()
	h, auth, codeRepo, code, state := newGateHandler(t, "stranger@example.com", []string{"akeem@example.com"})

	rec := runCallback(t, h, state)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "access_denied") {
		t.Errorf("body should contain access_denied: %s", rec.Body.String())
	}
	if auth.findOrCreateCalls != 0 {
		t.Errorf("FindOrCreateAgent ran %d times; a denied identity must never get an account", auth.findOrCreateCalls)
	}
	// The code must stay pending — never advanced to issued for a denied user.
	found, err := codeRepo.FindByCode(context.Background(), code)
	mustNoErr(t, err, "find code after deny")
	if found.Status != StatusPending {
		t.Errorf("code status = %q, want still %q", found.Status, StatusPending)
	}
}

func TestCallback_Allowlist_AdmitsListedEmail(t *testing.T) {
	t.Parallel()
	h, auth, _, _, state := newGateHandler(t, "akeem@example.com", []string{"akeem@example.com"})

	runCallback(t, h, state)

	if auth.findOrCreateCalls != 1 {
		t.Errorf("a listed email must pass the gate through to FindOrCreateAgent; calls=%d", auth.findOrCreateCalls)
	}
}

func TestCallback_Allowlist_IsCaseInsensitive(t *testing.T) {
	t.Parallel()
	h, auth, _, _, state := newGateHandler(t, "Akeem@Example.COM", []string{"akeem@example.com"})

	runCallback(t, h, state)

	if auth.findOrCreateCalls != 1 {
		t.Errorf("a mixed-case listed email must be admitted; calls=%d", auth.findOrCreateCalls)
	}
}

func TestCallback_NoAllowlist_AdmitsAnyIdentity(t *testing.T) {
	t.Parallel()
	h, auth, _, _, state := newGateHandler(t, "anyone@example.com", nil)

	runCallback(t, h, state)

	if auth.findOrCreateCalls != 1 {
		t.Errorf("with no allowlist the gate must be skipped and anyone admitted; calls=%d", auth.findOrCreateCalls)
	}
}

// A google or apple credential proves its email to owner binding, so the
// callback must not write one for an email the provider has not verified.
func TestCallback_UnverifiedProviderEmail_IsRefusedBeforeAnyCredential(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"google", "apple"} {
		for _, allowlist := range [][]string{nil, {"dana.reyes@example.com"}} {
			name := provider + "/no allowlist"
			if allowlist != nil {
				name = provider + "/listed email"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				logs := &captureLogger{}
				h, auth, codeRepo, code, state := newGateHandlerFor(t, authapp.UserInfo{
					Provider:       provider,
					ProviderUserID: provider + "-sub-7731",
					Email:          "dana.reyes@example.com",
					EmailVerified:  false,
				}, allowlist, logs)

				rec := runCallback(t, h, state)

				if rec.Code != http.StatusForbidden {
					t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
				}
				if !strings.Contains(rec.Body.String(), "access_denied") {
					t.Errorf("body should contain access_denied: %s", rec.Body.String())
				}
				if auth.findOrCreateCalls != 0 {
					t.Errorf("FindOrCreateAgent ran %d times; an unverified email must never get a credential", auth.findOrCreateCalls)
				}
				found, err := codeRepo.FindByCode(context.Background(), code)
				mustNoErr(t, err, "find code after refusal")
				if found.Status != StatusPending {
					t.Errorf("code status = %q, want still %q", found.Status, StatusPending)
				}
				logged := logs.text()
				if !strings.Contains(logged, provider) || !strings.Contains(logged, provider+"-sub-7731") {
					t.Errorf("the refusal should log the provider and subject id; got:\n%s", logged)
				}
				if strings.Contains(logged, "dana.reyes") {
					t.Errorf("the refusal must not log the email; got:\n%s", logged)
				}
			})
		}
	}
}

func TestCallback_VerifiedProviderEmail_IsAdmitted(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"google", "apple"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			h, auth, _, _, state := newGateHandlerFor(t, authapp.UserInfo{
				Provider:       provider,
				ProviderUserID: provider + "-sub-7731",
				Email:          "dana.reyes@example.com",
				EmailVerified:  true,
			}, nil, noopLogger{})

			runCallback(t, h, state)

			if auth.findOrCreateCalls != 1 {
				t.Errorf("a verified email must reach FindOrCreateAgent; calls=%d", auth.findOrCreateCalls)
			}
		})
	}
}

// A provider that sends no email_verified claim reports EmailVerified false for
// every profile, which says nothing; its sign-ins must not change.
func TestCallback_ProviderWithoutTheClaim_IsUnaffected(t *testing.T) {
	t.Parallel()
	h, auth, _, _, state := newGateHandlerFor(t, authapp.UserInfo{
		Provider:       "netsuite",
		ProviderUserID: "netsuite-4410",
		Email:          "dana.reyes@example.com",
	}, nil, noopLogger{})

	runCallback(t, h, state)

	if auth.findOrCreateCalls != 1 {
		t.Errorf("a provider without the claim must reach FindOrCreateAgent; calls=%d", auth.findOrCreateCalls)
	}
}
