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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

// fakeGateAuthService stubs only the two AuthenticationService methods the
// callback reaches on the OAuth path: ExchangeCode (returns a fixed identity)
// and FindOrCreateAgent (records that it ran, then errors so the handler stops
// right after the gate without needing real entities). Every other interface
// method is inherited from the nil embedded interface and is never called here.
type fakeGateAuthService struct {
	authapp.AuthenticationService
	email             string
	findOrCreateCalls int
}

func (f *fakeGateAuthService) ExchangeCode(ctx context.Context, code, codeVerifier, provider, redirectURI string) (*authapp.AuthResult, error) {
	return &authapp.AuthResult{UserInfo: authapp.UserInfo{Email: f.email}}, nil
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
func newGateHandler(t *testing.T, email string, allowlist []string) (echo.HandlerFunc, *fakeGateAuthService, AuthCodeRepository, string, string) {
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
	auth := &fakeGateAuthService{email: email}
	store := &gateSessionStore{code: code.Code, state: state}
	h := Callback(auth, store, codeRepo, nil, noopLogger{}, "https://twin.example", allowlist)
	return h, auth, codeRepo, code.Code, state
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
