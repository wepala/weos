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

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

// oneAgent answers FindByID with the agent it holds.
type oneAgent struct {
	authrepos.AgentRepository
	agent *authentities.Agent
}

func (o oneAgent) FindByID(context.Context, string) (*authentities.Agent, error) { return o.agent, nil }

// membership answers FindMemberRole with role, or with err when it is set.
type membership struct {
	authrepos.AccountRepository
	role string
	err  error
}

func (m membership) FindMemberRole(context.Context, string, string) (string, error) {
	return m.role, m.err
}
func (m membership) FindByMember(context.Context, string) ([]*authentities.Account, error) {
	return nil, nil
}

// issuing records whether a token was issued and with which extras.
type issuing struct {
	authapp.JWTService
	issued bool
	extras map[string]any
}

func (i *issuing) IssueToken(_ context.Context, _ *authentities.Agent, _ []*authentities.Account, _ string,
	_ *auth.SubscriptionClaim, extras map[string]any) (string, error) {
	i.issued = true
	i.extras = extras
	return "access-token", nil
}

func refreshWith(t *testing.T, accounts membership) (*httptest.ResponseRecorder, *issuing, RefreshTokenRepository, string) {
	t.Helper()
	db := setupTestDB(t)
	refreshRepo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	stored := &OAuthRefreshToken{AgentID: "agent-ops", AccountID: "acct-harbor", ClientID: "client-notes"}
	raw := "raw-refresh-token"
	mustNoErr(t, refreshRepo.Create(ctx, stored, raw), "create refresh token")

	agent, err := (&authentities.Agent{}).With("agent-ops", "Dana Whitfield", authentities.AgentTypePerson)
	mustNoErr(t, err, "make the agent")
	jwt := &issuing{}
	handler := Token(jwt, NewAuthCodeRepository(db), refreshRepo, oneAgent{agent: agent}, accounts, noopLogger{})

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(newTokenRequest(tokenForm(map[string]string{
		"grant_type": "refresh_token", "refresh_token": raw, "client_id": "client-notes",
	})), rec)
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return rec, jwt, refreshRepo, raw
}

// wm-mo1bp: a member refreshes, and the new access token carries the connector
// mark (wm-8i8ln); a person no longer in the account is refused with
// invalid_grant and the refresh token is revoked; a membership that cannot be
// read issues nothing.
func TestTokenHandler_RefreshToken_ChecksTheMembership(t *testing.T) {
	t.Run("a member", func(t *testing.T) {
		rec, jwt, _, _ := refreshWith(t, membership{role: authentities.RoleOwner})
		if rec.Code != http.StatusOK || !jwt.issued {
			t.Fatalf("got %d %s (issued %v), want 200 and a token", rec.Code, rec.Body.String(), jwt.issued)
		}
		if jwt.extras[TokenUseClaim] != TokenUseConnector {
			t.Fatalf("the refreshed token's extras = %v, want %s=%s", jwt.extras, TokenUseClaim, TokenUseConnector)
		}
	})

	t.Run("no longer a member", func(t *testing.T) {
		rec, jwt, refreshRepo, raw := refreshWith(t, membership{})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"invalid_grant"`) || jwt.issued {
			t.Fatalf("got %d %s (issued %v), want 400 invalid_grant and no token", rec.Code, rec.Body.String(), jwt.issued)
		}
		stored, err := refreshRepo.FindByTokenHash(context.Background(), HashToken(raw))
		mustNoErr(t, err, "read the refresh token back")
		if !stored.Revoked {
			t.Fatal("the refresh token was not revoked")
		}
	})

	t.Run("the membership cannot be read", func(t *testing.T) {
		rec, jwt, refreshRepo, raw := refreshWith(t, membership{err: errors.New("database away")})
		if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"server_error"`) || jwt.issued {
			t.Fatalf("got %d %s (issued %v), want 500 server_error and no token", rec.Code, rec.Body.String(), jwt.issued)
		}
		stored, err := refreshRepo.FindByTokenHash(context.Background(), HashToken(raw))
		mustNoErr(t, err, "read the refresh token back")
		if stored.Revoked {
			t.Fatal("an unreadable membership revoked the refresh token; it says nothing about the person")
		}
	})
}
