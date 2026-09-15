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
	"time"

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

func (d usersDirectory) CountMembersWithRole(_ context.Context, _, roleID string) (int, error) {
	n := 0
	for _, m := range d.members {
		if m.RoleID == roleID {
			n++
		}
	}
	return n, nil
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

// usersWarnings records every warning as one line: the message, then each key
// and value, separated by spaces.
type usersWarnings struct {
	nopLogger
	lines []string
}

func (l *usersWarnings) Warn(_ context.Context, msg string, keyvals ...any) {
	l.lines = append(l.lines, strings.TrimSpace(fmt.Sprintln(append([]any{msg}, keyvals...)...)))
}

// asIdentity puts agentID, acting in accountID, on every request.
func asIdentity(agentID, accountID string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{
				AgentID: agentID, AccountIDs: []string{accountID}, ActiveAccountID: accountID,
			})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// wm-govvg. A plain member of an account is refused every users route by the
// role check, and each refusal is recorded at warn with the caller and the
// account, and with the person asked about on GET and PUT, the way every other
// users-route refusal is. The line carries ids only, never an email address.
func TestUserRoutesRecordTheRefusalOfAPlainMember(t *testing.T) {
	accounts := &usersAccounts{
		roles: map[string]string{"ops|acct-harbor": authentities.RoleOwner, "clerk|acct-harbor": authentities.RoleMember},
	}
	logs := &usersWarnings{}
	h := handlers.NewUserHandler(handlers.UserHandlerConfig{
		AgentRepo: usersAgents{agents: map[string]*authentities.Agent{
			"ops":   usersPerson(t, "ops", "Harbor Operations"),
			"clerk": usersPerson(t, "clerk", "Lantern Clerk"),
		}},
		CredentialRepo: usersCredentials{},
		AccountRepo:    accounts,
		Members:        usersDirectory{},
		Logger:         logs,
	})

	e := echo.New()
	clerk := asIdentity("clerk", "acct-harbor")
	e.GET("/api/users", h.List, clerk)
	e.GET("/api/users/:id", h.Get, clerk)
	e.PUT("/api/users/:id", h.Update, clerk)

	for _, tc := range []struct{ method, path, body, target string }{
		{http.MethodGet, "/api/users", "", ""},
		{http.MethodGet, "/api/users/ops", "", "ops"},
		{http.MethodPut, "/api/users/ops", `{"role":"member"}`, "ops"},
	} {
		logs.lines = nil
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("a plain member's %s %s answered %d %s, want 403", tc.method, tc.path, rec.Code, rec.Body.String())
			continue
		}
		want := []string{"caller_agent_id clerk", "account_id acct-harbor"}
		if tc.target != "" {
			want = append(want, "target_agent_id "+tc.target)
		}
		var recorded []string
		for _, line := range logs.lines {
			if strings.HasPrefix(line, "users request refused") {
				recorded = append(recorded, line)
			}
		}
		if len(recorded) != 1 {
			t.Errorf("a plain member's %s %s recorded %d refusal warnings %q, want 1", tc.method, tc.path, len(recorded), logs.lines)
			continue
		}
		for _, w := range want {
			if !strings.Contains(recorded[0], w) {
				t.Errorf("the refusal of %s %s was recorded as %q, which does not carry %q", tc.method, tc.path, recorded[0], w)
			}
		}
		if tc.target == "" && strings.Contains(recorded[0], "target_agent_id") {
			t.Errorf("the refusal of the list names a target: %q", recorded[0])
		}
		if strings.Contains(recorded[0], "@") {
			t.Errorf("the refusal line carries an email address: %q", recorded[0])
		}
	}
	if len(accounts.saved) != 0 {
		t.Errorf("a plain member's requests saved roles %v; want none", accounts.saved)
	}
}

// usersCredentialsOf hands back the same credentials, in the order given, for
// any person.
type usersCredentialsOf struct {
	authrepos.CredentialRepository
	creds []*authentities.Credential
}

func (r usersCredentialsOf) FindByAgent(context.Context, string) ([]*authentities.Credential, error) {
	return r.creds, nil
}

func usersCredential(t *testing.T, id, email string, createdAt time.Time) *authentities.Credential {
	t.Helper()
	cred := &authentities.Credential{}
	if err := cred.Restore(id, "clerk", "password", id, email, "", true, createdAt, createdAt); err != nil {
		t.Fatal(err)
	}
	return cred
}

// wm-govvg. The list names a member by the earliest of their credentials that
// has an email. GET and PUT name them the same way, so all three routes agree
// when the first credential has no email, and whatever order the credentials
// are read back in.
func TestUserRoutesNameAMemberByTheirEarliestCredentialWithAnEmail(t *testing.T) {
	base := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	h := handlers.NewUserHandler(handlers.UserHandlerConfig{
		AgentRepo: usersAgents{agents: map[string]*authentities.Agent{
			"ops":   usersPerson(t, "ops", "Harbor Operations"),
			"clerk": usersPerson(t, "clerk", "Lantern Clerk"),
		}},
		CredentialRepo: usersCredentialsOf{creds: []*authentities.Credential{
			usersCredential(t, "cred-later", "clerk.desk@lanternhomes.example", base.Add(2*time.Hour)),
			usersCredential(t, "cred-first", "", base),
			usersCredential(t, "cred-earliest-email", "clerk@lanternhomes.example", base.Add(time.Hour)),
		}},
		AccountRepo: &usersAccounts{
			roles: map[string]string{"ops|acct-harbor": authentities.RoleOwner, "clerk|acct-harbor": authentities.RoleMember},
		},
		Members: usersDirectory{},
		Logger:  nopLogger{},
	})

	e := echo.New()
	ops := asIdentity("ops", "acct-harbor")
	e.GET("/api/users/:id", h.Get, ops)
	e.PUT("/api/users/:id", h.Update, ops)

	for _, tc := range []struct{ method, body string }{
		{http.MethodGet, ""},
		{http.MethodPut, `{"name":"Lantern Front Desk"}`},
	} {
		req := httptest.NewRequest(tc.method, "/api/users/clerk", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		var envelope struct {
			Data handlers.UserResponse `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s /api/users/clerk answered %d %s, want 200", tc.method, rec.Code, rec.Body.String())
		}
		if envelope.Data.Email != "clerk@lanternhomes.example" {
			t.Errorf("%s /api/users/clerk named the member %q, want clerk@lanternhomes.example, "+
				"the earliest credential that has an email", tc.method, envelope.Data.Email)
		}
	}
}
