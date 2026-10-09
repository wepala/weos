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
	"net/http"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"go.uber.org/fx"
)

// wm-gu3pm. Resource types and presets are shared by every account on an
// instance. With INSTANCE_ADMIN_ACCOUNT set, only an owner or admin of
// that account, acting in it, may change them over HTTP. These tests boot
// buildServer, because the property under test is serve's own wiring of those
// routes, not the middleware alone.

// schemaAdminAccount is the instance admin account every scope names.
const schemaAdminAccount = "instance-ops-account"

// schemaWarning is the text of the boot warning an instance with sign-in and
// no instance admin account logs.
const schemaWarning = "INSTANCE_ADMIN_ACCOUNT is not set"

// schemaScope is an instance with sign-in, an instance admin account, and four
// people: the operator owns the instance admin account, admin is an admin of
// it, member is a plain member of it — each signed in to act in it — and
// outsider owns an account of their own, the account their registration made.
type schemaScope struct {
	call     func(t *testing.T, method, path, body string, as usersPerson) serveAnswer
	logs     *bootLogCapture
	operator usersPerson
	admin    usersPerson
	member   usersPerson
	outsider usersPerson
	// operatorAtHome is the operator in the session their registration
	// opened, acting in the account that registration made.
	operatorAtHome usersPerson
}

func newSchemaScope(t *testing.T, configured string) *schemaScope {
	t.Helper()
	s := &schemaScope{logs: &bootLogCapture{}}
	var accounts authrepos.AccountRepository
	var credentials authrepos.CredentialRepository
	var authService authapp.AuthenticationService
	var sessionManager session.SessionManager

	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.PasswordAuthEnabled = true
	cfg.PasswordRegistrationEnabled = true
	cfg.InstanceAdminAccountID = configured
	srv := bootServe(t, cfg,
		fx.Populate(&accounts, &credentials, &authService, &sessionManager),
		fx.Decorate(func(entities.Logger) entities.Logger { return s.logs }),
	)
	s.call = func(t *testing.T, method, path, body string, as usersPerson) serveAnswer {
		t.Helper()
		return serveCall(t, srv, method, path, body, as.cookies)
	}

	ctx := context.Background()
	instance, err := (&authentities.Account{}).With(schemaAdminAccount, "Instance operators",
		authentities.AccountTypeOrganization)
	if err != nil {
		t.Fatalf("build the instance admin account: %v", err)
	}
	if err := accounts.Save(ctx, instance); err != nil {
		t.Fatalf("save the instance admin account: %v", err)
	}

	s.operator = usersRegister(t, srv, "ops@harborlegal.example")
	s.admin = usersRegister(t, srv, "counsel@cedarrealty.example")
	s.member = usersRegister(t, srv, "clerk@lanternhomes.example")
	s.outsider = usersRegister(t, srv, "reader@quillandfern.example")
	s.operatorAtHome = s.operator
	for _, m := range []struct {
		person *usersPerson
		role   string
	}{
		{&s.operator, authentities.RoleOwner},
		{&s.admin, authentities.RoleAdmin},
		{&s.member, authentities.RoleMember},
	} {
		if err := accounts.SaveMember(ctx, schemaAdminAccount, m.person.agentID, m.role); err != nil {
			t.Fatalf("add %s to the instance admin account as %s: %v", m.person.email, m.role, err)
		}
		m.person.cookies = usersSessionIn(t, authService, sessionManager, credentials, m.person.agentID, schemaAdminAccount)
		m.person.accountID = schemaAdminAccount
	}
	return s
}

// schemaRequest is one call on a schema route.
type schemaRequest struct{ method, path, body string }

// schemaMutations are the four routes that change what every account shares,
// aimed at the resource type typeID. Behaviors are per account and are not
// among them (wm-9m6sj).
func schemaMutations(typeID string) []schemaRequest {
	return []schemaRequest{
		{http.MethodPost, "/api/resource-types", `{"name":"Cedar Listing","slug":"cedar-listing"}`},
		{http.MethodPut, "/api/resource-types/" + typeID, `{"name":"Renamed By Someone Else","slug":"harbor-ledger"}`},
		{http.MethodDelete, "/api/resource-types/" + typeID, ""},
		{http.MethodPost, "/api/resource-types/presets/website", ""},
	}
}

// createType creates a resource type as the operator and returns its id.
func (s *schemaScope) createType(t *testing.T, name, slug string) string {
	t.Helper()
	created := s.call(t, http.MethodPost, "/api/resource-types",
		`{"name":"`+name+`","slug":"`+slug+`"}`, s.operator)
	if created.status != http.StatusCreated {
		t.Fatalf("the operator's POST /api/resource-types answered %d %s, want 201", created.status, created.body)
	}
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(created.body), &envelope); err != nil || envelope.Data.ID == "" {
		t.Fatalf("decode the created type %s: %v", created.body, err)
	}
	return envelope.Data.ID
}

// typeName is the name the instance holds for typeID, or "" when it holds none.
func (s *schemaScope) typeName(t *testing.T, typeID string) string {
	t.Helper()
	got := s.call(t, http.MethodGet, "/api/resource-types/"+typeID, "", s.operator)
	if got.status != http.StatusOK {
		return ""
	}
	var envelope struct {
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got.body), &envelope); err != nil {
		t.Fatalf("decode the type %s: %v", got.body, err)
	}
	return envelope.Data.Name
}

func schemaRefusalCode(t *testing.T, answer serveAnswer) string {
	t.Helper()
	var refusal struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal([]byte(answer.body), &refusal); err != nil {
		t.Fatalf("decode the refusal %q: %v", answer.body, err)
	}
	if refusal.Error == "" {
		t.Errorf("the refusal %q carries no error message", answer.body)
	}
	return refusal.Code
}

// The owner of an account of their own — what every first sign-in makes — is
// refused all four schema routes, and the instance's types are left as they were.
func TestServe_InstanceAdminRefusesTheOwnerOfAnotherAccountEverySchemaChange(t *testing.T) {
	s := newSchemaScope(t, schemaAdminAccount)
	typeID := s.createType(t, "Harbor Ledger", "harbor-ledger")

	for _, req := range schemaMutations(typeID) {
		got := s.call(t, req.method, req.path, req.body, s.outsider)
		if got.status != http.StatusForbidden || schemaRefusalCode(t, got) != middleware.InstanceAdminRequiredCode {
			t.Errorf("the outsider's %s %s answered %d %s; want 403 %s",
				req.method, req.path, got.status, got.body, middleware.InstanceAdminRequiredCode)
		}
	}
	if name := s.typeName(t, typeID); name != "Harbor Ledger" {
		t.Errorf("after the outsider's requests the type is named %q, want it unchanged as Harbor Ledger", name)
	}
	if name := s.typeName(t, "urn:type:cedar-listing"); name != "" {
		t.Errorf("the outsider's refused create left a type named %q", name)
	}
}

// A plain member of the instance admin account, acting in it, is refused too:
// the account is the right one, the role is not.
func TestServe_InstanceAdminRefusesAPlainMemberOfTheInstanceAdminAccount(t *testing.T) {
	s := newSchemaScope(t, schemaAdminAccount)
	typeID := s.createType(t, "Harbor Ledger", "harbor-ledger")

	for _, req := range schemaMutations(typeID) {
		got := s.call(t, req.method, req.path, req.body, s.member)
		if got.status != http.StatusForbidden {
			t.Errorf("the plain member's %s %s answered %d %s; want 403", req.method, req.path, got.status, got.body)
		}
	}
	if name := s.typeName(t, typeID); name != "Harbor Ledger" {
		t.Errorf("after the member's requests the type is named %q, want it unchanged as Harbor Ledger", name)
	}
}

// The operator signed in to their own account is one more account owner there.
func TestServe_InstanceAdminRefusesTheOperatorActingInTheirOwnAccount(t *testing.T) {
	s := newSchemaScope(t, schemaAdminAccount)
	typeID := s.createType(t, "Harbor Ledger", "harbor-ledger")

	for _, req := range schemaMutations(typeID) {
		got := s.call(t, req.method, req.path, req.body, s.operatorAtHome)
		if got.status != http.StatusForbidden {
			t.Errorf("the operator's %s %s from their own account answered %d %s, want 403",
				req.method, req.path, got.status, got.body)
		}
	}
	if name := s.typeName(t, typeID); name != "Harbor Ledger" {
		t.Errorf("after the refused delete the type is named %q, want Harbor Ledger", name)
	}
}

// A request with no identity is asked to sign in, on every schema route.
func TestServe_InstanceAdminAsksARequestWithNoSessionToSignIn(t *testing.T) {
	s := newSchemaScope(t, schemaAdminAccount)
	typeID := s.createType(t, "Harbor Ledger", "harbor-ledger")

	for _, req := range schemaMutations(typeID) {
		got := s.call(t, req.method, req.path, req.body, usersPerson{})
		if got.status != http.StatusUnauthorized {
			t.Errorf("%s %s with no session answered %d %s; want 401", req.method, req.path, got.status, got.body)
		}
	}
}

// The operator and an admin of the instance admin account, acting in it, are
// admitted, and their changes land.
func TestServe_InstanceAdminAdmitsTheOperatorAndAnAdminOfTheInstanceAdminAccount(t *testing.T) {
	s := newSchemaScope(t, schemaAdminAccount)
	typeID := s.createType(t, "Harbor Ledger", "harbor-ledger")

	for _, as := range []usersPerson{s.operator, s.admin} {
		for _, req := range schemaMutations(typeID)[3:] {
			got := s.call(t, req.method, req.path, req.body, as)
			if got.status == http.StatusUnauthorized || got.status == http.StatusForbidden {
				t.Errorf("%s's %s %s answered %d %s; want it admitted", as.email, req.method, req.path, got.status, got.body)
			}
		}
	}

	renamed := s.call(t, http.MethodPut, "/api/resource-types/"+typeID, `{"name":"Harbor Ledger Renamed","slug":"harbor-ledger"}`, s.admin)
	if renamed.status != http.StatusOK {
		t.Fatalf("the admin's PUT answered %d %s, want 200", renamed.status, renamed.body)
	}
	if name := s.typeName(t, typeID); name != "Harbor Ledger Renamed" {
		t.Errorf("after the admin's rename the type is named %q, want Harbor Ledger Renamed", name)
	}
	deleted := s.call(t, http.MethodDelete, "/api/resource-types/"+typeID, "", s.operator)
	if deleted.status != http.StatusNoContent {
		t.Fatalf("the operator's DELETE answered %d %s, want 204", deleted.status, deleted.body)
	}
}

// Behaviors are set per account, not for the instance: the service writes the
// caller's own account's override, and only an owner or admin of that account
// may write it. So the instance admin guard does not cover that route, and the
// owner of an account of their own still sets behaviors on it (wm-9m6sj).
func TestServe_InstanceAdminLeavesBehaviorsToTheOwnerOfEachAccount(t *testing.T) {
	s := newSchemaScope(t, schemaAdminAccount)
	s.createType(t, "Harbor Ledger", "harbor-ledger")

	got := s.call(t, http.MethodPut, "/api/resource-types/harbor-ledger/behaviors", `{"slugs":[]}`, s.outsider)
	if got.status != http.StatusOK {
		t.Fatalf("the outsider's PUT behaviors on their own account answered %d %s; want 200", got.status, got.body)
	}
}

// Reading stays open: GET routes answer the outsider as they did before.
func TestServe_InstanceAdminLeavesTheSchemaReadsOpen(t *testing.T) {
	s := newSchemaScope(t, schemaAdminAccount)
	s.createType(t, "Harbor Ledger", "harbor-ledger")

	for _, path := range []string{"/api/resource-types", "/api/resource-types/presets"} {
		if got := s.call(t, http.MethodGet, path, "", s.outsider); got.status != http.StatusOK {
			t.Errorf("the outsider's GET %s answered %d %s, want 200", path, got.status, got.body)
		}
	}
}

// Unset, nothing changes: the owner of any account may still change the
// instance's types — which is the hole, and why serve says so at boot.
func TestServe_InstanceAdminUnsetLeavesTheSchemaRoutesAsTheyWere(t *testing.T) {
	s := newSchemaScope(t, "")
	typeID := s.createType(t, "Harbor Ledger", "harbor-ledger")

	renamed := s.call(t, http.MethodPut, "/api/resource-types/"+typeID, `{"name":"Renamed By The Outsider","slug":"harbor-ledger"}`, s.outsider)
	if renamed.status != http.StatusOK {
		t.Fatalf("unset, the outsider's PUT answered %d %s; want 200, as before", renamed.status, renamed.body)
	}
	created := s.call(t, http.MethodPost, "/api/resource-types", `{"name":"Cedar Listing","slug":"cedar-listing"}`, s.outsider)
	if created.status != http.StatusCreated {
		t.Fatalf("unset, the outsider's POST answered %d %s; want 201, as before", created.status, created.body)
	}
}

// With sign-in on and no instance admin account, serve warns once at boot
// that any signed-in account can change the resource types. Named, it does not.
func TestServe_WarnsAtBootWhenNoInstanceAdminAccountIsSet(t *testing.T) {
	unset := newSchemaScope(t, "")
	warned := unset.logs.mentioning(schemaWarning)
	if len(warned) != 1 || !strings.HasPrefix(warned[0], "warn: ") {
		t.Fatalf("with sign-in and no instance admin account, boot logged %q; want exactly one warning", warned)
	}
	if !strings.Contains(warned[0], "any signed-in account") {
		t.Errorf("the warning %q does not say any signed-in account can change resource types", warned[0])
	}

	set := newSchemaScope(t, schemaAdminAccount)
	if got := set.logs.mentioning(schemaWarning); len(got) != 0 {
		t.Errorf("with an instance admin account set, boot logged %q; want no warning", got)
	}
}

// With no sign-in configured, serve is local development: no warning, and a
// named instance admin account is ignored rather than applied to the dev user.
func TestServe_InstanceAdminDoesNothingWithoutSignIn(t *testing.T) {
	for name, configured := range map[string]string{"unset": "", "set": schemaAdminAccount} {
		t.Run(name, func(t *testing.T) {
			logs := &bootLogCapture{}
			cfg := config.Default()
			cfg.InstanceAdminAccountID = configured
			srv := bootServe(t, cfg, fx.Decorate(func(entities.Logger) entities.Logger { return logs }))

			if got := logs.mentioning(schemaWarning); len(got) != 0 {
				t.Errorf("with no sign-in, boot logged %q; want no instance admin warning", got)
			}
			got := serveCall(t, srv, http.MethodPost, "/api/resource-types", `{"name":"Cedar Listing","slug":"cedar-listing"}`, nil)
			if got.status != http.StatusCreated {
				t.Errorf("with no sign-in, POST /api/resource-types answered %d %s; want 201, as before", got.status, got.body)
			}
		})
	}
}

// schemaMissingAccount is the text of the boot warning for an
// INSTANCE_ADMIN_ACCOUNT that names no account on the instance.
const schemaMissingAccount = "INSTANCE_ADMIN_ACCOUNT names an account that does not exist"

// A mistyped or not-yet-created account locks every schema change, the
// operator's too. serve still starts — on a fresh deploy the operator's
// account exists only after their first sign-in — but it warns once, naming
// the id it looked for (wm-0xf60).
func TestServe_InstanceAdminWarnsAtBootWhenTheNamedAccountDoesNotExist(t *testing.T) {
	logs := &bootLogCapture{}
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.PasswordAuthEnabled = true
	cfg.InstanceAdminAccountID = "no-such-account"
	srv := bootServe(t, cfg, fx.Decorate(func(entities.Logger) entities.Logger { return logs }))

	warned := logs.mentioning(schemaMissingAccount)
	if len(warned) != 1 || !strings.HasPrefix(warned[0], "warn: ") || !strings.Contains(warned[0], "no-such-account") {
		t.Fatalf("with INSTANCE_ADMIN_ACCOUNT naming no account, boot logged %q; want one warning naming no-such-account", warned)
	}
	if !strings.Contains(warned[0], "/api/auth/me") {
		t.Errorf("the warning %q does not say where the account id comes from", warned[0])
	}
	if got := serveCall(t, srv, http.MethodGet, "/api/resource-types/presets", "", nil); got.status == 0 {
		t.Fatalf("serve did not answer after the warning")
	}
}

// When the named account exists at boot, serve does not warn about it.
func TestServe_InstanceAdminDoesNotWarnWhenTheNamedAccountExists(t *testing.T) {
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.PasswordAuthEnabled = true
	dir := t.TempDir()

	var accounts authrepos.AccountRepository
	first := bootServeOn(t, cfg, dir, fx.Populate(&accounts))
	instance, err := (&authentities.Account{}).With(schemaAdminAccount, "Instance operators",
		authentities.AccountTypeOrganization)
	if err != nil {
		t.Fatalf("build the instance admin account: %v", err)
	}
	if err := accounts.Save(context.Background(), instance); err != nil {
		t.Fatalf("save the instance admin account: %v", err)
	}
	first.stop(t)

	logs := &bootLogCapture{}
	cfg.InstanceAdminAccountID = schemaAdminAccount
	bootServeOn(t, cfg, dir, fx.Decorate(func(entities.Logger) entities.Logger { return logs }))
	if got := logs.mentioning(schemaMissingAccount); len(got) != 0 {
		t.Errorf("with the named account present, boot logged %q; want no warning", got)
	}
	if got := logs.mentioning("limited to the instance admin account"); len(got) != 1 {
		t.Errorf("boot logged %q; want one line saying changes are limited to the instance admin account", got)
	}
}

// A value of spaces names no account. serve treats it as unset at boot, the
// same as the guard does, so the boot log and the routes cannot disagree.
func TestServe_InstanceAdminTreatsSpacesAsUnset(t *testing.T) {
	s := newSchemaScope(t, "   ")
	if warned := s.logs.mentioning(schemaWarning); len(warned) != 1 {
		t.Errorf("with INSTANCE_ADMIN_ACCOUNT of spaces, boot logged %q; want the one not-set warning", warned)
	}
	if got := s.logs.mentioning("limited to the instance admin account"); len(got) != 0 {
		t.Errorf("with INSTANCE_ADMIN_ACCOUNT of spaces, boot logged %q; want no claim that changes are limited", got)
	}
}
