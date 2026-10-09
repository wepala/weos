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
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
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
	body    ErrorEnvelope
}

// throughGuard sends one request through RequireInstanceAdmin configured with
// configured, as identity (nil for a request with no identity).
func throughGuard(t *testing.T, configured string, book accountBook, identity *auth.Identity) guardAnswer {
	t.Helper()
	return throughGuardWith(t, configured, book, identity, nil)
}

// throughGuardWith is throughGuard with prepare, when set, applied to the
// request's context before the guard reads it.
func throughGuardWith(
	t *testing.T, configured string, book authrepos.AccountRepository, identity *auth.Identity,
	prepare func(context.Context) context.Context,
) guardAnswer {
	t.Helper()
	e := echo.New()
	e.Use(Messages())
	reached := false
	e.POST("/api/resource-types", func(c echo.Context) error {
		reached = true
		return c.NoContent(http.StatusCreated)
	}, RequireInstanceAdmin(configured, book, nopLogger{}))

	req := httptest.NewRequest(http.MethodPost, "/api/resource-types", nil)
	if identity != nil {
		req = req.WithContext(auth.ContextWithAgent(req.Context(), identity))
	}
	if prepare != nil {
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				c.SetRequest(c.Request().WithContext(prepare(c.Request().Context())))
				return next(c)
			}
		})
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	answer := guardAnswer{status: rec.Code, reached: reached}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &answer.body); err != nil {
			t.Fatalf("the answer %q is not the error envelope: %v", rec.Body.String(), err)
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
				t.Fatalf("an %s of the instance account, acting in it, answered %d %+v; want the handler reached",
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
	if got.body.Error == "" {
		t.Fatalf("the 401 carries no error message: %+v", got.body)
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
				t.Errorf("unset (%q), %s answered %d %+v; want the handler reached", configured, name, got.status, got.body)
			}
		}
	}
}

type failingRoles struct{ accountBook }

func (failingRoles) FindMemberRole(context.Context, string, string) (string, error) {
	return "", errors.New("database is unreachable")
}

// A role that cannot be read is not a role that was read and found wanting:
// the guard fails closed, with a 500, and never reaches the handler. The 500
// carries a code of its own, so a client can tell it from any other failure
// (wm-poxmk).
func TestRequireInstanceAdmin_FailsClosedWhenTheRoleCannotBeRead(t *testing.T) {
	got := throughGuardWith(t, instanceAccount, failingRoles{instanceBook()}, actingIn(operatorAgent, instanceAccount), nil)
	if got.reached || got.status != http.StatusInternalServerError {
		t.Fatalf("an unreadable role answered %d %+v (handler reached: %v); want 500", got.status, got.body, got.reached)
	}
	if got.body.Error == "" || got.body.Code != InstanceAdminCheckFailedCode {
		t.Fatalf("the 500 is %+v; want an error message and code %q", got.body, InstanceAdminCheckFailedCode)
	}
}

// The refusals are the error envelope every handler answers with, so a
// client parses them with the one parser it has: the messages the request
// gathered before the guard refused it travel with the refusal (wm-poxmk).
func TestRequireInstanceAdmin_RefusesWithTheSharedErrorEnvelope(t *testing.T) {
	note := entities.Message{Type: "warning", Text: "gathered before the guard"}
	gather := func(ctx context.Context) context.Context {
		entities.AddMessage(ctx, note)
		return ctx
	}
	for name, identity := range map[string]*auth.Identity{
		"401": nil,
		"403": actingIn(outsiderAgent, outsiderAccount),
	} {
		t.Run(name, func(t *testing.T) {
			got := throughGuardWith(t, instanceAccount, instanceBook(), identity, gather)
			if got.reached || len(got.body.Messages) != 1 || got.body.Messages[0] != note {
				t.Fatalf("the %s refusal is %d %+v; want the envelope carrying the gathered message", name, got.status, got.body)
			}
		})
	}
}

// The 403 says how to get through: act in the instance admin account, and
// end any impersonation first — under one, the guard judges the person
// impersonated (wm-poxmk).
func TestRequireInstanceAdmin_RefusalSaysHowToGetThrough(t *testing.T) {
	got := throughGuard(t, instanceAccount, instanceBook(), actingIn(operatorAgent, "operator-own-account"))
	wantRefused(t, "the operator acting in their own account", got)
	for _, want := range []string{"instance admin account", "impersonation"} {
		if !strings.Contains(got.body.Error, want) {
			t.Errorf("the 403 says %q; want it to mention %q", got.body.Error, want)
		}
	}
}

// Under an impersonation the refusal log names the impersonator as well as
// the person impersonated, so an operator reading it sees why.
func TestRequireInstanceAdmin_RefusalUnderAnImpersonationNamesTheImpersonator(t *testing.T) {
	logs := &recordingLogger{}
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := context.WithValue(c.Request().Context(), impersonatorKey{}, actingIn(adminAgent, instanceAccount))
			ctx = auth.ContextWithAgent(ctx, actingIn(memberAgent, instanceAccount))
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})
	e.POST("/api/resource-types", func(c echo.Context) error {
		return c.NoContent(http.StatusCreated)
	}, RequireInstanceAdmin(instanceAccount, instanceBook(), logs))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/resource-types", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("an admin impersonating a plain member answered %d %s; want 403", rec.Code, rec.Body.String())
	}
	if !logs.has("impersonator_agent_id", adminAgent) || !logs.has("caller_agent_id", memberAgent) {
		t.Fatalf("the refusal logged %v; want the impersonator %s and the caller %s", logs.lines, adminAgent, memberAgent)
	}
}

// recordingLogger keeps every key/value pair the guard logs.
type recordingLogger struct{ lines [][]any }

func (l *recordingLogger) Debug(context.Context, string, ...any) {}
func (l *recordingLogger) Info(context.Context, string, ...any)  {}
func (l *recordingLogger) Warn(_ context.Context, _ string, f ...any) {
	l.lines = append(l.lines, f)
}
func (l *recordingLogger) Error(_ context.Context, _ string, f ...any) {
	l.lines = append(l.lines, f)
}

func (l *recordingLogger) has(key, value string) bool {
	for _, fields := range l.lines {
		for i := 0; i+1 < len(fields); i += 2 {
			if fields[i] == key && fields[i+1] == value {
				return true
			}
		}
	}
	return false
}

func wantRefused(t *testing.T, who string, got guardAnswer) {
	t.Helper()
	if got.reached || got.status != http.StatusForbidden {
		t.Fatalf("%s answered %d %+v (handler reached: %v); want 403", who, got.status, got.body, got.reached)
	}
	if got.body.Error == "" || got.body.Code != InstanceAdminRequiredCode {
		t.Fatalf("%s was refused with %+v; want an error message and code %q", who, got.body, InstanceAdminRequiredCode)
	}
}
