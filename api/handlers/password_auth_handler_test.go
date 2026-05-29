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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/labstack/echo/v4"
)

// fakeAuthService embeds the AuthenticationService interface so unimplemented
// methods stay nil-pointer-panicking — only the calls the handler makes need
// real bodies, which keeps the fake small.
type fakeAuthService struct {
	authapp.AuthenticationService

	registerAgent   *authentities.Agent
	registerCred    *authentities.Credential
	registerAccount *authentities.Account
	registerErr     error

	verifyAgent   *authentities.Agent
	verifyCred    *authentities.Credential
	verifyAccount *authentities.Account
	verifyErr     error

	sessionResult *authentities.AuthSession
	sessionErr    error

	tokenString string
	tokenErr    error

	gotEmail       string
	gotDisplayName string
	gotPassword    string
}

func (f *fakeAuthService) RegisterPassword(_ context.Context, email, displayName, plaintext string) (
	*authentities.Agent, *authentities.Credential, *authentities.Account, error,
) {
	f.gotEmail = email
	f.gotDisplayName = displayName
	f.gotPassword = plaintext
	return f.registerAgent, f.registerCred, f.registerAccount, f.registerErr
}

func (f *fakeAuthService) VerifyPassword(_ context.Context, email, plaintext string) (
	*authentities.Agent, *authentities.Credential, *authentities.Account, error,
) {
	f.gotEmail = email
	f.gotPassword = plaintext
	return f.verifyAgent, f.verifyCred, f.verifyAccount, f.verifyErr
}

func (f *fakeAuthService) CreateSession(
	_ context.Context, _, _, _, _ string, _ time.Duration,
) (*authentities.AuthSession, error) {
	return f.sessionResult, f.sessionErr
}

func (f *fakeAuthService) IssueIdentityToken(
	_ context.Context, _ *authentities.Agent, _ string,
) (string, error) {
	return f.tokenString, f.tokenErr
}

type fakeSessionManager struct {
	session.SessionManager

	createCalls int
	createErr   error
	gotData     session.SessionData
}

func (f *fakeSessionManager) CreateHTTPSession(_ http.ResponseWriter, _ *http.Request, data session.SessionData) error {
	f.createCalls++
	f.gotData = data
	return f.createErr
}

func newAgent(t *testing.T, id, name string) *authentities.Agent {
	t.Helper()
	a, err := (&authentities.Agent{}).With(id, name, authentities.AgentTypePerson)
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}
	return a
}

func newCredential(t *testing.T) *authentities.Credential {
	t.Helper()
	c, err := (&authentities.Credential{}).With(
		"cred-1", "agent-1", "password", "user@example.com", "user@example.com", "alice",
	)
	if err != nil {
		t.Fatalf("build credential: %v", err)
	}
	return c
}

func newAccount(t *testing.T, id, name string) *authentities.Account {
	t.Helper()
	a, err := (&authentities.Account{}).With(id, name, authentities.AccountTypePersonal)
	if err != nil {
		t.Fatalf("build account: %v", err)
	}
	return a
}

func newAuthSession(t *testing.T) *authentities.AuthSession {
	t.Helper()
	s, err := (&authentities.AuthSession{}).With(
		"sess-1", "agent-1", "cred-1", "127.0.0.1", "go-test", time.Now().Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("build auth session: %v", err)
	}
	return s
}

func newJSONRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestPasswordAuthHandler_Register_MissingFields(t *testing.T) {
	h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    &fakeAuthService{},
		SessionManager: &fakeSessionManager{},
		Logger:         nopLogger{},
	})

	cases := []struct {
		name string
		body string
	}{
		{"empty body", `{}`},
		{"missing password", `{"email":"u@example.com"}`},
		{"missing email", `{"password":"pw"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(newJSONRequest(http.MethodPost, "/api/auth/register", tc.body), rec)
			if err := h.Register(c); err != nil {
				t.Fatalf("Register: %v", err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestPasswordAuthHandler_Register_DefaultsDisplayNameToLocalPart(t *testing.T) {
	auth := &fakeAuthService{
		registerAgent:   newAgent(t, "agent-1", "alice"),
		registerCred:    newCredential(t),
		registerAccount: newAccount(t, "acct-1", "alice"),
		sessionResult:   newAuthSession(t),
	}
	sm := &fakeSessionManager{}
	h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    auth,
		SessionManager: sm,
		Logger:         nopLogger{},
	})

	rec := httptest.NewRecorder()
	req := newJSONRequest(http.MethodPost, "/api/auth/register", `{"email":"alice@example.com","password":"pw"}`)
	c := echo.New().NewContext(req, rec)

	if err := h.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if auth.gotDisplayName != "alice" {
		t.Errorf("display name = %q, want %q (should default to email local-part)", auth.gotDisplayName, "alice")
	}
}

func TestPasswordAuthHandler_Register_ErrorMapping(t *testing.T) {
	cases := []struct {
		name      string
		regErr    error
		wantCode  int
		wantError string
	}{
		{"email taken", authapp.ErrEmailAlreadyTaken, http.StatusConflict, "email already registered"},
		{"password support missing", authapp.ErrPasswordSupportNotConfigured, http.StatusServiceUnavailable, "password registration unavailable"},
		{"unknown error", errors.New("boom"), http.StatusInternalServerError, "failed to register account"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
				AuthService:    &fakeAuthService{registerErr: tc.regErr},
				SessionManager: &fakeSessionManager{},
				Logger:         nopLogger{},
			})

			rec := httptest.NewRecorder()
			req := newJSONRequest(http.MethodPost, "/api/auth/register",
				`{"email":"u@example.com","password":"pw","display_name":"u"}`)
			c := echo.New().NewContext(req, rec)
			if err := h.Register(c); err != nil {
				t.Fatalf("Register: %v", err)
			}
			if rec.Code != tc.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			var env struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &env)
			if env.Error != tc.wantError {
				t.Errorf("error = %q, want %q", env.Error, tc.wantError)
			}
		})
	}
}

func TestPasswordAuthHandler_Login_InvalidPassword(t *testing.T) {
	h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    &fakeAuthService{verifyErr: authapp.ErrInvalidPassword},
		SessionManager: &fakeSessionManager{},
		Logger:         nopLogger{},
	})

	rec := httptest.NewRecorder()
	req := newJSONRequest(http.MethodPost, "/api/auth/password-login",
		`{"email":"u@example.com","password":"wrong"}`)
	c := echo.New().NewContext(req, rec)
	if err := h.Login(c); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPasswordAuthHandler_Login_SetsSessionAndJWTCookie(t *testing.T) {
	auth := &fakeAuthService{
		verifyAgent:   newAgent(t, "agent-1", "alice"),
		verifyCred:    newCredential(t),
		verifyAccount: newAccount(t, "acct-1", "alice"),
		sessionResult: newAuthSession(t),
		tokenString:   "the.jwt.token",
	}
	sm := &fakeSessionManager{}
	h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    auth,
		SessionManager: sm,
		Logger:         nopLogger{},
		// SecureCookies left false — the test runs over plain HTTP and the
		// Set-Cookie header should reflect that without forcing Secure.
	})

	rec := httptest.NewRecorder()
	req := newJSONRequest(http.MethodPost, "/api/auth/password-login",
		`{"email":"alice@example.com","password":"pw"}`)
	c := echo.New().NewContext(req, rec)
	if err := h.Login(c); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if sm.createCalls != 1 {
		t.Errorf("CreateHTTPSession calls = %d, want 1", sm.createCalls)
	}
	if sm.gotData.AccountID != "acct-1" {
		t.Errorf("session AccountID = %q, want %q", sm.gotData.AccountID, "acct-1")
	}

	cookie := findCookie(rec.Result().Cookies(), "pericarp_token")
	if cookie == nil {
		t.Fatal("missing JWT cookie on successful login")
	}
	if cookie.Value != "the.jwt.token" {
		t.Errorf("cookie value = %q, want %q", cookie.Value, "the.jwt.token")
	}
	if cookie.Secure {
		t.Error("cookie Secure = true, want false (SecureCookies not configured)")
	}
	if !cookie.HttpOnly {
		t.Error("cookie HttpOnly = false, want true")
	}
	if cookie.MaxAge <= 0 {
		t.Errorf("cookie MaxAge = %d, want >0", cookie.MaxAge)
	}
}

func TestPasswordAuthHandler_Login_TokenIssuanceFailureStillSucceeds(t *testing.T) {
	auth := &fakeAuthService{
		verifyAgent:   newAgent(t, "agent-1", "alice"),
		verifyCred:    newCredential(t),
		sessionResult: newAuthSession(t),
		// Pericarp may still hand back a token alongside an error; the
		// handler should drop both the cookie *and* the body token so the
		// client doesn't see a JWT it can't actually use.
		tokenString: "leaked.jwt.token",
		tokenErr:    errors.New("jwt down"),
	}
	h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    auth,
		SessionManager: &fakeSessionManager{},
		Logger:         nopLogger{},
	})

	rec := httptest.NewRecorder()
	req := newJSONRequest(http.MethodPost, "/api/auth/password-login",
		`{"email":"u@example.com","password":"pw"}`)
	c := echo.New().NewContext(req, rec)
	if err := h.Login(c); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d — JWT outage must not block login", rec.Code, http.StatusOK)
	}
	if cookie := findCookie(rec.Result().Cookies(), "pericarp_token"); cookie != nil && cookie.Value != "" {
		t.Errorf("JWT cookie unexpectedly set: %q", cookie.Value)
	}

	var env struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if env.Data.Token != "" {
		t.Errorf("response token = %q, want empty when issuance fails", env.Data.Token)
	}
}

func TestPasswordAuthHandler_Logout_ClearsJWTCookie(t *testing.T) {
	h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    &fakeAuthService{},
		SessionManager: &fakeSessionManager{},
		Logger:         nopLogger{},
		SecureCookies:  true,
	})

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil), rec)
	oauthCalls := 0
	oauthLogout := func(http.ResponseWriter, *http.Request) { oauthCalls++ }

	if err := h.Logout(c, oauthLogout); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if oauthCalls != 1 {
		t.Errorf("oauth logout calls = %d, want 1", oauthCalls)
	}

	cookie := findCookie(rec.Result().Cookies(), "pericarp_token")
	if cookie == nil {
		t.Fatal("expected expired JWT cookie in response")
	}
	if cookie.MaxAge >= 0 {
		t.Errorf("cookie MaxAge = %d, want negative (cookie cleared)", cookie.MaxAge)
	}
	if !cookie.Secure {
		t.Error("cookie Secure = false, want true (SecureCookies configured)")
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}
