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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
)

// verifiedEmailAuthService stubs the AuthenticationService methods pericarp's
// callback reaches before it writes a credential. FindOrCreateAgent records
// that it ran and stops the handler, so a zero count means no credential was
// written. Every other method is inherited from the nil embedded interface.
type verifiedEmailAuthService struct {
	authapp.AuthenticationService
	userInfo          authapp.UserInfo
	findOrCreateCalls int
}

func (s *verifiedEmailAuthService) ValidateState(context.Context, string, string) error {
	return nil
}

func (s *verifiedEmailAuthService) ExchangeCode(context.Context, string, string, string, string) (*authapp.AuthResult, error) {
	return &authapp.AuthResult{UserInfo: s.userInfo}, nil
}

func (s *verifiedEmailAuthService) FindOrCreateAgent(context.Context, authapp.UserInfo) (*authentities.Agent, *authentities.Credential, *authentities.Account, error) {
	s.findOrCreateCalls++
	return nil, nil, nil, errors.New("stop after the verification check")
}

// flowSessionManager hands the callback a pending flow for one provider.
type flowSessionManager struct {
	session.SessionManager
	provider string
}

func (m flowSessionManager) GetFlowData(http.ResponseWriter, *http.Request) (*session.FlowData, error) {
	return &session.FlowData{State: "state-42", CodeVerifier: "verifier", Provider: m.provider}, nil
}

// runPericarpCallback drives pericarp's own callback through the wrapper
// serve mounts at /api/auth/callback, the way a provider's browser redirect
// reaches it: a GET for Google, Apple's form_post for Apple.
func runPericarpCallback(t *testing.T, userInfo authapp.UserInfo) (*httptest.ResponseRecorder, *verifiedEmailAuthService) {
	t.Helper()
	auth := &verifiedEmailAuthService{userInfo: userInfo}
	handlers := authhttp.NewAuthHandlers(authhttp.HandlerConfig{
		AuthService:    auth,
		SessionManager: flowSessionManager{provider: userInfo.Provider},
	})

	var req *http.Request
	if userInfo.Provider == "apple" {
		form := url.Values{"code": {"provider-code"}, "state": {"state-42"}}
		req = httptest.NewRequest(http.MethodPost, "/api/auth/callback", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=provider-code&state=state-42", nil)
	}
	return runCallback(t, req, handlers.Callback), auth
}

func TestAuthCallback_RefusesAnUnverifiedProviderEmail(t *testing.T) {
	for _, provider := range []string{"google", "apple"} {
		t.Run(provider, func(t *testing.T) {
			rec, auth := runPericarpCallback(t, authapp.UserInfo{
				Provider:       provider,
				ProviderUserID: provider + "-sub-7731",
				Email:          "dana.reyes@example.com",
				EmailVerified:  false,
			})

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
			}
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
			}
			if body.Code != "email_not_verified" {
				t.Errorf("code = %q, want email_not_verified", body.Code)
			}
			if auth.findOrCreateCalls != 0 {
				t.Errorf("FindOrCreateAgent ran %d times; an unverified email must never get a credential", auth.findOrCreateCalls)
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("a refused sign-in must not redirect; Location = %q", loc)
			}
		})
	}
}

func TestAuthCallback_AdmitsVerifiedAndUnclaimedEmails(t *testing.T) {
	cases := map[string]authapp.UserInfo{
		"verified google": {Provider: "google", ProviderUserID: "google-sub-7731", Email: "dana.reyes@example.com", EmailVerified: true},
		"verified apple":  {Provider: "apple", ProviderUserID: "apple-sub-7731", Email: "dana.reyes@example.com", EmailVerified: true},
		"netsuite":        {Provider: "netsuite", ProviderUserID: "netsuite-4410", Email: "dana.reyes@example.com"},
	}
	for name, userInfo := range cases {
		t.Run(name, func(t *testing.T) {
			_, auth := runPericarpCallback(t, userInfo)

			if auth.findOrCreateCalls != 1 {
				t.Errorf("the sign-in must reach FindOrCreateAgent; calls=%d", auth.findOrCreateCalls)
			}
		})
	}
}
