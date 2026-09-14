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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"go.uber.org/fx"
)

// wm-aj2eb. An app in a native shell holds no cookie for the instance, only
// the token its sign-in handed back, so every protected route has to take that
// token the way the MCP group does — and the token path has to refuse what the
// session path refuses (wm-jwojd). These tests boot serve's own routes. No
// failure message prints a token or a cookie.

const (
	bootMemberEmail   = "marcus.hale@harborlegal.example"
	bootMemberSubject = "208234567891"
	bootMemberName    = "Marcus Hale"
	bootOwnerSubject  = "108234567890"
	bootOwnerName     = "Dana Whitfield"
	// presetProbePath is a preset handler mounted Protected. It answers whom
	// the request was authenticated as.
	presetProbePath = "/bearer-probe"
	confirmDeletion = `{"confirm":"DELETE"}`
)

// doorSignIn is a person signed in through the door: the token the answer
// carries, the cookies it set, and whom and which account it names.
type doorSignIn struct {
	token     string
	cookies   []*http.Cookie
	agentID   string
	accountID string
}

func signInThroughTheDoor(t *testing.T, srv *httptest.Server, door *bootDoor, email, subject, name string) doorSignIn {
	t.Helper()
	answer := serveCall(t, srv, http.MethodPost, "/api/auth/assert", door.assertionBodyFor(t, email, subject, name), nil)
	if answer.status != http.StatusOK {
		t.Fatalf("POST /api/auth/assert for %s answered %d, want 200", email, answer.status)
	}
	var decoded struct {
		Data struct {
			Token string `json:"token"`
			Agent struct {
				ID string `json:"id"`
			} `json:"agent"`
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &decoded); err != nil ||
		decoded.Data.Token == "" || decoded.Data.Agent.ID == "" || decoded.Data.Account.ID == "" {
		t.Fatalf("the sign-in answer for %s carries no token, person or account (decode error: %v)", email, err)
	}
	return doorSignIn{
		token:     decoded.Data.Token,
		cookies:   answer.cookies,
		agentID:   decoded.Data.Agent.ID,
		accountID: decoded.Data.Account.ID,
	}
}

// serveRequest sends method path with body as JSON when there is one, an
// Authorization header when token is not empty, and cookies.
func serveRequest(t *testing.T, srv *httptest.Server, method, path, body, token string, cookies []*http.Cookie) serveAnswer {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return serveSend(t, req)
}

// refusalCode is the code in a refusal's body, or "" when it has none.
func refusalCode(body string) string {
	var refusal struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &refusal); err != nil {
		return ""
	}
	return refusal.Code
}

// withProtectedPresetProbe adds a preset handler mounted Protected, the way a
// product's preset mounts its own reads, to serve's graph.
func withProtectedPresetProbe() fx.Option {
	return fx.Decorate(func(mounted application.PresetHTTPHandlers) application.PresetHTTPHandlers {
		probe := application.MountedHandler{
			Method:    http.MethodGet,
			Path:      presetProbePath,
			Protected: true,
			Source:    "bearer-probe",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				identity := auth.AgentFromCtx(r.Context())
				if identity == nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{
					"agent_id":   identity.AgentID,
					"account_id": identity.ActiveAccountID,
				})
			},
		}
		return append(append(application.PresetHTTPHandlers{}, mounted...), probe)
	})
}

func trustedIssuerConfig(door *bootDoor) config.Config {
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.TrustedIssuer = door.settings()
	return cfg
}

// A token with no cookie reaches the protected routes — core's own, a dynamic
// resource route, and a preset handler mounted Protected — and so does a plain
// browser session, on every one of them.
func TestServe_ProtectedRoutesAnswerTheTokenTheAssertionIssued(t *testing.T) {
	door := newBootDoor(t)
	srv := bootServe(t, trustedIssuerConfig(door), withProtectedPresetProbe())
	owner := signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)

	// A write with only the token, so the dynamic resource route has a type.
	created := serveRequest(t, srv, http.MethodPost, "/api/resource-types",
		`{"name":"Pantry Shelf","slug":"pantry-shelf"}`, owner.token, nil)
	if created.status != http.StatusCreated {
		t.Fatalf("POST /api/resource-types with only the token answered %d %s, want 201", created.status, created.body)
	}

	routes := []string{
		"/api/resource-types",
		"/api/pantry-shelf",
		"/api/notifications",
		"/api/notifications/unread-count",
		"/api/persons",
		"/api/account/export",
		"/api" + presetProbePath,
	}
	for _, path := range routes {
		if got := serveRequest(t, srv, http.MethodGet, path, "", owner.token, nil); got.status != http.StatusOK {
			t.Errorf("GET %s with only the token answered %d %s, want 200", path, got.status, got.body)
		}
		if got := serveRequest(t, srv, http.MethodGet, path, "", "", owner.cookies); got.status != http.StatusOK {
			t.Errorf("GET %s with the browser session answered %d %s, want 200", path, got.status, got.body)
		}
	}

	probe := serveRequest(t, srv, http.MethodGet, "/api"+presetProbePath, "", owner.token, nil)
	if !strings.Contains(probe.body, owner.agentID) || !strings.Contains(probe.body, owner.accountID) {
		t.Fatalf("the preset handler was not given the token's person and account: %s", probe.body)
	}
}

// When a request carries a token and a cookie, the token decides: a good
// token is answered for its own person, and a bad one is refused even beside a
// good cookie.
func TestServe_ATokenBesideACookieIsJudgedByTheToken(t *testing.T) {
	door := newBootDoor(t)
	srv := bootServe(t, trustedIssuerConfig(door), withProtectedPresetProbe())
	owner := signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
	other := signInThroughTheDoor(t, srv, door, bootMemberEmail, bootMemberSubject, bootMemberName)

	both := serveRequest(t, srv, http.MethodGet, "/api"+presetProbePath, "", other.token, owner.cookies)
	if both.status != http.StatusOK || !strings.Contains(both.body, other.agentID) || strings.Contains(both.body, owner.agentID) {
		t.Fatalf("a token beside another person's cookie answered %d %s, want 200 for the token's person", both.status, both.body)
	}

	tampered := serveRequest(t, srv, http.MethodGet, "/api"+presetProbePath, "", other.token+"x", owner.cookies)
	if tampered.status != http.StatusUnauthorized || !strings.Contains(tampered.body, "invalid_token") {
		t.Fatalf("a tampered token beside a good cookie answered %d %s, want 401 invalid_token", tampered.status, tampered.body)
	}
}

// wm-jwojd. A person removed from an account keeps the token they were given
// for it. That token must be refused with the code a session gets, on the
// protected routes and on the MCP group, which share the check.
func TestServe_ARemovedMembersTokenIsRefused(t *testing.T) {
	door := newBootDoor(t)
	var jwtService authapp.JWTService
	var agents authrepos.AgentRepository
	var accounts authrepos.AccountRepository
	srv := bootServe(t, trustedIssuerConfig(door), fx.Populate(&jwtService, &agents, &accounts))
	owner := signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
	member := signInThroughTheDoor(t, srv, door, bootMemberEmail, bootMemberSubject, bootMemberName)

	ctx := context.Background()
	if err := accounts.SaveMember(ctx, owner.accountID, member.agentID, authentities.RoleMember); err != nil {
		t.Fatalf("add the member to the owner's account: %v", err)
	}
	agent, err := agents.FindByID(ctx, member.agentID)
	if err != nil || agent == nil {
		t.Fatalf("read the member: %v", err)
	}
	household, err := accounts.FindByID(ctx, owner.accountID)
	if err != nil || household == nil {
		t.Fatalf("read the owner's account: %v", err)
	}
	token, err := jwtService.IssueToken(ctx, agent, []*authentities.Account{household}, household.GetID(), nil, nil)
	if err != nil {
		t.Fatalf("issue the member's token: %v", err)
	}

	if got := serveRequest(t, srv, http.MethodGet, "/api/resource-types", "", token, nil); got.status != http.StatusOK {
		t.Fatalf("GET /api/resource-types with the member's token answered %d %s, want 200 before the removal", got.status, got.body)
	}

	if err := accounts.RemoveMember(ctx, owner.accountID, member.agentID); err != nil {
		t.Fatalf("remove the member: %v", err)
	}

	for _, path := range []string{"/api/resource-types", "/api/notifications", "/api" + presetProbePath} {
		got := serveRequest(t, srv, http.MethodGet, path, "", token, nil)
		if got.status != http.StatusUnauthorized || refusalCode(got.body) != "account_access_revoked" {
			t.Errorf("GET %s with a removed member's token answered %d %s, want 401 account_access_revoked", path, got.status, got.body)
		}
	}
	if got := bearerMCPCall(t, srv, token); got.status != http.StatusUnauthorized || refusalCode(got.body) != "account_access_revoked" {
		t.Errorf("POST /api/mcp with a removed member's token answered %d %s, want 401 account_access_revoked", got.status, got.body)
	}
}

// The deletion takes the token alone, and a browser session as before.
func TestServe_TheAccountDeletionAnswersTheTokenAlone(t *testing.T) {
	door := newBootDoor(t)
	srv := bootServe(t, trustedIssuerConfig(door))
	app := signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
	browser := signInThroughTheDoor(t, srv, door, bootMemberEmail, bootMemberSubject, bootMemberName)

	byToken := serveRequest(t, srv, http.MethodDelete, "/api/account", confirmDeletion, app.token, nil)
	if byToken.status != http.StatusOK || !strings.Contains(byToken.body, app.accountID) {
		t.Fatalf("DELETE /api/account with only the token answered %d %s, want 200 naming the token's account", byToken.status, byToken.body)
	}
	// wm-sx2zx: the refusal says the account is gone, not merely that the token
	// is bad, so the app shows that the deletion is done instead of offering to
	// sign in again. It keeps the 401 and the invalid_token challenge.
	gone := serveRequest(t, srv, http.MethodGet, "/api/resource-types", "", app.token, nil)
	if gone.status != http.StatusUnauthorized || refusalCode(gone.body) != "account_gone" {
		t.Fatalf("GET /api/resource-types with the deleted account's token answered %d %s, want 401 account_gone", gone.status, gone.body)
	}
	if challenge := gone.header.Get("WWW-Authenticate"); !strings.Contains(challenge, `error="invalid_token"`) {
		t.Fatalf("the gone account's refusal carries the challenge %q, want invalid_token", challenge)
	}

	bySession := serveRequest(t, srv, http.MethodDelete, "/api/account", confirmDeletion, "", browser.cookies)
	if bySession.status != http.StatusOK || !strings.Contains(bySession.body, browser.accountID) {
		t.Fatalf("DELETE /api/account with the browser session answered %d %s, want 200 naming the session's account", bySession.status, bySession.body)
	}
}

// An account whose deletion began and did not finish refuses its token on
// every route, with the code that says so — and still lets that token run the
// deletion again, as a session may.
func TestServe_ALockedAccountsTokenIsRefusedButMayFinishTheDeletion(t *testing.T) {
	door := newBootDoor(t)
	var accounts authrepos.AccountRepository
	var locks repositories.AccountErasureLocks
	srv := bootServe(t, trustedIssuerConfig(door), withProtectedPresetProbe(), fx.Populate(&accounts, &locks))
	owner := signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)

	ctx := context.Background()
	if err := locks.Lock(ctx, owner.accountID, owner.agentID); err != nil {
		t.Fatalf("lock the account: %v", err)
	}
	account, err := accounts.FindByID(ctx, owner.accountID)
	if err != nil || account == nil {
		t.Fatalf("read the account: %v", err)
	}
	if err := account.Deactivate(); err != nil {
		t.Fatalf("deactivate the account: %v", err)
	}
	if err := accounts.Save(ctx, account); err != nil {
		t.Fatalf("save the deactivated account: %v", err)
	}

	for _, path := range []string{"/api/resource-types", "/api" + presetProbePath} {
		got := serveRequest(t, srv, http.MethodGet, path, "", owner.token, nil)
		if got.status != http.StatusUnauthorized || refusalCode(got.body) != "account_erasure_pending" {
			t.Errorf("GET %s with a locked account's token answered %d %s, want 401 account_erasure_pending", path, got.status, got.body)
		}
	}
	if got := bearerMCPCall(t, srv, owner.token); got.status != http.StatusUnauthorized || refusalCode(got.body) != "account_erasure_pending" {
		t.Errorf("POST /api/mcp with a locked account's token answered %d %s, want 401 account_erasure_pending", got.status, got.body)
	}

	finish := serveRequest(t, srv, http.MethodDelete, "/api/account", confirmDeletion, owner.token, nil)
	if finish.status != http.StatusOK || !strings.Contains(finish.body, owner.accountID) {
		t.Fatalf("DELETE /api/account with a locked account's token answered %d %s, want 200 finishing the deletion", finish.status, finish.body)
	}
}

// wm-92vba. A token that names no account is refused on every route that takes
// a token — the protected API, the MCP group and the deletion — with the answer
// the session path gives a session that names none, unscoped_session. No sign-in
// issues such a token; this pins the refusal for any issuer that ever does.
func TestServe_ATokenThatNamesNoAccountIsRefusedAsAnUnscopedSessionIs(t *testing.T) {
	door := newBootDoor(t)
	var jwtService authapp.JWTService
	var agents authrepos.AgentRepository
	srv := bootServe(t, trustedIssuerConfig(door), withProtectedPresetProbe(), fx.Populate(&jwtService, &agents))
	owner := signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)

	ctx := context.Background()
	agent, err := agents.FindByID(ctx, owner.agentID)
	if err != nil || agent == nil {
		t.Fatalf("read the person: %v", err)
	}
	unscoped, err := jwtService.IssueToken(ctx, agent, nil, "", nil, nil)
	if err != nil {
		t.Fatalf("issue a token that names no account: %v", err)
	}

	for _, call := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/resource-types", ""},
		{http.MethodGet, "/api" + presetProbePath, ""},
		{http.MethodDelete, "/api/account", confirmDeletion},
	} {
		got := serveRequest(t, srv, call.method, call.path, call.body, unscoped, nil)
		if got.status != http.StatusUnauthorized || refusalCode(got.body) != "unscoped_session" {
			t.Errorf("%s %s with a token that names no account answered %d %s, want 401 unscoped_session", call.method, call.path, got.status, got.body)
		}
	}
	if got := bearerMCPCall(t, srv, unscoped); got.status != http.StatusUnauthorized || refusalCode(got.body) != "unscoped_session" {
		t.Errorf("POST /api/mcp with a token that names no account answered %d %s, want 401 unscoped_session", got.status, got.body)
	}
	// The owner's own token, which names the account, is untouched.
	if got := serveRequest(t, srv, http.MethodGet, "/api/resource-types", "", owner.token, nil); got.status != http.StatusOK {
		t.Fatalf("GET /api/resource-types with the door's token answered %d %s, want 200", got.status, got.body)
	}
}
