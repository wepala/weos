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

package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/labstack/echo/v4"
)

// wm-gu3pm. Resource types are shared by every account on an instance, so
// owning an account is not enough to change them: on an instance where every
// first sign-in makes its person the owner of a new account, that would let
// anyone who signs in rewrite or delete the types everyone else depends on.

const (
	instanceAccount = "instance-ops-account"
	operatorAgent   = "operator-agent"
	adminAgent      = "admin-agent"
	memberAgent     = "member-agent"
	outsiderAgent   = "outsider-agent"
	outsiderAccount = "outsider-own-account"
)

// instanceBook is an instance with one operator account: an owner, an admin
// and a plain member of it, and an outsider who owns an account of their own.
// The operator owns an account of their own too.
func instanceBook() accountBook {
	return accountBook{roles: map[string]string{
		operatorAgent + "|" + instanceAccount:        authentities.RoleOwner,
		operatorAgent + "|" + "operator-own-account": authentities.RoleOwner,
		adminAgent + "|" + instanceAccount:           authentities.RoleAdmin,
		memberAgent + "|" + instanceAccount:          authentities.RoleMember,
		outsiderAgent + "|" + outsiderAccount:        authentities.RoleOwner,
	}}
}

type guardAnswer struct {
	status  int
	reached bool
	body    map[string]string
}

// throughGuard sends one request through RequireInstanceAdmin configured with
// configured, as identity (nil for a request with no identity).
func throughGuard(t *testing.T, configured string, book accountBook, identity *auth.Identity) guardAnswer {
	t.Helper()
	e := echo.New()
	reached := false
	e.POST("/api/resource-types", func(c echo.Context) error {
		reached = true
		return c.NoContent(http.StatusCreated)
	}, RequireInstanceAdmin(configured, book, nopLogger{}))

	req := httptest.NewRequest(http.MethodPost, "/api/resource-types", nil)
	if identity != nil {
		req = req.WithContext(auth.ContextWithAgent(req.Context(), identity))
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	answer := guardAnswer{status: rec.Code, reached: reached}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &answer.body); err != nil {
			t.Fatalf("the answer %q is not a JSON object: %v", rec.Body.String(), err)
		}
	}
	return answer
}

func actingIn(agentID, accountID string) *auth.Identity {
	return &auth.Identity{AgentID: agentID, AccountIDs: []string{accountID}, ActiveAccountID: accountID}
}

func TestRequireInstanceAdmin_AdmitsAnOwnerOrAdminActingInTheInstanceAccount(t *testing.T) {
	for name, agent := range map[string]string{"owner": operatorAgent, "admin": adminAgent} {
		t.Run(name, func(t *testing.T) {
			got := throughGuard(t, instanceAccount, instanceBook(), actingIn(agent, instanceAccount))
			if !got.reached || got.status != http.StatusCreated {
				t.Fatalf("an %s of the instance account, acting in it, answered %d %v; want the handler reached",
					name, got.status, got.body)
			}
		})
	}
}

func TestRequireInstanceAdmin_RefusesAnOwnerOfAnotherAccount(t *testing.T) {
	got := throughGuard(t, instanceAccount, instanceBook(), actingIn(outsiderAgent, outsiderAccount))
	wantRefused(t, "the owner of another account", got)
}

func TestRequireInstanceAdmin_RefusesAPlainMemberOfTheInstanceAccount(t *testing.T) {
	got := throughGuard(t, instanceAccount, instanceBook(), actingIn(memberAgent, instanceAccount))
	wantRefused(t, "a plain member of the instance account", got)
}

// The operator's role is read in the account they act in: signed in to their
// own account, the operator is one more account owner, and is refused.
func TestRequireInstanceAdmin_RefusesTheOperatorActingInAnotherAccount(t *testing.T) {
	got := throughGuard(t, instanceAccount, instanceBook(), actingIn(operatorAgent, "operator-own-account"))
	wantRefused(t, "the instance account's owner acting in their own account", got)
}

// A session that has settled on no account has not finished signing in.
func TestRequireInstanceAdmin_RefusesAnIdentityWithNoActiveAccount(t *testing.T) {
	identity := &auth.Identity{AgentID: operatorAgent, AccountIDs: []string{instanceAccount}}
	got := throughGuard(t, instanceAccount, instanceBook(), identity)
	wantRefused(t, "the operator with no active account", got)
}

// Naming the instance account as the active account proves nothing by itself:
// the role is read there, so a person who is not a member is refused.
func TestRequireInstanceAdmin_RefusesANonMemberClaimingTheInstanceAccount(t *testing.T) {
	got := throughGuard(t, instanceAccount, instanceBook(), actingIn(outsiderAgent, instanceAccount))
	wantRefused(t, "a non-member whose identity names the instance account", got)
}

func TestRequireInstanceAdmin_AsksARequestWithNoIdentityToSignIn(t *testing.T) {
	got := throughGuard(t, instanceAccount, instanceBook(), nil)
	if got.reached || got.status != http.StatusUnauthorized {
		t.Fatalf("a request with no identity answered %d %v (handler reached: %v); want 401",
			got.status, got.body, got.reached)
	}
	if got.body["error"] == "" {
		t.Fatalf("the 401 carries no error message: %v", got.body)
	}
}

// Unset, the guard changes nothing: every caller the routes admitted before
// is admitted still, so an instance that does not name an operator account
// keeps working exactly as it did.
func TestRequireInstanceAdmin_UnsetAdmitsEveryCaller(t *testing.T) {
	callers := map[string]*auth.Identity{
		"the owner of another account": actingIn(outsiderAgent, outsiderAccount),
		"a plain member":               actingIn(memberAgent, instanceAccount),
		"a request with no identity":   nil,
	}
	for _, configured := range []string{"", "   "} {
		for name, identity := range callers {
			got := throughGuard(t, configured, instanceBook(), identity)
			if !got.reached || got.status != http.StatusCreated {
				t.Errorf("unset (%q), %s answered %d %v; want the handler reached", configured, name, got.status, got.body)
			}
		}
	}
}

type failingRoles struct{ accountBook }

func (failingRoles) FindMemberRole(context.Context, string, string) (string, error) {
	return "", errors.New("database is unreachable")
}

// A role that cannot be read is not a role that was read and found wanting:
// the guard fails closed, with a 500, and never reaches the handler.
func TestRequireInstanceAdmin_FailsClosedWhenTheRoleCannotBeRead(t *testing.T) {
	e := echo.New()
	reached := false
	e.POST("/api/resource-types", func(c echo.Context) error {
		reached = true
		return c.NoContent(http.StatusCreated)
	}, RequireInstanceAdmin(instanceAccount, failingRoles{instanceBook()}, nopLogger{}))
	req := httptest.NewRequest(http.MethodPost, "/api/resource-types", nil)
	req = req.WithContext(auth.ContextWithAgent(req.Context(), actingIn(operatorAgent, instanceAccount)))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if reached || rec.Code != http.StatusInternalServerError {
		t.Fatalf("an unreadable role answered %d %s (handler reached: %v); want 500", rec.Code, rec.Body.String(), reached)
	}
}

func wantRefused(t *testing.T, who string, got guardAnswer) {
	t.Helper()
	if got.reached || got.status != http.StatusForbidden {
		t.Fatalf("%s answered %d %v (handler reached: %v); want 403", who, got.status, got.body, got.reached)
	}
	if got.body["error"] == "" || got.body["code"] != InstanceAdminRequiredCode {
		t.Fatalf("%s was refused with %v; want an error message and code %q", who, got.body, InstanceAdminRequiredCode)
	}
}
