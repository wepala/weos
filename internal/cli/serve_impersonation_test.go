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
	"strings"
	"testing"
	"time"

	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/gorilla/sessions"
	"go.uber.org/fx"
)

// wm-ptcuk. These tests boot buildServer, because the property under test is
// what serve's own wiring allows: the start route, the session auth in front
// of it, and the impersonation middleware that later acts on the cookie.

// signedUpPerson is a person who registered with a password, and so owns the
// account the registration created for them.
type signedUpPerson struct {
	agentID   string
	accountID string
	cookies   []*http.Cookie
}

// passwordInstance is an instance with open password registration: every
// person who signs up owns an account of their own.
func passwordInstance(t *testing.T, extra ...fx.Option) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.PasswordAuthEnabled = true
	cfg.PasswordRegistrationEnabled = true
	return bootServe(t, cfg, extra...)
}

func signUp(t *testing.T, srv *httptest.Server, email string) signedUpPerson {
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
	return signedUpPerson{agentID: envelope.Data.Agent.ID, accountID: envelope.Data.Account.ID, cookies: answer.cookies}
}

// startImpersonation asks, as caller, to impersonate agentID.
func startImpersonation(t *testing.T, srv *httptest.Server, caller signedUpPerson, agentID string) serveAnswer {
	t.Helper()
	return serveCall(t, srv, http.MethodPost, "/api/admin/impersonate",
		fmt.Sprintf(`{"agent_id":%q}`, agentID), caller.cookies)
}

// actingAccount is the account a request with cookies acts in, as the account
// export reports it, with the status the export answered.
func actingAccount(t *testing.T, srv *httptest.Server, cookies []*http.Cookie) (serveAnswer, string) {
	t.Helper()
	answer := serveCall(t, srv, http.MethodGet, "/api/account/export", "", cookies)
	if answer.status != http.StatusOK {
		return answer, ""
	}
	var doc struct {
		Scope struct {
			AccountID string `json:"accountId"`
		} `json:"weos:exportScope"`
	}
	if err := json.Unmarshal([]byte(answer.body), &doc); err != nil {
		t.Fatalf("decode the account export: %v", err)
	}
	return answer, doc.Scope.AccountID
}

// meAs reads the identity with cookies, and reports whom it answered for and
// whether it said an impersonation is active.
func meAs(t *testing.T, srv *httptest.Server, cookies []*http.Cookie) (id string, impersonating bool) {
	t.Helper()
	answer := serveCall(t, srv, http.MethodGet, "/api/auth/me", "", cookies)
	if answer.status != http.StatusOK {
		t.Fatalf("GET /api/auth/me answered %d", answer.status)
	}
	var envelope struct {
		Data struct {
			ID            string `json:"id"`
			Impersonating bool   `json:"impersonating"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &envelope); err != nil {
		t.Fatalf("decode the identity read: %v", err)
	}
	return envelope.Data.ID, envelope.Data.Impersonating
}

func errorCode(t *testing.T, answer serveAnswer) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(answer.body), &body); err != nil {
		t.Fatalf("the answer %d is not JSON: %v", answer.status, err)
	}
	return body.Code
}

func withCookies(sets ...[]*http.Cookie) []*http.Cookie {
	var all []*http.Cookie
	for _, set := range sets {
		all = append(all, set...)
	}
	return all
}

func impersonationCookieIn(cookies []*http.Cookie) *http.Cookie {
	for _, c := range cookies {
		if c.Name == apimw.ImpersonationSessionName {
			return c
		}
	}
	return nil
}

// The owner of one account is refused an impersonation of a person who is not
// a member of that account, and gets no way to act in that person's account.
func TestServe_ImpersonationOfAPersonOutsideTheCallersAccountIsRefused(t *testing.T) {
	srv := passwordInstance(t)
	ops := signUp(t, srv, "ops@harborlegal.example")
	counsel := signUp(t, srv, "counsel@cedarrealty.example")
	if ops.accountID == counsel.accountID {
		t.Fatalf("the two sign-ups share one account; the test needs two")
	}

	started := startImpersonation(t, srv, ops, counsel.agentID)
	if started.status != http.StatusForbidden || errorCode(t, started) != apimw.CodeImpersonationTargetNotMember {
		t.Errorf("starting an impersonation of a person outside the caller's account answered %d %s, want 403 %s",
			started.status, started.body, apimw.CodeImpersonationTargetNotMember)
	}
	if c := impersonationCookieIn(started.cookies); c != nil && c.MaxAge >= 0 {
		t.Errorf("the refused start set an impersonation cookie")
	}

	answer, acting := actingAccount(t, srv, withCookies(ops.cookies, started.cookies))
	if acting == counsel.accountID {
		t.Fatalf("after the start the caller acts in the other person's account (export answered %d)", answer.status)
	}
	if acting != ops.accountID {
		t.Fatalf("after the refused start the caller's export answered %d for account %q, want their own", answer.status, acting)
	}
}

// A person who does not exist gets exactly the refusal a person outside the
// account gets, so the answer says nothing about who exists on the instance.
func TestServe_ImpersonationOfAnUnknownPersonIsRefusedTheSameWay(t *testing.T) {
	srv := passwordInstance(t)
	ops := signUp(t, srv, "ops@harborlegal.example")
	counsel := signUp(t, srv, "counsel@cedarrealty.example")

	outside := startImpersonation(t, srv, ops, counsel.agentID)
	unknown := startImpersonation(t, srv, ops, "2Zq7mQb0Xn9YpR4sT1vW8kLcE3dA")
	if unknown.status != http.StatusForbidden || errorCode(t, unknown) != apimw.CodeImpersonationTargetNotMember {
		t.Fatalf("an unknown person answered %d %s, want 403 %s", unknown.status, unknown.body, apimw.CodeImpersonationTargetNotMember)
	}
	if unknown.status != outside.status || unknown.body != outside.body {
		t.Fatalf("an unknown person answered %d %s; a person outside the account answered %d %s",
			unknown.status, unknown.body, outside.status, outside.body)
	}
}

// An owner still impersonates a member of their own account, acts as that
// member in that account, and can stop.
func TestServe_ImpersonationOfAMemberOfTheCallersAccountStillWorks(t *testing.T) {
	var accounts authrepos.AccountRepository
	srv := passwordInstance(t, fx.Populate(&accounts))
	ops := signUp(t, srv, "ops@harborlegal.example")
	counsel := signUp(t, srv, "counsel@cedarrealty.example")
	if err := accounts.SaveMember(context.Background(), ops.accountID, counsel.agentID, authentities.RoleMember); err != nil {
		t.Fatalf("add counsel to the caller's account: %v", err)
	}

	started := startImpersonation(t, srv, ops, counsel.agentID)
	if started.status != http.StatusOK {
		t.Fatalf("starting an impersonation of a member answered %d %s, want 200", started.status, started.body)
	}
	impersonating := withCookies(ops.cookies, started.cookies)

	answer, acting := actingAccount(t, srv, impersonating)
	if answer.status != http.StatusOK || acting != ops.accountID {
		t.Fatalf("while impersonating, the export answered %d for account %q, want the caller's account %q",
			answer.status, acting, ops.accountID)
	}
	if id, active := meAs(t, srv, impersonating); id != counsel.agentID || !active {
		t.Fatalf("the identity read answered for %q impersonating=%v, want counsel, impersonating", id, active)
	}
	status, data := impersonationStatus(t, srv, impersonating)
	user, _ := data["user"].(map[string]any)
	if status.status != http.StatusOK || data["active"] != true || user["id"] != counsel.agentID {
		t.Fatalf("the impersonation status answered %d %s, want active for counsel", status.status, status.body)
	}

	stopped := serveCall(t, srv, http.MethodPost, "/api/admin/stop-impersonation", "", impersonating)
	if stopped.status != http.StatusOK {
		t.Fatalf("stopping answered %d %s, want 200", stopped.status, stopped.body)
	}
	if !impersonationCleared(stopped) {
		t.Fatalf("stopping did not clear the impersonation cookie")
	}
}

// wm-ptcuk, Copilot review 5203947880. A start refused with the impersonation
// code while an impersonation is held ends the held one: the admin reads that
// code as "the impersonation ended" and reads the identity again, which must
// not put the old banner back.
func TestServe_ARefusedStartEndsTheImpersonationHeld(t *testing.T) {
	var accounts authrepos.AccountRepository
	srv := passwordInstance(t, fx.Populate(&accounts))
	ops := signUp(t, srv, "ops@harborlegal.example")
	counsel := signUp(t, srv, "counsel@cedarrealty.example")
	broker := signUp(t, srv, "broker@lanternhomes.example")
	if err := accounts.SaveMember(context.Background(), ops.accountID, counsel.agentID, authentities.RoleMember); err != nil {
		t.Fatalf("add counsel to the caller's account: %v", err)
	}
	started := startImpersonation(t, srv, ops, counsel.agentID)
	if started.status != http.StatusOK {
		t.Fatalf("starting an impersonation of a member answered %d %s, want 200", started.status, started.body)
	}
	holding := ops
	holding.cookies = withCookies(ops.cookies, started.cookies)

	refused := startImpersonation(t, srv, holding, broker.agentID)
	if refused.status != http.StatusForbidden || errorCode(t, refused) != apimw.CodeImpersonationTargetNotMember {
		t.Fatalf("starting an impersonation of a person outside the account answered %d %s, want 403 %s",
			refused.status, refused.body, apimw.CodeImpersonationTargetNotMember)
	}
	if !impersonationCleared(refused) {
		t.Fatalf("the refused start left the held impersonation cookie in place")
	}
	// The browser drops the expired cookie, so the next identity read carries
	// only the session.
	if id, active := meAs(t, srv, ops.cookies); id != ops.agentID || active {
		t.Fatalf("after the refusal the identity read answered for %q impersonating=%v, want ops, not impersonating", id, active)
	}
}

// wm-1yjuv. A cookie that names another person than the one signed in — left
// on a shared browser, or made by hand — is never reported as an
// impersonation, and the status, identity and stop routes each end it. The
// stop is recorded against the person who made the request.
func TestServe_AnImpersonationCookieStartedByAnotherPersonIsNeitherReportedNorKept(t *testing.T) {
	var accounts authrepos.AccountRepository
	logs, capture := capturedLogs()
	srv := passwordInstance(t, fx.Populate(&accounts), capture)
	ops := signUp(t, srv, "ops@harborlegal.example")
	counsel := signUp(t, srv, "counsel@cedarrealty.example")
	broker := signUp(t, srv, "broker@lanternhomes.example")
	if err := accounts.SaveMember(context.Background(), ops.accountID, counsel.agentID, authentities.RoleMember); err != nil {
		t.Fatalf("add counsel to the caller's account: %v", err)
	}
	started := startImpersonation(t, srv, ops, counsel.agentID)
	if started.status != http.StatusOK {
		t.Fatalf("starting an impersonation of a member answered %d %s, want 200", started.status, started.body)
	}
	left := withCookies(broker.cookies, started.cookies)

	status, data := impersonationStatus(t, srv, left)
	if status.status != http.StatusOK || data["active"] != false {
		t.Fatalf("the status for another person's cookie answered %d %s, want active false", status.status, status.body)
	}
	if _, named := data["user"]; named || strings.Contains(status.body, counsel.agentID) {
		t.Fatalf("the status for another person's cookie names a person: %s", status.body)
	}
	if !impersonationCleared(status) {
		t.Fatalf("the status for another person's cookie did not clear it")
	}

	me := serveCall(t, srv, http.MethodGet, "/api/auth/me", "", left)
	var identity struct {
		Data struct {
			ID            string `json:"id"`
			Impersonating bool   `json:"impersonating"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(me.body), &identity); err != nil || me.status != http.StatusOK {
		t.Fatalf("the identity read answered %d %s", me.status, me.body)
	}
	if identity.Data.ID != broker.agentID || identity.Data.Impersonating {
		t.Fatalf("the identity read answered for %q impersonating=%v, want broker, not impersonating",
			identity.Data.ID, identity.Data.Impersonating)
	}
	if !impersonationCleared(me) {
		t.Fatalf("the identity read did not clear a cookie started by another person")
	}

	stopped := serveCall(t, srv, http.MethodPost, "/api/admin/stop-impersonation", "", left)
	if stopped.status != http.StatusOK || !impersonationCleared(stopped) {
		t.Fatalf("stopping with another person's cookie answered %d %s cleared=%v, want 200 and the cookie cleared",
			stopped.status, stopped.body, impersonationCleared(stopped))
	}
	if lines := logs.mentioning("impersonation stopped"); len(lines) != 0 {
		t.Fatalf("a stop by broker was recorded as the stop of ops's impersonation:\n%s", strings.Join(lines, "\n"))
	}
	cleared := logs.mentioning("not started by the person signed in")
	if len(cleared) != 1 || !strings.Contains(cleared[0], "agent_id "+broker.agentID) {
		t.Fatalf("want one line recording that broker cleared the cookie, got:\n%s", strings.Join(cleared, "\n"))
	}
}

// wm-1yjuv. Stopping ends an impersonation the protected routes would refuse,
// rather than being refused with it, and records the person signed in.
func TestServe_StoppingEndsAnImpersonationTheProtectedRoutesRefuse(t *testing.T) {
	var store sessions.Store
	logs, capture := capturedLogs()
	srv := passwordInstance(t, fx.Populate(&store), capture)
	ops := signUp(t, srv, "ops@harborlegal.example")
	counsel := signUp(t, srv, "counsel@cedarrealty.example")
	stale := withCookies(ops.cookies, mintImpersonationCookie(t, store, counsel.agentID, ops.agentID, ops.accountID))

	stopped := serveCall(t, srv, http.MethodPost, "/api/admin/stop-impersonation", "", stale)
	if stopped.status != http.StatusOK || !impersonationCleared(stopped) {
		t.Fatalf("stopping a refused impersonation answered %d %s cleared=%v, want 200 and the cookie cleared",
			stopped.status, stopped.body, impersonationCleared(stopped))
	}
	lines := logs.mentioning("impersonation stopped")
	if len(lines) != 1 || !strings.Contains(lines[0], ops.agentID) {
		t.Fatalf("want one stop line naming ops, got:\n%s", strings.Join(lines, "\n"))
	}
}

// sessionIn opens a second session for person, acting in accountID, as a second
// tab signed in to another account would, and returns its cookies.
func sessionIn(t *testing.T, authService authapp.AuthenticationService, sessionManager session.SessionManager,
	credentials authrepos.CredentialRepository, person signedUpPerson, accountID string) []*http.Cookie {
	t.Helper()
	ctx := context.Background()
	creds, err := credentials.FindByAgent(ctx, person.agentID)
	if err != nil || len(creds) == 0 {
		t.Fatalf("find the person's credential: %v", err)
	}
	opened, err := authService.CreateSession(ctx, person.agentID, accountID, creds[0].GetID(), "127.0.0.1", "serve-test", time.Hour)
	if err != nil {
		t.Fatalf("open a session in account %s: %v", accountID, err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	rec := httptest.NewRecorder()
	if err := sessionManager.CreateHTTPSession(rec, req, session.SessionData{
		SessionID: opened.GetID(),
		AgentID:   person.agentID,
		AccountID: accountID,
		CreatedAt: time.Now(),
		ExpiresAt: opened.ExpiresAt(),
	}); err != nil {
		t.Fatalf("write the session cookie: %v", err)
	}
	return rec.Result().Cookies()
}

// wm-dpzo5. An impersonation stays in the account it started in. When the
// caller acts in another account — where the same person is also a member and
// the caller is also an admin — the impersonation is refused and ended there
// rather than carried along.
func TestServe_AnImpersonationDoesNotFollowTheCallerIntoAnotherAccount(t *testing.T) {
	var accounts authrepos.AccountRepository
	var credentials authrepos.CredentialRepository
	var authService authapp.AuthenticationService
	var sessionManager session.SessionManager
	srv := passwordInstance(t, fx.Populate(&accounts, &credentials, &authService, &sessionManager))
	ops := signUp(t, srv, "ops@harborlegal.example")
	broker := signUp(t, srv, "broker@cedarrealty.example")
	counsel := signUp(t, srv, "counsel@lanternhomes.example")
	for _, m := range []struct{ account, agent, role string }{
		{ops.accountID, counsel.agentID, authentities.RoleMember},
		{broker.accountID, counsel.agentID, authentities.RoleMember},
		{broker.accountID, ops.agentID, authentities.RoleAdmin},
	} {
		if err := accounts.SaveMember(context.Background(), m.account, m.agent, m.role); err != nil {
			t.Fatalf("add %s to %s as %s: %v", m.agent, m.account, m.role, err)
		}
	}

	started := startImpersonation(t, srv, ops, counsel.agentID)
	if started.status != http.StatusOK {
		t.Fatalf("starting an impersonation of a member answered %d %s, want 200", started.status, started.body)
	}
	impersonating := withCookies(ops.cookies, started.cookies)
	if answer, acting := actingAccount(t, srv, impersonating); answer.status != http.StatusOK || acting != ops.accountID {
		t.Fatalf("while impersonating, the export answered %d for account %q, want the account it started in", answer.status, acting)
	}

	inOtherAccount := sessionIn(t, authService, sessionManager, credentials, ops, broker.accountID)
	if answer, acting := actingAccount(t, srv, inOtherAccount); answer.status != http.StatusOK || acting != broker.accountID {
		t.Fatalf("the test could not put the caller in the other account: the export answered %d %s for %q",
			answer.status, answer.body, acting)
	}
	moved := withCookies(inOtherAccount, started.cookies)

	if id, active := meAs(t, srv, moved); id != ops.agentID || active {
		t.Fatalf("the identity read answered for %q impersonating=%v, want ops, not impersonating", id, active)
	}
	refused, acting := actingAccount(t, srv, moved)
	if refused.status != http.StatusForbidden || errorCode(t, refused) != apimw.CodeImpersonationTargetNotMember {
		t.Fatalf("with the caller in the other account, the impersonation answered %d %s for account %q, want 403 %s",
			refused.status, refused.body, acting, apimw.CodeImpersonationTargetNotMember)
	}
	if !impersonationCleared(refused) {
		t.Fatalf("the refusal did not clear the impersonation cookie")
	}
}

// wm-4dnpt. A person who is not an owner or admin of the account they act in
// is refused the start, and the refusal is recorded with who asked, in which
// account, for whom, and from where — and no cookie value. The start and stop
// lines name the account whose authority allowed the impersonation.
func TestServe_ImpersonationRecordsTheAccountAndTheRefusalOfANonAdmin(t *testing.T) {
	var accounts authrepos.AccountRepository
	var credentials authrepos.CredentialRepository
	var authService authapp.AuthenticationService
	var sessionManager session.SessionManager
	logs, capture := capturedLogs()
	srv := passwordInstance(t, fx.Populate(&accounts, &credentials, &authService, &sessionManager), capture)
	ops := signUp(t, srv, "ops@harborlegal.example")
	counsel := signUp(t, srv, "counsel@cedarrealty.example")
	if err := accounts.SaveMember(context.Background(), ops.accountID, counsel.agentID, authentities.RoleMember); err != nil {
		t.Fatalf("add counsel to the caller's account: %v", err)
	}

	// counsel, acting in Harbor Legal where they are a member, asks to
	// impersonate its owner.
	asMember := sessionIn(t, authService, sessionManager, credentials, counsel, ops.accountID)
	refused := serveCall(t, srv, http.MethodPost, "/api/admin/impersonate",
		fmt.Sprintf(`{"agent_id":%q}`, ops.agentID), asMember)
	if refused.status != http.StatusForbidden {
		t.Fatalf("a member's start answered %d %s, want 403", refused.status, refused.body)
	}
	lines := logs.mentioning("not an owner or admin")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "warn: ") {
		t.Fatalf("want one warning recording the refusal, got:\n%s", strings.Join(lines, "\n"))
	}
	for _, want := range []string{
		"admin_agent_id " + counsel.agentID,
		"account_id " + ops.accountID,
		"target_agent_id " + ops.agentID,
		"ip ",
	} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the refusal line %q does not carry %q", lines[0], want)
		}
	}
	for _, c := range asMember {
		if c.Value != "" && strings.Contains(logs.text(), c.Value) {
			t.Errorf("the log carries the value of the %s cookie", c.Name)
		}
	}

	started := startImpersonation(t, srv, ops, counsel.agentID)
	if started.status != http.StatusOK {
		t.Fatalf("starting an impersonation of a member answered %d %s, want 200", started.status, started.body)
	}
	stopped := serveCall(t, srv, http.MethodPost, "/api/admin/stop-impersonation", "", withCookies(ops.cookies, started.cookies))
	if stopped.status != http.StatusOK {
		t.Fatalf("stopping answered %d %s, want 200", stopped.status, stopped.body)
	}
	for _, msg := range []string{"impersonation started", "impersonation stopped"} {
		got := logs.mentioning(msg)
		if len(got) != 1 || !strings.Contains(got[0], "account_id "+ops.accountID) {
			t.Errorf("want one %q line naming account %s, got:\n%s", msg, ops.accountID, strings.Join(got, "\n"))
		}
	}
}

// wm-1yjuv. Signing out ends an impersonation, so the next person to sign in
// on the same browser does not inherit the cookie.
func TestServe_SigningOutEndsTheImpersonation(t *testing.T) {
	var accounts authrepos.AccountRepository
	srv := passwordInstance(t, fx.Populate(&accounts))
	ops := signUp(t, srv, "ops@harborlegal.example")
	counsel := signUp(t, srv, "counsel@cedarrealty.example")
	if err := accounts.SaveMember(context.Background(), ops.accountID, counsel.agentID, authentities.RoleMember); err != nil {
		t.Fatalf("add counsel to the caller's account: %v", err)
	}
	started := startImpersonation(t, srv, ops, counsel.agentID)
	if started.status != http.StatusOK {
		t.Fatalf("starting an impersonation of a member answered %d %s, want 200", started.status, started.body)
	}

	out := serveCall(t, srv, http.MethodPost, "/api/auth/logout", "", withCookies(ops.cookies, started.cookies))
	if out.status >= http.StatusBadRequest {
		t.Fatalf("signing out answered %d %s", out.status, out.body)
	}
	if !impersonationCleared(out) {
		t.Fatalf("signing out did not clear the impersonation cookie")
	}
}

// mintImpersonationCookie encodes, with the server's own store, the cookie the
// start route writes for an impersonation of target started by real in
// account.
func mintImpersonationCookie(t *testing.T, store sessions.Store, target, real, account string) []*http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/impersonate", nil)
	rec := httptest.NewRecorder()
	sess, err := store.New(req, apimw.ImpersonationSessionName)
	if err != nil {
		t.Fatalf("start the impersonation session: %v", err)
	}
	sess.Values[apimw.KeyImpersonatedAgentID] = target
	sess.Values[apimw.KeyRealAgentID] = real
	sess.Values[apimw.KeyRealAccountID] = account
	if err := sess.Save(req, rec); err != nil {
		t.Fatalf("save the impersonation session: %v", err)
	}
	return rec.Result().Cookies()
}

// impersonationCleared reports whether answer expired the impersonation cookie.
func impersonationCleared(answer serveAnswer) bool {
	c := impersonationCookieIn(answer.cookies)
	return c != nil && c.MaxAge < 0
}

// impersonationStatus reads GET /api/admin/impersonation-status with cookies
// and returns the answer and its data.
func impersonationStatus(t *testing.T, srv *httptest.Server, cookies []*http.Cookie) (serveAnswer, map[string]any) {
	t.Helper()
	answer := serveCall(t, srv, http.MethodGet, "/api/admin/impersonation-status", "", cookies)
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if answer.status == http.StatusOK {
		if err := json.Unmarshal([]byte(answer.body), &envelope); err != nil {
			t.Fatalf("decode the impersonation status: %v", err)
		}
	}
	return answer, envelope.Data
}

// capturedLogs replaces the server's logger with one that records every line.
func capturedLogs() (*bootLogCapture, fx.Option) {
	logs := &bootLogCapture{}
	return logs, fx.Decorate(func(entities.Logger) entities.Logger { return logs })
}

// An impersonation cookie minted before the start route checked membership
// does not keep working: the protected routes refuse it and clear it, and
// the identity read does not report it.
func TestServe_AnImpersonationCookieForAPersonOutsideTheAccountIsNotHonored(t *testing.T) {
	var store sessions.Store
	srv := passwordInstance(t, fx.Populate(&store))
	ops := signUp(t, srv, "ops@harborlegal.example")
	counsel := signUp(t, srv, "counsel@cedarrealty.example")

	// The cookie the start route used to write for this request.
	stale := withCookies(ops.cookies, mintImpersonationCookie(t, store, counsel.agentID, ops.agentID, ops.accountID))

	if id, active := meAs(t, srv, stale); id != ops.agentID || active {
		t.Fatalf("the identity read answered for %q impersonating=%v, want ops, not impersonating", id, active)
	}

	refused, acting := actingAccount(t, srv, stale)
	if refused.status != http.StatusForbidden || errorCode(t, refused) != apimw.CodeImpersonationTargetNotMember {
		t.Fatalf("a protected read with the cookie answered %d for account %q, want 403 %s",
			refused.status, acting, apimw.CodeImpersonationTargetNotMember)
	}
	if c := impersonationCookieIn(refused.cookies); c == nil || c.MaxAge >= 0 {
		t.Fatalf("the refusal did not clear the impersonation cookie")
	}

	answer, acting := actingAccount(t, srv, ops.cookies)
	if answer.status != http.StatusOK || acting != ops.accountID {
		t.Fatalf("with the cookie cleared the export answered %d for account %q, want the caller's own", answer.status, acting)
	}
}
