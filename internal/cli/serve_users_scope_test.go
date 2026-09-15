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
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"go.uber.org/fx"
)

// wm-govvg. The users routes — GET /api/users, GET /api/users/:id and
// PUT /api/users/:id — are judged through buildServer, because the property
// under test is what serve's own wiring allows: the session in front of the
// routes and the account a signed-in person acts in. On an instance with open
// registration every person owns the account their registration created, so
// an owner role says nothing about anybody outside that one account.

// usersUnknownID is shaped like a person's id and names nobody.
const usersUnknownID = "2Zq7mQb0Xn9YpR4sT1vW8kLcE3dA"

// usersRefusal is the message every refused users request is logged with.
const usersRefusal = "not a member of the caller's account"

type usersPerson struct {
	email        string
	agentID      string
	ownAccountID string         // the account the person's registration created
	accountID    string         // the account the person's session acts in
	cookies      []*http.Cookie // a session acting in accountID
}

// usersScope is an instance with three people on it. owner owns the
// instance's first account; member is a plain member of that account, signed
// in to act in it; outsider owns an account of their own and belongs to no
// other.
type usersScope struct {
	srv         *httptest.Server
	accounts    authrepos.AccountRepository
	agents      authrepos.AgentRepository
	credentials authrepos.CredentialRepository
	authService authapp.AuthenticationService
	sessions    session.SessionManager
	logs        *bootLogCapture
	owner       usersPerson
	member      usersPerson
	outsider    usersPerson
}

func newUsersScope(t *testing.T, extra ...fx.Option) *usersScope {
	t.Helper()
	s := &usersScope{logs: &bootLogCapture{}}
	var credentials authrepos.CredentialRepository
	var authService authapp.AuthenticationService
	var sessionManager session.SessionManager

	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.PasswordAuthEnabled = true
	cfg.PasswordRegistrationEnabled = true
	options := append([]fx.Option{
		fx.Populate(&s.accounts, &s.agents, &credentials, &authService, &sessionManager),
		fx.Decorate(func(entities.Logger) entities.Logger { return s.logs }),
	}, extra...)
	s.srv = bootServe(t, cfg, options...)
	s.authService, s.sessions, s.credentials = authService, sessionManager, credentials

	people := []usersPerson{
		usersRegister(t, s.srv, "ops@harborlegal.example"),
		usersRegister(t, s.srv, "counsel@cedarrealty.example"),
		usersRegister(t, s.srv, "clerk@lanternhomes.example"),
	}
	ctx := context.Background()
	first, err := s.accounts.FindAll(ctx, "", 1)
	if err != nil || len(first.Data) == 0 {
		t.Fatalf("find the instance's first account: %v", err)
	}
	ownerAt := -1
	for i, p := range people {
		if p.ownAccountID == first.Data[0].GetID() {
			ownerAt = i
		}
	}
	if ownerAt < 0 {
		t.Fatalf("none of the three registrations created the instance's first account")
	}
	var rest []usersPerson
	for i, p := range people {
		if i != ownerAt {
			rest = append(rest, p)
		}
	}
	s.owner, s.outsider, s.member = people[ownerAt], rest[0], rest[1]
	if err := s.accounts.SaveMember(ctx, s.owner.accountID, s.member.agentID, authentities.RoleMember); err != nil {
		t.Fatalf("add the member to the owner's account: %v", err)
	}
	s.member.cookies = usersSessionIn(t, authService, sessionManager, credentials, s.member.agentID, s.owner.accountID)
	s.member.accountID = s.owner.accountID
	return s
}

func usersRegister(t *testing.T, srv *httptest.Server, email string) usersPerson {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":"correct-horse-battery-staple"}`, email)
	answer := serveCall(t, srv, http.MethodPost, "/api/auth/register", body, nil)
	if answer.status != http.StatusOK {
		t.Fatalf("registering %s answered %d", email, answer.status)
	}
	var envelope struct {
		Data struct {
			Agent struct {
				ID string `json:"id"`
			} `json:"agent"`
			Account *struct {
				ID string `json:"id"`
			} `json:"account"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &envelope); err != nil {
		t.Fatalf("decode the registration of %s: %v", email, err)
	}
	if envelope.Data.Agent.ID == "" || envelope.Data.Account == nil || envelope.Data.Account.ID == "" {
		t.Fatalf("the registration of %s named no person or no account", email)
	}
	if len(answer.cookies) == 0 {
		t.Fatalf("the registration of %s set no session cookie", email)
	}
	return usersPerson{
		email:        email,
		agentID:      envelope.Data.Agent.ID,
		ownAccountID: envelope.Data.Account.ID,
		accountID:    envelope.Data.Account.ID,
		cookies:      answer.cookies,
	}
}

// usersSessionIn opens a session for agentID acting in accountID, as a sign-in
// to that account would, and returns its cookies.
func usersSessionIn(t *testing.T, authService authapp.AuthenticationService, sessionManager session.SessionManager,
	credentials authrepos.CredentialRepository, agentID, accountID string) []*http.Cookie {
	t.Helper()
	ctx := context.Background()
	creds, err := credentials.FindByAgent(ctx, agentID)
	if err != nil || len(creds) == 0 {
		t.Fatalf("find the person's credential: %v", err)
	}
	opened, err := authService.CreateSession(ctx, agentID, accountID, creds[0].GetID(), "127.0.0.1", "serve-test", time.Hour)
	if err != nil {
		t.Fatalf("open a session in account %s: %v", accountID, err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	rec := httptest.NewRecorder()
	if err := sessionManager.CreateHTTPSession(rec, req, session.SessionData{
		SessionID: opened.GetID(),
		AgentID:   agentID,
		AccountID: accountID,
		CreatedAt: time.Now(),
		ExpiresAt: opened.ExpiresAt(),
	}); err != nil {
		t.Fatalf("write the session cookie: %v", err)
	}
	return rec.Result().Cookies()
}

func (s *usersScope) call(t *testing.T, method, path, body string, as usersPerson) serveAnswer {
	t.Helper()
	return serveCall(t, s.srv, method, path, body, as.cookies)
}

type usersRow struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Status string `json:"status"`
	Role   string `json:"role"`
}

func usersListed(t *testing.T, answer serveAnswer) []usersRow {
	t.Helper()
	var envelope struct {
		Data []usersRow `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &envelope); err != nil {
		t.Fatalf("decode the users list: %v", err)
	}
	return envelope.Data
}

func userAnswered(t *testing.T, answer serveAnswer) usersRow {
	t.Helper()
	var envelope struct {
		Data usersRow `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &envelope); err != nil {
		t.Fatalf("decode the user: %v", err)
	}
	return envelope.Data
}

// roleIn is the role agentID holds in accountID, or "" for no membership.
func (s *usersScope) roleIn(t *testing.T, accountID, agentID string) string {
	t.Helper()
	role, err := s.accounts.FindMemberRole(context.Background(), accountID, agentID)
	if err != nil {
		t.Fatalf("read the role of %s in %s: %v", agentID, accountID, err)
	}
	return role
}

func (s *usersScope) nameOf(t *testing.T, agentID string) string {
	t.Helper()
	agent, err := s.agents.FindByID(context.Background(), agentID)
	if err != nil || agent == nil {
		t.Fatalf("read the person %s: %v", agentID, err)
	}
	return agent.Name()
}

// describe names each listed row by who it is in the scope, for a failure
// message that says which people leaked.
func (s *usersScope) describe(rows []usersRow) string {
	who := map[string]string{s.owner.agentID: "owner", s.member.agentID: "member", s.outsider.agentID: "outsider"}
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		name := who[r.ID]
		if name == "" {
			name = r.ID
		}
		parts = append(parts, fmt.Sprintf("%s(role %q)", name, r.Role))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// wantRefusalRecorded checks that a refusal of caller's request about target
// was logged at warn, naming the caller, the account and the person, and no
// email address.
func (s *usersScope) wantRefusalRecorded(t *testing.T, caller usersPerson, target usersPerson) {
	t.Helper()
	var found []string
	for _, line := range s.logs.mentioning(usersRefusal) {
		if strings.Contains(line, "target_agent_id "+target.agentID) {
			found = append(found, line)
		}
	}
	if len(found) == 0 {
		t.Errorf("no warning recorded the refusal of %s's request about %s", caller.agentID, target.agentID)
		return
	}
	for _, line := range found {
		if !strings.HasPrefix(line, "warn: ") {
			t.Errorf("the refusal was not logged at warn: %s", line)
		}
		for _, want := range []string{"caller_agent_id " + caller.agentID, "account_id " + caller.accountID} {
			if !strings.Contains(line, want) {
				t.Errorf("the refusal line %q does not carry %q", line, want)
			}
		}
		if strings.Contains(line, "@") {
			t.Errorf("the refusal line carries an email address: %s", line)
		}
	}
}

// The owner of one account lists only the members of that account.
func TestServe_UsersListNamesOnlyMembersOfTheCallersAccount(t *testing.T) {
	s := newUsersScope(t)

	answer := s.call(t, http.MethodGet, "/api/users", "", s.outsider)
	if answer.status != http.StatusOK {
		t.Fatalf("the list answered %d %s, want 200", answer.status, answer.body)
	}
	rows := usersListed(t, answer)
	if len(rows) != 1 || rows[0].ID != s.outsider.agentID || rows[0].Role != authentities.RoleOwner {
		t.Fatalf("the owner of their own account listed %s; want only themselves, as owner", s.describe(rows))
	}
}

// The owner of one account cannot fetch a person outside it, and a person who
// does not exist gets exactly the same answer.
func TestServe_UsersGetRefusesAPersonOutsideTheCallersAccount(t *testing.T) {
	s := newUsersScope(t)

	outside := s.call(t, http.MethodGet, "/api/users/"+s.owner.agentID, "", s.outsider)
	if outside.status != http.StatusNotFound {
		t.Errorf("fetching a person outside the caller's account answered %d %s, want 404", outside.status, outside.body)
	}
	if strings.Contains(outside.body, s.owner.email) || strings.Contains(outside.body, s.owner.agentID) {
		t.Errorf("the answer about a person outside the caller's account describes them: %s", outside.body)
	}
	unknown := s.call(t, http.MethodGet, "/api/users/"+usersUnknownID, "", s.outsider)
	if unknown.status != outside.status || unknown.body != outside.body {
		t.Errorf("an unknown person answered %d %s; a person outside the account answered %d %s",
			unknown.status, unknown.body, outside.status, outside.body)
	}
	s.wantRefusalRecorded(t, s.outsider, s.owner)
}

// The owner of one account cannot change the name or the role of a person
// outside it, and nothing is saved in any account.
func TestServe_UsersUpdateRefusesAPersonOutsideTheCallersAccount(t *testing.T) {
	s := newUsersScope(t)
	nameBefore := s.nameOf(t, s.owner.agentID)

	body := `{"name":"Renamed Elsewhere","role":"member"}`
	outside := s.call(t, http.MethodPut, "/api/users/"+s.owner.agentID, body, s.outsider)
	if outside.status != http.StatusNotFound {
		t.Errorf("changing a person outside the caller's account answered %d %s, want 404", outside.status, outside.body)
	}
	unknown := s.call(t, http.MethodPut, "/api/users/"+usersUnknownID, body, s.outsider)
	if unknown.status != outside.status || unknown.body != outside.body {
		t.Errorf("an unknown person answered %d %s; a person outside the account answered %d %s",
			unknown.status, unknown.body, outside.status, outside.body)
	}
	if role := s.roleIn(t, s.owner.accountID, s.owner.agentID); role != authentities.RoleOwner {
		t.Errorf("after the request the owner of the instance's first account holds %q there, want owner", role)
	}
	if role := s.roleIn(t, s.outsider.accountID, s.owner.agentID); role != "" {
		t.Errorf("after the request the person holds %q in the caller's account, want no membership", role)
	}
	if name := s.nameOf(t, s.owner.agentID); name != nameBefore {
		t.Errorf("after the request the person is named %q, want %q", name, nameBefore)
	}

	member := s.call(t, http.MethodPut, "/api/users/"+s.member.agentID, `{"role":"admin"}`, s.outsider)
	if member.status != http.StatusNotFound {
		t.Errorf("changing a member of another account answered %d %s, want 404", member.status, member.body)
	}
	if role := s.roleIn(t, s.owner.accountID, s.member.agentID); role != authentities.RoleMember {
		t.Errorf("after the request the member of the instance's first account holds %q there, want member", role)
	}
	s.wantRefusalRecorded(t, s.outsider, s.owner)
	s.wantRefusalRecorded(t, s.outsider, s.member)
}

// A person who sets their own role changes it in the account they act in, and
// in no other account.
func TestServe_UsersUpdateOfTheCallerChangesARoleOnlyInTheCallersAccount(t *testing.T) {
	s := newUsersScope(t)

	answer := s.call(t, http.MethodPut, "/api/users/"+s.outsider.agentID, `{"role":"owner"}`, s.outsider)
	if answer.status != http.StatusOK {
		t.Errorf("setting one's own role answered %d %s, want 200", answer.status, answer.body)
	}
	if role := s.roleIn(t, s.owner.accountID, s.outsider.agentID); role != "" {
		t.Errorf("after setting their own role the caller holds %q in the instance's first account, "+
			"which they do not belong to; want no membership", role)
	}
	if role := s.roleIn(t, s.outsider.accountID, s.outsider.agentID); role != authentities.RoleOwner {
		t.Errorf("after setting their own role the caller holds %q in their own account, want owner", role)
	}
}

// An owner still lists, fetches and changes the members of their own account,
// and a role change lands in that account only.
func TestServe_UsersRoutesStillServeAnOwnerForTheMembersOfTheirAccount(t *testing.T) {
	s := newUsersScope(t)

	listed := s.call(t, http.MethodGet, "/api/users", "", s.owner)
	if listed.status != http.StatusOK {
		t.Fatalf("the owner's list answered %d %s, want 200", listed.status, listed.body)
	}
	rows := usersListed(t, listed)
	roles := map[string]string{}
	for _, r := range rows {
		roles[r.ID] = r.Role
	}
	if len(rows) != 2 || roles[s.owner.agentID] != authentities.RoleOwner || roles[s.member.agentID] != authentities.RoleMember {
		t.Errorf("the owner listed %s; want the owner as owner and the member as member", s.describe(rows))
	}

	got := s.call(t, http.MethodGet, "/api/users/"+s.member.agentID, "", s.owner)
	if got.status != http.StatusOK {
		t.Fatalf("fetching a member answered %d %s, want 200", got.status, got.body)
	}
	if row := userAnswered(t, got); row.ID != s.member.agentID || row.Role != authentities.RoleMember || row.Email != s.member.email {
		t.Errorf("fetching a member answered %+v, want the member, as member, with their email", row)
	}

	put := s.call(t, http.MethodPut, "/api/users/"+s.member.agentID, `{"name":"Lantern Clerk","role":"admin"}`, s.owner)
	if put.status != http.StatusOK {
		t.Fatalf("changing a member answered %d %s, want 200", put.status, put.body)
	}
	if row := userAnswered(t, put); row.Role != authentities.RoleAdmin || row.Name != "Lantern Clerk" {
		t.Errorf("changing a member answered %+v, want the new name and admin", row)
	}
	if role := s.roleIn(t, s.owner.accountID, s.member.agentID); role != authentities.RoleAdmin {
		t.Errorf("after the change the member holds %q in the owner's account, want admin", role)
	}
	if role := s.roleIn(t, s.member.ownAccountID, s.member.agentID); role != authentities.RoleOwner {
		t.Errorf("after the change the member holds %q in their own account, want owner (unchanged)", role)
	}
}

// refusalCode is the stable code on a refusal body, or "" when it carries none.
func refusalCode(t *testing.T, answer serveAnswer) string {
	t.Helper()
	var refusal struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(answer.body), &refusal); err != nil {
		t.Fatalf("decode the refusal %q: %v", answer.body, err)
	}
	return refusal.Code
}

// wm-8uq74. A session that names no account the person acts in is refused on
// every users route with unscoped_session, and changes nothing, even though
// the person owns an account and could be matched to it.
func TestServe_UsersRoutesRefuseASessionWithNoActiveAccount(t *testing.T) {
	s := newUsersScope(t)
	legacy := usersSessionIn(t, s.authService, s.sessions, s.credentials, s.owner.agentID, "")

	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/users", ""},
		{http.MethodGet, "/api/users/" + s.member.agentID, ""},
		{http.MethodPut, "/api/users/" + s.member.agentID, `{"name":"Renamed Without An Account","role":"admin"}`},
	} {
		answer := serveCall(t, s.srv, req.method, req.path, req.body, legacy)
		if answer.status != http.StatusUnauthorized || refusalCode(t, answer) != "unscoped_session" {
			t.Errorf("%s %s from a session with no account answered %d %s; want 401 unscoped_session",
				req.method, req.path, answer.status, answer.body)
		}
	}
	if role := s.roleIn(t, s.owner.accountID, s.member.agentID); role != authentities.RoleMember {
		t.Errorf("after the refused requests the member holds %q, want member", role)
	}
	if name := s.nameOf(t, s.member.agentID); name == "Renamed Without An Account" {
		t.Error("a refused request renamed the member")
	}
}

// usersDefaultPage is how many people a users list holds when the client names
// no limit (wm-g7284).
const usersDefaultPage = 100

type usersPageAnswer struct {
	rows    []usersRow
	cursor  string
	hasMore bool
}

func usersPage(t *testing.T, answer serveAnswer) usersPageAnswer {
	t.Helper()
	if answer.status != http.StatusOK {
		t.Fatalf("the list answered %d %s, want 200", answer.status, answer.body)
	}
	var envelope struct {
		Data    []usersRow `json:"data"`
		Cursor  string     `json:"cursor"`
		HasMore bool       `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(answer.body), &envelope); err != nil {
		t.Fatalf("decode the users page: %v", err)
	}
	return usersPageAnswer{rows: envelope.Data, cursor: envelope.Cursor, hasMore: envelope.HasMore}
}

// countingAgents counts the person lookups made for each id.
type countingAgents struct {
	authrepos.AgentRepository
	mu    sync.Mutex
	finds map[string]int
}

func (c *countingAgents) FindByID(ctx context.Context, id string) (*authentities.Agent, error) {
	c.mu.Lock()
	c.finds[id]++
	c.mu.Unlock()
	return c.AgentRepository.FindByID(ctx, id)
}

// countingCredentials counts the credential lookups made for each person.
type countingCredentials struct {
	authrepos.CredentialRepository
	mu    sync.Mutex
	finds map[string]int
}

func (c *countingCredentials) FindByAgent(ctx context.Context, agentID string) ([]*authentities.Credential, error) {
	c.mu.Lock()
	c.finds[agentID]++
	c.mu.Unlock()
	return c.CredentialRepository.FindByAgent(ctx, agentID)
}

// wm-g7284. The owner of a large account lists it a page at a time, with the
// default page size when no limit is named, and no page loads its people one
// by one.
func TestServe_UsersListPagesALargeAccountWithoutAQueryPerMember(t *testing.T) {
	agents := &countingAgents{finds: map[string]int{}}
	credentials := &countingCredentials{finds: map[string]int{}}
	s := newUsersScope(t,
		fx.Decorate(func(r authrepos.AgentRepository) authrepos.AgentRepository {
			agents.AgentRepository = r
			return agents
		}),
		fx.Decorate(func(r authrepos.CredentialRepository) authrepos.CredentialRepository {
			credentials.CredentialRepository = r
			return credentials
		}),
	)
	ctx := context.Background()
	seeded := map[string]bool{}
	for i := 0; i < usersDefaultPage; i++ {
		id := fmt.Sprintf("harbor-paralegal-%03d", i)
		agent, err := (&authentities.Agent{}).With(id, fmt.Sprintf("Harbor Paralegal %03d", i), authentities.AgentTypePerson)
		if err != nil {
			t.Fatalf("build person %s: %v", id, err)
		}
		if err := s.agents.Save(ctx, agent); err != nil {
			t.Fatalf("save person %s: %v", id, err)
		}
		if err := s.accounts.SaveMember(ctx, s.owner.accountID, id, authentities.RoleMember); err != nil {
			t.Fatalf("add %s to the owner's account: %v", id, err)
		}
		seeded[id] = true
	}
	total := usersDefaultPage + 2 // the seeded people, the owner and the member

	first := usersPage(t, s.call(t, http.MethodGet, "/api/users", "", s.owner))
	if len(first.rows) != usersDefaultPage || !first.hasMore || first.cursor == "" {
		t.Fatalf("the first page held %d people (has_more=%v, cursor=%q); want %d, more to come, and a cursor",
			len(first.rows), first.hasMore, first.cursor, usersDefaultPage)
	}
	second := usersPage(t, s.call(t, http.MethodGet, "/api/users?cursor="+url.QueryEscape(first.cursor), "", s.owner))
	if len(second.rows) != total-usersDefaultPage || second.hasMore {
		t.Errorf("the second page held %d people (has_more=%v); want %d and no more",
			len(second.rows), second.hasMore, total-usersDefaultPage)
	}
	listed := map[string]bool{}
	for _, r := range append(first.rows, second.rows...) {
		if listed[r.ID] {
			t.Errorf("%s was listed on both pages", r.ID)
		}
		listed[r.ID] = true
	}
	for id := range seeded {
		if !listed[id] {
			t.Errorf("%s, a member of the account, was on neither page", id)
		}
	}
	if !listed[s.owner.agentID] || !listed[s.member.agentID] || listed[s.outsider.agentID] {
		t.Errorf("the pages listed %s; want the owner and the member and not the outsider", s.describe(append(first.rows, second.rows...)))
	}

	small := usersPage(t, s.call(t, http.MethodGet, "/api/users?limit=5", "", s.owner))
	if len(small.rows) != 5 || !small.hasMore {
		t.Errorf("a page of 5 held %d people (has_more=%v)", len(small.rows), small.hasMore)
	}

	lookups := 0
	for id := range seeded {
		lookups += agents.finds[id] + credentials.finds[id]
	}
	if lookups != 0 {
		t.Errorf("listing the account looked people up one by one %d times; want their records loaded with the page", lookups)
	}
}

// wm-9wslp. The users handler lists people through the member directory the
// module provides, not through the published AccountMemberQuery, so serve's
// graph must hold one.
func TestServe_ProvidesTheMemberDirectoryTheUsersRoutesList(t *testing.T) {
	var directory repositories.AccountMemberDirectory
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	bootServe(t, cfg, fx.Populate(&directory))
	if directory == nil {
		t.Fatal("serve's graph provides no AccountMemberDirectory")
	}
}

// A plain member of an account is refused all three routes by the role check,
// before any person is looked at.
func TestServe_UsersRoutesRefuseAPlainMemberOfTheAccount(t *testing.T) {
	s := newUsersScope(t)

	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/users", ""},
		{http.MethodGet, "/api/users/" + s.owner.agentID, ""},
		{http.MethodPut, "/api/users/" + s.owner.agentID, `{"role":"member"}`},
		{http.MethodPut, "/api/users/" + s.member.agentID, `{"role":"owner"}`},
	} {
		answer := s.call(t, req.method, req.path, req.body, s.member)
		if answer.status != http.StatusForbidden {
			t.Errorf("a plain member's %s %s answered %d %s, want 403", req.method, req.path, answer.status, answer.body)
		}
	}
	if role := s.roleIn(t, s.owner.accountID, s.owner.agentID); role != authentities.RoleOwner {
		t.Errorf("after the member's requests the owner holds %q, want owner", role)
	}
	if role := s.roleIn(t, s.owner.accountID, s.member.agentID); role != authentities.RoleMember {
		t.Errorf("after the member's requests the member holds %q, want member", role)
	}
}
