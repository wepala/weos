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
	"testing"

	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/internal/config"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
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

	stopped := serveCall(t, srv, http.MethodPost, "/api/admin/stop-impersonation", "", impersonating)
	if stopped.status != http.StatusOK {
		t.Fatalf("stopping answered %d %s, want 200", stopped.status, stopped.body)
	}
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
	req := httptest.NewRequest(http.MethodPost, "/api/admin/impersonate", nil)
	rec := httptest.NewRecorder()
	sess, err := store.New(req, apimw.ImpersonationSessionName)
	if err != nil {
		t.Fatalf("start the impersonation session: %v", err)
	}
	sess.Values[apimw.KeyImpersonatedAgentID] = counsel.agentID
	sess.Values[apimw.KeyRealAgentID] = ops.agentID
	sess.Values[apimw.KeyRealAccountID] = ops.accountID
	if err := sess.Save(req, rec); err != nil {
		t.Fatalf("save the impersonation session: %v", err)
	}
	stale := withCookies(ops.cookies, rec.Result().Cookies())

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
