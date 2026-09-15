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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

// usersAccounts answers membership questions from maps and records every role
// it is asked to save.
type usersAccounts struct {
	authrepos.AccountRepository
	memberships map[string][]*authentities.Account // agent id -> accounts, first first
	roles       map[string]string                  // "agent|account" -> role
	saved       []string
}

func (a *usersAccounts) FindByMember(_ context.Context, agentID string) ([]*authentities.Account, error) {
	return a.memberships[agentID], nil
}

func (a *usersAccounts) FindMemberRole(_ context.Context, accountID, agentID string) (string, error) {
	return a.roles[agentID+"|"+accountID], nil
}

func (a *usersAccounts) SaveMember(_ context.Context, accountID, agentID, roleID string) error {
	a.saved = append(a.saved, agentID+"|"+accountID+"|"+roleID)
	return nil
}

type usersAgents struct {
	authrepos.AgentRepository
	agents map[string]*authentities.Agent
}

func (a usersAgents) FindByID(_ context.Context, id string) (*authentities.Agent, error) {
	return a.agents[id], nil
}

func (a usersAgents) Save(context.Context, *authentities.Agent) error { return nil }

type usersCredentials struct{ authrepos.CredentialRepository }

func (usersCredentials) FindByAgent(context.Context, string) ([]*authentities.Credential, error) {
	return nil, nil
}

type usersDirectory struct{ members []repositories.AccountMember }

func (d usersDirectory) ListMembers(context.Context, string, string, int) (*repositories.AccountMemberPage, error) {
	return &repositories.AccountMemberPage{Members: d.members}, nil
}

func usersPerson(t *testing.T, id, name string) *authentities.Agent {
	t.Helper()
	agent, err := (&authentities.Agent{}).With(id, name, authentities.AgentTypePerson)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// wm-8uq74. An identity that names no active account is refused on every users
// route with unscoped_session, even when the person belongs to an account they
// own. The routes must not pick one of the person's accounts for them: with
// several accounts, the one picked may not be the one the person means.
func TestUserRoutesRefuseAnIdentityWithNoActiveAccount(t *testing.T) {
	accounts := &usersAccounts{
		memberships: map[string][]*authentities.Account{"ops": {harborAccount(t)}},
		roles:       map[string]string{"ops|acct-harbor": authentities.RoleOwner, "clerk|acct-harbor": authentities.RoleMember},
	}
	h := handlers.NewUserHandler(handlers.UserHandlerConfig{
		AgentRepo: usersAgents{agents: map[string]*authentities.Agent{
			"ops":   usersPerson(t, "ops", "Harbor Operations"),
			"clerk": usersPerson(t, "clerk", "Lantern Clerk"),
		}},
		CredentialRepo: usersCredentials{},
		AccountRepo:    accounts,
		Members: usersDirectory{members: []repositories.AccountMember{
			{AgentID: "clerk", RoleID: authentities.RoleMember, HasRecord: true, Name: "Lantern Clerk", Status: "active"},
			{AgentID: "ops", RoleID: authentities.RoleOwner, HasRecord: true, Name: "Harbor Operations", Status: "active"},
		}},
		Logger: nopLogger{},
	})

	e := echo.New()
	noActiveAccount := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{
				AgentID: "ops", AccountIDs: []string{"acct-harbor"}, ActiveAccountID: "",
			})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
	e.GET("/api/users", h.List, noActiveAccount)
	e.GET("/api/users/:id", h.Get, noActiveAccount)
	e.PUT("/api/users/:id", h.Update, noActiveAccount)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/users", ""},
		{http.MethodGet, "/api/users/clerk", ""},
		{http.MethodPut, "/api/users/clerk", `{"role":"admin"}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
		if rec.Code != http.StatusUnauthorized || refusal.Code != "unscoped_session" {
			t.Errorf("%s %s with no active account answered %d %s; want 401 unscoped_session",
				tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	if len(accounts.saved) != 0 {
		t.Errorf("a request with no active account saved roles %v; want none", accounts.saved)
	}
}

// wm-ii1hz. A users handler built without its member directory would answer
// every list with a nil-pointer panic. It must refuse to be built at all, and
// say which dependency is missing.
func TestNewUserHandlerRefusesToBeBuiltWithoutAMemberDirectory(t *testing.T) {
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handlers.NewUserHandler(handlers.UserHandlerConfig{Logger: nopLogger{}})
	}()
	if recovered == nil {
		t.Fatal("NewUserHandler built a handler with no Members; want it to stop at construction")
	}
	if said := fmt.Sprint(recovered); !strings.Contains(said, "Members") {
		t.Errorf("the construction failure says %q; want it to name the missing Members dependency", said)
	}
}
