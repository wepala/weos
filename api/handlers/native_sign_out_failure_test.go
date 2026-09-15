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
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	"github.com/labstack/echo/v4"
)

// failingNativeSignOutStore is a refresh token store that cannot be read, or
// cannot be written, as when its database is down. The one token it can read is
// a live native refresh token.
type failingNativeSignOutStore struct {
	weosoauth.RefreshTokenRepository
	findErr   error
	revokeErr error
}

func (s *failingNativeSignOutStore) FindByTokenHash(context.Context, string) (*weosoauth.OAuthRefreshToken, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	return &weosoauth.OAuthRefreshToken{
		ID: "rt-phone", FamilyID: "rt-phone", AgentID: "agent-ops", AccountID: "acct-harbor",
		ClientID: weosoauth.NativeClientID, ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (s *failingNativeSignOutStore) RevokeFamily(context.Context, string) error { return s.revokeErr }

func (s *failingNativeSignOutStore) RevokeForAgent(context.Context, string, string) error {
	return s.revokeErr
}

// nativeSignOutLog records every line a handler logs, with its fields.
type nativeSignOutLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *nativeSignOutLog) record(level, msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, level+" "+msg+" "+fmt.Sprint(fields...))
}

func (l *nativeSignOutLog) Debug(_ context.Context, msg string, fields ...any) {
	l.record("DEBUG", msg, fields)
}
func (l *nativeSignOutLog) Info(_ context.Context, msg string, fields ...any) {
	l.record("INFO", msg, fields)
}
func (l *nativeSignOutLog) Warn(_ context.Context, msg string, fields ...any) {
	l.record("WARN", msg, fields)
}
func (l *nativeSignOutLog) Error(_ context.Context, msg string, fields ...any) {
	l.record("ERROR", msg, fields)
}

func (l *nativeSignOutLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// A sign-out whose app session cannot be ended — the refresh token store is
// down — still signs the browser out: the cookie is cleared, the browser
// session ends and the answer is 200. The answer says the app's session was not
// ended, so the app signs out again, and the failure is logged without the
// token (wm-5rziu).
func TestPasswordAuthHandler_Logout_EndsTheBrowserSessionWhenTheAppSessionCannotBeEnded(t *testing.T) {
	const refreshToken = "phone-refresh-token-value"
	stores := map[string]*failingNativeSignOutStore{
		"the store cannot be read":    {findErr: errors.New("database is locked")},
		"the store cannot be written": {revokeErr: errors.New("database is locked")},
	}
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			log := &nativeSignOutLog{}
			h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
				AuthService:    &fakeAuthService{},
				SessionManager: &fakeSessionManager{},
				Logger:         log,
				RefreshTokens:  store,
			})
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(
				newJSONRequest(http.MethodPost, "/api/auth/logout", `{"refresh_token":"`+refreshToken+`"}`), rec)
			browserSessionsEnded := 0
			oauthLogout := func(w http.ResponseWriter, _ *http.Request) {
				browserSessionsEnded++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				// A failed write shows up as a missing body, which the test reports.
				_, _ = w.Write([]byte(`{"status":"logged out"}` + "\n"))
			}

			if err := h.Logout(c, oauthLogout); err != nil {
				t.Fatalf("Logout: %v", err)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("the sign-out answered %d, want 200", rec.Code)
			}
			if browserSessionsEnded != 1 {
				t.Fatalf("the browser session was ended %d times, want 1", browserSessionsEnded)
			}
			if cookie := findCookie(rec.Result().Cookies(), "pericarp_token"); cookie == nil || cookie.MaxAge >= 0 {
				t.Fatal("the sign-out did not clear the JWT cookie")
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode the sign-out's answer: %v", err)
			}
			if body["status"] != "logged out" || body["app_session"] != "not_ended" || body["code"] != "app_session_not_ended" {
				t.Fatalf("the sign-out answered status %v app_session %v code %v, want logged out, not_ended, app_session_not_ended",
					body["status"], body["app_session"], body["code"])
			}
			lines := log.all()
			loggedError := false
			for _, line := range lines {
				if strings.HasPrefix(line, "ERROR ") {
					loggedError = true
				}
				if strings.Contains(line, refreshToken) {
					t.Fatal("a log line carries the refresh token")
				}
			}
			if !loggedError {
				t.Fatal("the failure to end the app's session was not logged as an error")
			}
		})
	}
}
