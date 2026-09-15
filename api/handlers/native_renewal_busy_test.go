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
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/domain/repositories"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

// busyNativeRenewalStore is a refresh token store whose rotation fails the way a
// locked database does. The presented token reads live the first time, and as
// afterFailure when the renewal reads it again; any later read finds nothing.
type busyNativeRenewalStore struct {
	weosoauth.RefreshTokenRepository
	mu            sync.Mutex
	reads         int
	live          weosoauth.OAuthRefreshToken
	afterFailure  weosoauth.OAuthRefreshToken
	revocations   int
	familyRevoked bool
}

func (s *busyNativeRenewalStore) FindByTokenHash(context.Context, string) (*weosoauth.OAuthRefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	switch s.reads {
	case 1:
		row := s.live
		return &row, nil
	case 2:
		row := s.afterFailure
		return &row, nil
	default:
		return nil, weosoauth.ErrNotFound
	}
}

func (s *busyNativeRenewalStore) Rotate(context.Context, string, *weosoauth.OAuthRefreshToken, string) error {
	return errors.New("database is locked")
}

func (s *busyNativeRenewalStore) RevokeFamily(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.familyRevoked = true
	return nil
}

func (s *busyNativeRenewalStore) Revoke(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revocations++
	return nil
}

type busyRenewalAccounts struct {
	authrepos.AccountRepository
	account *authentities.Account
}

func (a busyRenewalAccounts) FindByID(context.Context, string) (*authentities.Account, error) {
	return a.account, nil
}

func (a busyRenewalAccounts) FindMemberRole(context.Context, string, string) (string, error) {
	return authentities.RoleOwner, nil
}

type busyRenewalAgents struct {
	authrepos.AgentRepository
	agent *authentities.Agent
}

func (a busyRenewalAgents) FindByID(context.Context, string) (*authentities.Agent, error) {
	return a.agent, nil
}

type busyRenewalLocks struct {
	repositories.AccountErasureLocks
}

func (busyRenewalLocks) IsLocked(context.Context, string) (bool, error) { return false, nil }

// wm-tu180. A renewal whose rotation fails — a busy or locked database — never
// answers 500, which an app would retry with a refresh token it cannot tell was
// spent. When another renewal spent the token meanwhile, this one is refused
// with invalid_refresh_token and the session's family is left alone. When
// nobody spent it, nothing was spent, and the answer is 503 with Retry-After so
// the app sends the same renewal again.
func TestPasswordAuthHandler_Refresh_ARotationThatFailsNeverAnswers500(t *testing.T) {
	const refreshToken = "phone-refresh-token-value"
	now := time.Now()
	live := weosoauth.OAuthRefreshToken{
		ID: "rt-phone", FamilyID: "rt-phone", AgentID: "agent-ops", AccountID: "acct-harbor",
		ClientID: weosoauth.NativeClientID, ExpiresAt: now.Add(time.Hour),
	}
	spentByAnother := live
	spentByAnother.Revoked = true
	spentByAnother.SuccessorID = "rt-phone-next"
	spentByAnother.RotatedAt = &now

	cases := []struct {
		name         string
		afterFailure weosoauth.OAuthRefreshToken
		wantStatus   int
		wantCode     string
	}{
		{"another renewal spent the token meanwhile", spentByAnother, http.StatusUnauthorized, handlers.CodeInvalidRefreshToken},
		{"nobody spent the token", live, http.StatusServiceUnavailable, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &busyNativeRenewalStore{live: live, afterFailure: tc.afterFailure}
			h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
				AuthService:         &fakeAuthService{tokenString: "renewed-access-token"},
				SessionManager:      &fakeSessionManager{},
				Logger:              nopLogger{},
				AccountRepo:         busyRenewalAccounts{account: newAccount(t, "acct-harbor", "Harbor Legal")},
				AgentRepo:           busyRenewalAgents{agent: newAgent(t, "agent-ops", "Dana Whitfield")},
				ErasureLocks:        busyRenewalLocks{},
				RefreshTokens:       store,
				RefreshSuccessorKey: []byte("the successor key a busy renewal test holds"),
			})
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(
				newJSONRequest(http.MethodPost, "/api/auth/refresh", `{"refresh_token":"`+refreshToken+`"}`), rec)

			if err := h.Refresh(c); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			var answer struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
				t.Fatalf("decode the renewal's answer: %v", err)
			}
			if rec.Code != tc.wantStatus || answer.Code != tc.wantCode {
				t.Fatalf("the renewal answered %d code %q, want %d code %q", rec.Code, answer.Code, tc.wantStatus, tc.wantCode)
			}
			if tc.wantStatus == http.StatusServiceUnavailable && rec.Header().Get("Retry-After") == "" {
				t.Fatal("a 503 renewal carries no Retry-After")
			}
			if strings.Contains(rec.Body.String(), "renewed-access-token") {
				t.Fatal("a renewal that did not renew carries an access token")
			}
			if store.familyRevoked || store.revocations != 0 {
				t.Fatalf("the renewal revoked the family %v and %d tokens; a failed rotation must revoke nothing",
					store.familyRevoked, store.revocations)
			}
		})
	}
}
