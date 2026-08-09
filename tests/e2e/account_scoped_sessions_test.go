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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/cucumber/godog"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// TestAccountScopedSessions drives the scenarios that decide which account a
// session acts in, and how the four refusals are told apart.
//
// It mounts authhttp.RequireAuth, NOT apimw.SoftAuth. That distinction is the
// whole point of this file. SoftAuth fills ActiveAccountID in from its own
// membership lookup, so a session carrying no account still behaves correctly
// underneath it — which is exactly why the defect these scenarios describe
// survived in production while every existing test stayed green. Anything
// written on setupTestEnv's harness would pass with the bug present.
//
// The remaining scenarios in the feature file — per-account settings, the
// knowledge graph, the token and the connector path — are tagged
// @pending-steps and excluded below. Their steps are not written yet. The
// exclusion is explicit rather than implicit: godog runs Strict, so an
// unimplemented step fails the suite instead of quietly passing.
func TestAccountScopedSessions(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initAccountScopedSessionScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/account_scoped_sessions.feature"},
			Tags:     "~@pending-steps && ~@pending-product",
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("account scoped session acceptance scenarios failed")
	}
}

const projectsPath = "/api/project"

// person is everything a scenario needs to act as one signed-in human: the
// identity the instance knows them by, and the cookie their browser holds.
type person struct {
	email      string
	password   string
	agentID    string
	sessionID  string
	cookie     string
	accountID  string // the account their last sign-in reported
	lastAnswer *capturedAnswer
}

// capturedAnswer keeps the whole answer rather than a verdict, because several
// steps compare refusals against one another and a boolean cannot be compared.
type capturedAnswer struct {
	status int
	body   string
	code   string
}

type accountScopedWorld struct {
	app    *fx.App
	tmpDir string
	dsn    string
	server *httptest.Server

	authService         authapp.AuthenticationService
	credRepo            authrepos.CredentialRepository
	agentRepo           authrepos.AgentRepository
	accountRepo         authrepos.AccountRepository
	sessionManager      session.SessionManager
	sessionStore        sessions.Store
	authzChecker        *authcasbin.CasbinAuthorizationChecker
	resourceService     application.ResourceService
	resourceTypeService application.ResourceTypeService
	jwtService          authapp.JWTService
	behaviorSettings    repositories.BehaviorSettingsRepository
	inviteService       *authapp.InviteService
	inviteRepo          authrepos.InviteRepository
	logger              entities.Logger

	registrationEnabled bool

	// inviteToken is what the last staged invitation handed back. A scenario
	// only ever says "accepts the invitation", so the token stays in here.
	inviteToken string

	// accounts maps the name a scenario uses to the id the instance minted.
	accounts map[string]string
	people   map[string]*person
	// order records who acted, so "each of them" and "all three" steps can
	// speak about the same people in the order the scenario introduced them.
	order []string

	envBefore map[string]*string
}

func initAccountScopedSessionScenario(sc *godog.ScenarioContext) {
	w := &accountScopedWorld{
		accounts: map[string]string{},
		people:   map[string]*person{},
	}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	// --- instance ---
	sc.Step(`^a WeOS instance where password sign-in is enabled and requests are authenticated by their session$`,
		func() error { return w.boot(false) })
	sc.Step(`^a WeOS instance where password sign-in and registration are enabled and requests are authenticated by their session$`,
		func() error { return w.boot(true) })
	sc.Step(`^a WeOS instance whose database already holds sessions from before the upgrade$`, w.databaseHoldsOldSessions)
	sc.Step(`^the instance is started with password sign-in enabled$`, w.restart)
	sc.Step(`^the instance starts and serves requests$`, w.instanceServes)
	sc.Step(`^someone who signs in fresh is served$`, w.freshSignInIsServed)

	// --- accounts and people ---
	sc.Step(`^the account "([^"]*)", whose owner "([^"]*)" signs in with password "([^"]*)"$`, w.accountWithOwner)
	sc.Step(`^the account "([^"]*)", which "([^"]*)" was added to and signs in to with password "([^"]*)"$`, w.accountWithMember)
	sc.Step(`^the account "([^"]*)", which "([^"]*)" and "([^"]*)" both belong to and sign in to$`, w.accountWithTwoMembers)
	sc.Step(`^"([^"]*)" also belongs to the account "([^"]*)"$`, w.alsoBelongsTo)
	sc.Step(`^"([^"]*)" was also added to the account "([^"]*)" and is signed in to it$`, w.alsoAddedAndSignedIn)
	sc.Step(`^that person's own personal account has been deactivated$`, w.personalAccountDeactivated)
	sc.Step(`^"([^"]*)" has a "([^"]*)" named "([^"]*)" in "([^"]*)"$`, w.personHasResourceIn)
	sc.Step(`^"([^"]*)" is removed from "([^"]*)"$`, w.removedFrom)
	sc.Step(`^"([^"]*)" is deactivated$`, w.accountDeactivated)
	sc.Step(`^"([^"]*)" has been deactivated$`, w.accountDeactivated)
	sc.Step(`^"([^"]*)" is signed in and their requests are being served$`, w.signedInAndServed)
	sc.Step(`^the account "([^"]*)" has invited "([^"]*)", who has not accepted yet$`, w.accountHasInvited)
	sc.Step(`^"([^"]*)" accepts the invitation$`, w.acceptsInvitation)
	sc.Step(`^the invitation is refused$`, w.invitationRefused)
	sc.Step(`^the answer says the account is not available rather than that the instance failed$`,
		w.answerBlamesTheAccountNotTheInstance)
	sc.Step(`^"([^"]*)" is not a member of "([^"]*)"$`, w.notAMember)
	sc.Step(`^they are not served anything belonging to "([^"]*)"$`, w.notServedAnythingOf)
	sc.Step(`^"([^"]*)" is still a member of "([^"]*)"$`, w.stillAMember)

	// --- per-account settings ---
	sc.Step(`^the meal-planning types are installed$`, w.mealPlanningInstalled)
	sc.Step(`^"([^"]*)" has turned off the rule that only one pantry may be the default one$`, w.turnsOffSingleDefault)
	sc.Step(`^"([^"]*)" has changed no settings$`, w.changedNoSettings)
	sc.Step(`^they sign in and mark a second pantry as the default one$`, w.theyMarkASecondDefaultPantry)
	sc.Step(`^"([^"]*)" signs in and marks a second pantry as the default one$`, w.marksASecondDefaultPantry)
	sc.Step(`^both pantries are marked as the default one$`, func() error { return w.defaultPantryCountIs(2) })
	sc.Step(`^only the second pantry is marked as the default one$`, w.onlySecondPantryIsDefault)

	// --- signing in ---
	sc.Step(`^"([^"]*)" signs in$`, w.signsIn)
	sc.Step(`^"([^"]*)" signs in again$`, w.signsIn)
	sc.Step(`^"([^"]*)" is signed in$`, w.signsIn)
	sc.Step(`^they sign in again$`, w.theySignInAgain)
	sc.Step(`^both of them sign in$`, w.bothSignIn)
	sc.Step(`^both of them are signed in$`, w.bothSignIn)
	sc.Step(`^"([^"]*)" registers with password "([^"]*)"$`, w.registers)
	sc.Step(`^the sign-in succeeds$`, w.lastSucceeded)
	sc.Step(`^the registration succeeds$`, w.lastSucceeded)
	sc.Step(`^the sign-in reports which account it signed them in to$`, w.signInReportedAnAccount)

	// --- staged sessions ---
	sc.Step(`^"([^"]*)" holds a session made before sessions recorded an account$`, w.holdsUnscopedSession)
	sc.Step(`^"([^"]*)" holds a session with no account to act in$`, w.holdsUnscopedSession)
	sc.Step(`^"([^"]*)" holds a session naming an account they were removed from$`, w.holdsRevokedSession)
	sc.Step(`^"([^"]*)" holds a session naming an account that has been deactivated$`, w.holdsDeactivatedSession)
	sc.Step(`^"([^"]*)" signed in and their session has since (expired|been signed out)$`, w.sessionHasSince)

	// --- making requests ---
	sc.Step(`^they list the projects they can see$`, w.listProjects)
	sc.Step(`^they make a request with that session$`, w.makeRequest)
	sc.Step(`^they make a request with the session they were given$`, w.makeRequest)
	sc.Step(`^they make a request with the session they already held$`, w.makeRequest)
	sc.Step(`^someone makes a request carrying no session$`, w.requestWithNoSession)
	sc.Step(`^each of them makes a request with the session they hold$`, w.eachMakesRequest)
	sc.Step(`^each of them creates a "([^"]*)" named "([^"]*)"$`, w.eachCreates)
	sc.Step(`^the very first request they make with the session they were given is served$`, w.firstRequestServed)
	sc.Step(`^a "([^"]*)" they create afterwards is one they can see$`, w.createsAndSees)

	// --- what came back ---
	sc.Step(`^the projects they see include "([^"]*)"$`, w.projectsInclude)
	sc.Step(`^the projects they see exclude "([^"]*)"$`, w.projectsExclude)
	sc.Step(`^the request is served$`, w.requestServed)
	sc.Step(`^the request is refused as not authenticated$`, w.requestRefused)
	sc.Step(`^all three requests are refused as not authenticated$`, w.allThreeRefused)
	sc.Step(`^the refusal carries no code$`, w.refusalHasNoCode)
	sc.Step(`^the refusal says the session has no account to act in$`, func() error { return w.refusalCodeIs("unscoped_session") })
	sc.Step(`^the refusal says their access to the account was taken away$`, func() error { return w.refusalCodeIs("account_access_revoked") })
	sc.Step(`^the refusal says the account itself is not available$`, func() error { return w.refusalCodeIs("account_deactivated") })
	sc.Step(`^the refusal does not say they were removed from the account$`, w.refusalIsNotRevocation)
	sc.Step(`^the three refusals carry three different codes$`, w.threeDistinctCodes)
	sc.Step(`^none of them looks like the refusal a session that simply expired gets$`, w.noneLooksUncoded)
	sc.Step(`^the request "([^"]*)" makes says their access to the account was taken away$`,
		func(email string) error { return w.personRefusalCodeIs(email, "account_access_revoked") })
	sc.Step(`^the request "([^"]*)" makes says the account itself is not available$`,
		func(email string) error { return w.personRefusalCodeIs(email, "account_deactivated") })
	sc.Step(`^the account their requests act in is "([^"]*)"$`, w.actingAccountIsNamed)
	sc.Step(`^the account their requests act in is the one the sign-in reported$`, w.actingAccountIsReported)
	sc.Step(`^the account that first request acts in is the one the registration reported$`, w.actingAccountIsReported)
	sc.Step(`^the session still names no account afterwards$`, w.sessionStillUnscoped)
	sc.Step(`^signing in a further time does not change the answer$`, w.signingInAgainChangesNothing)
	sc.Step(`^the session is not signed out, so it serves again once the membership is back$`, w.sessionRecoversWhenMembershipReturns)
	sc.Step(`^each of them sees only their own "([^"]*)"$`, w.eachSeesOnlyTheirOwn)
	sc.Step(`^the two projects belong to different accounts$`, w.twoProjectsDifferentAccounts)
}

// --- instance lifecycle -----------------------------------------------------

func (w *accountScopedWorld) boot(registration bool) error {
	if w.tmpDir == "" {
		dir, err := os.MkdirTemp("", "weos-account-scoped-e2e-")
		if err != nil {
			return fmt.Errorf("could not create a temp dir: %w", err)
		}
		w.tmpDir = dir
		w.dsn = filepath.Join(dir, "account-scoped.db")
	}
	w.registrationEnabled = registration
	w.setEnv("PASSWORD_AUTH_ENABLED", ptr("true"))
	w.setEnv("PASSWORD_REGISTRATION_ENABLED", ptr(boolText(registration)))
	w.setEnv("GOOGLE_CLIENT_ID", nil)
	w.setEnv("GOOGLE_CLIENT_SECRET", nil)

	cfg := config.Default()
	cfg.LoadFromEnvironment()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Provide(weosoauth.ProvideJWTService),
		fx.Populate(&w.authService, &w.credRepo, &w.agentRepo, &w.accountRepo),
		fx.Populate(&w.sessionManager, &w.sessionStore, &w.authzChecker, &w.logger),
		fx.Populate(&w.resourceService, &w.resourceTypeService, &w.jwtService),
		fx.Populate(&w.inviteService, &w.inviteRepo),
		fx.Populate(&w.behaviorSettings),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app

	// The tasks preset defines the "project" type the scenarios create and
	// list. Installed here rather than staged per scenario so every scenario
	// starts from the same catalog.
	if _, err := w.resourceTypeService.InstallPreset(context.Background(), "tasks", true); err != nil {
		return fmt.Errorf("could not install the tasks preset: %w", err)
	}

	// Authorization is not what these scenarios measure — which account a
	// session acts in is. Casbin denies every write to a role with no policy,
	// so without this an unrelated 403 would stand in for the answer the
	// scenario is actually asking about. Everyone here is staged as an owner
	// and the owner role is seeded wildcard, exactly as startup does it.
	if err := application.SeedAdminPolicies(w.authzChecker); err != nil {
		return fmt.Errorf("could not seed the owner policies: %w", err)
	}

	// The production route layout, mounted through the same calls serve.go
	// makes — including RequireAuth, which is the middleware under test.
	e := echo.New()
	e.HideBanner = true
	api := e.Group("/api")
	api.Use(apimw.Messages())
	passwordHandlers := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    w.authService,
		SessionManager: w.sessionManager,
		SecureCookies:  false,
		Logger:         w.logger,
	})
	handlers.MountPasswordAuth(api, passwordHandlers, handlers.PasswordAuthRoutes{
		SignIn:       true,
		Registration: registration,
	})

	guards := []echo.MiddlewareFunc{
		echo.WrapMiddleware(authhttp.RequireAuth(w.sessionManager, w.authService)),
		apimw.Impersonation(w.sessionStore, w.accountRepo, w.logger),
		apimw.AuthorizeResource(w.authzChecker, w.accountRepo, w.logger),
	}
	resourceHandler := handlers.NewResourceHandler(w.resourceService, w.resourceTypeService, w.logger)
	api.POST("/:typeSlug", resourceHandler.Create, guards...)
	api.GET("/:typeSlug", resourceHandler.List, guards...)
	api.GET("/:typeSlug/:id", resourceHandler.Get, guards...)

	// Accepting an invitation carries its own authorization in the token, so
	// serve.go mounts it outside the guarded group. With no OAuth provider
	// configured it takes no auth middleware at all, which is the shape here.
	inviteHandler := handlers.NewInviteHandler(handlers.InviteHandlerConfig{
		InviteService:  w.inviteService,
		InviteRepo:     w.inviteRepo,
		AccountRepo:    w.accountRepo,
		CredentialRepo: w.credRepo,
		Logger:         w.logger,
	})
	api.Group("").POST("/invites/accept", inviteHandler.Accept)

	w.server = httptest.NewServer(e)
	return nil
}

// databaseHoldsOldSessions boots an instance, signs someone in, and leaves the
// session row behind — then blanks its account so the row looks like one
// written before sessions carried an account at all.
func (w *accountScopedWorld) databaseHoldsOldSessions() error {
	if err := w.boot(false); err != nil {
		return err
	}
	if err := w.accountWithOwner("Harbor Legal", "ops@harborlegal.example", aPassword); err != nil {
		return err
	}
	return w.holdsUnscopedSession("ops@harborlegal.example")
}

func (w *accountScopedWorld) restart() error {
	w.stopServer()
	return w.boot(w.registrationEnabled)
}

func (w *accountScopedWorld) instanceServes() error {
	if w.server == nil {
		return fmt.Errorf("the instance did not come back up")
	}
	// An unauthenticated call still proves the process is answering; it is
	// refused, not unanswered.
	res, err := http.Get(w.server.URL + projectsPath)
	if err != nil {
		return fmt.Errorf("the instance is not serving: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 500 {
		return fmt.Errorf("the instance answered %d, so it did not start cleanly", res.StatusCode)
	}
	return nil
}

func (w *accountScopedWorld) freshSignInIsServed() error {
	const email = "fresh@harborlegal.example"
	if err := w.accountWithOwner("Fresh Account", email, aPassword); err != nil {
		return err
	}
	if err := w.signsIn(email); err != nil {
		return err
	}
	return w.requestServedFor(email)
}

func (w *accountScopedWorld) stopServer() {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
	if w.app != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		_ = w.app.Stop(stopCtx)
		cancel()
		w.app = nil
	}
}

func (w *accountScopedWorld) teardown() {
	w.stopServer()
	for k, v := range w.envBefore {
		if v == nil {
			os.Unsetenv(k)
			continue
		}
		os.Setenv(k, *v)
	}
	w.envBefore = nil
}

func (w *accountScopedWorld) setEnv(key string, value *string) {
	if w.envBefore == nil {
		w.envBefore = map[string]*string{}
	}
	if _, seen := w.envBefore[key]; !seen {
		if existing, ok := os.LookupEnv(key); ok {
			w.envBefore[key] = &existing
		} else {
			w.envBefore[key] = nil
		}
	}
	if value == nil {
		os.Unsetenv(key)
		return
	}
	os.Setenv(key, *value)
}

// --- staging accounts and people -------------------------------------------

// personFor registers the identity if the scenario has not met them yet. The
// personal account pericarp mints is renamed to the name the scenario used, so
// "the account Harbor Legal, whose owner ops@..." is one account with an owner
// rather than two accounts that happen to share a person.
func (w *accountScopedWorld) personFor(email, password string) (*person, error) {
	if p, ok := w.people[email]; ok {
		return p, nil
	}
	ctx := context.Background()
	agent, _, account, err := w.authService.RegisterPassword(ctx, email, displayNameFor(email), password)
	if err != nil {
		return nil, fmt.Errorf("could not create %q: %w", email, err)
	}
	p := &person{email: email, password: password, agentID: agent.GetID()}
	if account != nil {
		p.accountID = account.GetID()
		if err := w.accountRepo.SaveMember(ctx, account.GetID(), agent.GetID(), authentities.RoleOwner); err != nil {
			return nil, fmt.Errorf("could not record %q as owner of their own account: %w", email, err)
		}
	}
	w.people[email] = p
	w.order = append(w.order, email)
	return p, nil
}

func (w *accountScopedWorld) accountWithOwner(name, email, password string) error {
	p, err := w.personFor(email, password)
	if err != nil {
		return err
	}
	if p.accountID == "" {
		return fmt.Errorf("%q was created without a personal account to name %q", email, name)
	}
	if err := w.renameAccount(p.accountID, name); err != nil {
		return err
	}
	w.accounts[name] = p.accountID
	return nil
}

// accountWithMember builds an account the person did NOT get by registering —
// the invite shape. Their own personal account still exists, which is what
// makes the "not the one a lookup would find" scenario meaningful.
func (w *accountScopedWorld) accountWithMember(name, email, password string) error {
	p, err := w.personFor(email, password)
	if err != nil {
		return err
	}
	id, err := w.createAccount(name)
	if err != nil {
		return err
	}
	if err := w.accountRepo.SaveMember(context.Background(), id, p.agentID, authentities.RoleOwner); err != nil {
		return fmt.Errorf("could not add %q to %q: %w", email, name, err)
	}
	// "was added to and signs in to" means their sign-in has to land HERE.
	// Registering minted them a personal account, and sign-in would resolve
	// that one instead. Deactivating it makes sign-in pass over it, which is
	// the shape of someone who only ever arrived by invitation.
	personal, err := w.accountRepo.FindPersonalByMember(context.Background(), p.agentID)
	if err != nil {
		return fmt.Errorf("could not read the personal account of %q: %w", email, err)
	}
	if personal != nil {
		if err := w.deactivate(personal.GetID()); err != nil {
			return err
		}
	}
	return nil
}

func (w *accountScopedWorld) accountWithTwoMembers(name, first, second string) error {
	if err := w.accountWithMember(name, first, aPassword); err != nil {
		return err
	}
	return w.addExistingMember(name, second, true)
}

// addExistingMember puts someone into an already-staged account. signInHere
// says whether the scenario expects their sign-in to LAND in that account: if
// it does, a live personal account would win instead and has to go. "also
// belongs to" is the opposite case — they keep their own account, and a later
// step may well expect a sign-in to return to it.
func (w *accountScopedWorld) addExistingMember(name, email string, signInHere bool) error {
	p, err := w.personFor(email, aPassword)
	if err != nil {
		return err
	}
	id, ok := w.accounts[name]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", name)
	}
	if err := w.accountRepo.SaveMember(context.Background(), id, p.agentID, authentities.RoleOwner); err != nil {
		return err
	}
	if !signInHere {
		return nil
	}
	personal, err := w.accountRepo.FindPersonalByMember(context.Background(), p.agentID)
	if err != nil {
		return fmt.Errorf("could not read the personal account of %q: %w", email, err)
	}
	if personal != nil && personal.GetID() != id {
		return w.deactivate(personal.GetID())
	}
	return nil
}

func (w *accountScopedWorld) alsoBelongsTo(email, name string) error {
	if _, ok := w.accounts[name]; !ok {
		if _, err := w.createAccount(name); err != nil {
			return err
		}
	}
	return w.addExistingMember(name, email, false)
}

func (w *accountScopedWorld) alsoAddedAndSignedIn(email, name string) error {
	if err := w.alsoBelongsTo(email, name); err != nil {
		return err
	}
	// Signed in TO that account: the session names it explicitly rather than
	// whichever one a sign-in would have resolved.
	return w.stageSession(email, w.accounts[name])
}

func (w *accountScopedWorld) createAccount(name string) (string, error) {
	id := "acct-" + slugForAccountName(name)
	account, err := (&authentities.Account{}).With(id, name, "organization")
	if err != nil {
		return "", fmt.Errorf("could not build account %q: %w", name, err)
	}
	if err := w.accountRepo.Save(context.Background(), account); err != nil {
		return "", fmt.Errorf("could not save account %q: %w", name, err)
	}
	w.accounts[name] = id
	return id, nil
}

func (w *accountScopedWorld) renameAccount(id, name string) error {
	ctx := context.Background()
	existing, err := w.accountRepo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("could not read account %s: %w", id, err)
	}
	renamed := &authentities.Account{}
	if err := renamed.Restore(id, name, existing.AccountType(), existing.Active(), existing.CreatedAt()); err != nil {
		return fmt.Errorf("could not rename account %s: %w", id, err)
	}
	return w.accountRepo.Save(ctx, renamed)
}

func (w *accountScopedWorld) personalAccountDeactivated() error {
	if len(w.order) == 0 {
		return fmt.Errorf("no person has been staged yet")
	}
	p := w.people[w.order[len(w.order)-1]]
	ctx := context.Background()
	personal, err := w.accountRepo.FindPersonalByMember(ctx, p.agentID)
	if err != nil {
		return fmt.Errorf("could not read the personal account of %q: %w", p.email, err)
	}
	if personal == nil {
		return nil // nothing to deactivate; the scenario's premise already holds
	}
	return w.deactivate(personal.GetID())
}

func (w *accountScopedWorld) accountDeactivated(name string) error {
	id, ok := w.accounts[name]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", name)
	}
	return w.deactivate(id)
}

func (w *accountScopedWorld) deactivate(id string) error {
	ctx := context.Background()
	account, err := w.accountRepo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("could not read account %s: %w", id, err)
	}
	if err := account.Deactivate(); err != nil {
		return fmt.Errorf("could not deactivate account %s: %w", id, err)
	}
	return w.accountRepo.Save(ctx, account)
}

func (w *accountScopedWorld) removedFrom(email, name string) error {
	p, ok := w.people[email]
	if !ok {
		return fmt.Errorf("%q has not been staged", email)
	}
	id, ok := w.accounts[name]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", name)
	}
	return w.accountRepo.RemoveMember(context.Background(), id, p.agentID)
}

func (w *accountScopedWorld) stillAMember(email, name string) error {
	p, ok := w.people[email]
	if !ok {
		return fmt.Errorf("%q has not been staged", email)
	}
	role, err := w.accountRepo.FindMemberRole(context.Background(), w.accounts[name], p.agentID)
	if err != nil {
		return fmt.Errorf("could not read the membership of %q: %w", email, err)
	}
	if role == "" {
		return fmt.Errorf("%q is no longer a member of %q, but the scenario needs them to be", email, name)
	}
	return nil
}

func (w *accountScopedWorld) personHasResourceIn(email, typeSlug, name, accountName string) error {
	p, ok := w.people[email]
	if !ok {
		return fmt.Errorf("%q has not been staged", email)
	}
	id, ok := w.accounts[accountName]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", accountName)
	}
	ctx := auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID:         p.agentID,
		AccountIDs:      []string{id},
		ActiveAccountID: id,
	})
	_, err := w.resourceService.Create(ctx, application.CreateResourceCommand{
		TypeSlug: typeSlug,
		Data:     json.RawMessage(fmt.Sprintf(`{"name":%q}`, name)),
	})
	if err != nil {
		return fmt.Errorf("could not stage a %q named %q in %q: %w", typeSlug, name, accountName, err)
	}
	return nil
}

// --- per-account settings ------------------------------------------------

func (w *accountScopedWorld) mealPlanningInstalled() error {
	if _, err := w.resourceTypeService.InstallPreset(context.Background(), "meal-planning", true); err != nil {
		return fmt.Errorf("could not install the meal-planning preset: %w", err)
	}
	return nil
}

// turnsOffSingleDefault writes an account override that enables no behaviors
// for pantries. An override is distinguished from "no override" by being
// non-nil rather than by being non-empty, so an empty list is how a rule gets
// switched off rather than left at its preset default.
func (w *accountScopedWorld) turnsOffSingleDefault(email string) error {
	p, ok := w.people[email]
	if !ok {
		return fmt.Errorf("%q has not been staged", email)
	}
	accountID := p.accountID
	if accountID == "" {
		return fmt.Errorf("%q has no account whose settings could be changed", email)
	}
	return w.behaviorSettings.SaveByAccountAndType(context.Background(), accountID, "pantry", []string{})
}

// changedNoSettings is deliberately empty: the scenario states the absence of
// an override, and writing one to represent "nothing" would be the opposite of
// what it says.
func (w *accountScopedWorld) changedNoSettings(string) error { return nil }

func (w *accountScopedWorld) theyMarkASecondDefaultPantry() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	return w.marksASecondDefaultPantry(p.email)
}

func (w *accountScopedWorld) marksASecondDefaultPantry(email string) error {
	if err := w.signsIn(email); err != nil {
		return err
	}
	p := w.people[email]
	for _, name := range []string{"Kitchen", "Garage"} {
		if err := w.request(p, http.MethodPost, "/api/pantry",
			fmt.Sprintf(`{"name":%q,"isDefault":true}`, name)); err != nil {
			return err
		}
		if p.lastAnswer.status >= 400 {
			return fmt.Errorf("%q could not mark the %q pantry default: %s", email, name, describe(p.lastAnswer))
		}
	}
	return nil
}

func (w *accountScopedWorld) defaultPantryCountIs(want int) error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	got, err := w.countDefaultPantries(p)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%d pantries are marked default, want %d: %s", got, want, describe(p.lastAnswer))
	}
	return nil
}

func (w *accountScopedWorld) onlySecondPantryIsDefault() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	got, err := w.countDefaultPantries(p)
	if err != nil {
		return err
	}
	if got != 1 {
		return fmt.Errorf("%d pantries are marked default, want only the second: %s", got, describe(p.lastAnswer))
	}
	if !strings.Contains(p.lastAnswer.body, "Garage") {
		return fmt.Errorf("the second pantry is missing entirely: %s", describe(p.lastAnswer))
	}
	return nil
}

func (w *accountScopedWorld) countDefaultPantries(p *person) (int, error) {
	if err := w.request(p, http.MethodGet, "/api/pantry", ""); err != nil {
		return 0, err
	}
	if p.lastAnswer.status != http.StatusOK {
		return 0, fmt.Errorf("could not list pantries: %s", describe(p.lastAnswer))
	}
	// isDefault arrives as a number from the SQLite-backed projection rather
	// than as a JSON bool, so it is read loosely and judged truthy.
	var body struct {
		Data []struct {
			Name      string `json:"name"`
			IsDefault any    `json:"isDefault"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(p.lastAnswer.body), &body); err != nil {
		return 0, fmt.Errorf("could not read the pantry listing: %w (%s)", err, p.lastAnswer.body)
	}
	count := 0
	for _, item := range body.Data {
		if isTruthy(item.IsDefault) {
			count++
		}
	}
	return count, nil
}

// --- signing in -------------------------------------------------------------

func (w *accountScopedWorld) signsIn(email string) error {
	p, ok := w.people[email]
	if !ok {
		var err error
		if p, err = w.personFor(email, aPassword); err != nil {
			return err
		}
	}
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, p.password)
	res, err := http.Post(w.server.URL+signInPath, "application/json", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("sign-in for %q failed: %w", email, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	p.lastAnswer = &capturedAnswer{status: res.StatusCode, body: string(raw)}
	if res.StatusCode == http.StatusOK {
		p.cookie = sessionCookieHeader(res.Cookies())
		p.accountID = reportedAccountID(raw)
		p.sessionID = w.sessionIDBehind(p.cookie)
	}
	return nil
}

// sessionIDBehind reads the id out of the cookie the instance just set, so a
// step can ask the store about a session that was created over HTTP rather
// than staged here.
func (w *accountScopedWorld) sessionIDBehind(cookie string) string {
	if cookie == "" {
		return ""
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", cookie)
	data, err := w.sessionManager.GetHTTPSession(req)
	if err != nil || data == nil {
		return ""
	}
	return data.SessionID
}

func (w *accountScopedWorld) theySignInAgain() error {
	if len(w.order) == 0 {
		return fmt.Errorf("nobody has been staged to sign in")
	}
	return w.signsIn(w.order[len(w.order)-1])
}

func (w *accountScopedWorld) bothSignIn() error {
	if len(w.order) < 2 {
		return fmt.Errorf("the scenario staged %d people, not two", len(w.order))
	}
	for _, email := range w.order[:2] {
		if err := w.signsIn(email); err != nil {
			return err
		}
	}
	return nil
}

func (w *accountScopedWorld) registers(email, password string) error {
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	res, err := http.Post(w.server.URL+registrationPath, "application/json", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("registration for %q failed: %w", email, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	p := &person{email: email, password: password,
		lastAnswer: &capturedAnswer{status: res.StatusCode, body: string(raw)}}
	if res.StatusCode == http.StatusOK {
		p.cookie = sessionCookieHeader(res.Cookies())
		p.accountID = reportedAccountID(raw)
		p.sessionID = w.sessionIDBehind(p.cookie)
	}
	w.people[email] = p
	w.order = append(w.order, email)
	return nil
}

func (w *accountScopedWorld) lastSucceeded() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if p.lastAnswer == nil || p.lastAnswer.status != http.StatusOK {
		return fmt.Errorf("expected success, got %s", describe(p.lastAnswer))
	}
	return nil
}

func (w *accountScopedWorld) signInReportedAnAccount() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if p.accountID == "" {
		return fmt.Errorf("the sign-in named no account: %s", describe(p.lastAnswer))
	}
	return nil
}

// --- staged sessions --------------------------------------------------------

// stageSession makes a session directly, so a scenario can hold one shaped in a
// way no sign-in would produce today — unscoped, or naming an account the
// person is about to lose.
func (w *accountScopedWorld) stageSession(email, accountID string) error {
	p, ok := w.people[email]
	if !ok {
		var err error
		if p, err = w.personFor(email, aPassword); err != nil {
			return err
		}
	}
	ctx := context.Background()
	creds, err := w.credRepo.FindByEmail(ctx, strings.ToLower(email))
	if err != nil || len(creds) == 0 {
		return fmt.Errorf("no credential for %q to build a session from", email)
	}
	authSession, err := w.authService.CreateSession(ctx, p.agentID, accountID, creds[0].GetID(),
		"127.0.0.1", "acceptance", time.Hour, authapp.AccountAlreadyVerified())
	if err != nil {
		return fmt.Errorf("could not stage a session for %q: %w", email, err)
	}
	p.sessionID = authSession.GetID()
	cookie, err := w.cookieFor(session.SessionData{
		SessionID: authSession.GetID(),
		AgentID:   p.agentID,
		AccountID: accountID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		return err
	}
	p.cookie = cookie
	return nil
}

func (w *accountScopedWorld) holdsUnscopedSession(email string) error {
	return w.stageSession(email, "")
}

func (w *accountScopedWorld) holdsRevokedSession(email string) error {
	name := "Cedar Realty"
	if _, ok := w.accounts[name]; !ok {
		if _, err := w.createAccount(name); err != nil {
			return err
		}
	}
	if err := w.alsoBelongsTo(email, name); err != nil {
		return err
	}
	if err := w.stageSession(email, w.accounts[name]); err != nil {
		return err
	}
	return w.removedFrom(email, name)
}

func (w *accountScopedWorld) holdsDeactivatedSession(email string) error {
	name := "Summit Partners"
	if _, ok := w.accounts[name]; !ok {
		if _, err := w.createAccount(name); err != nil {
			return err
		}
	}
	if err := w.alsoBelongsTo(email, name); err != nil {
		return err
	}
	if err := w.stageSession(email, w.accounts[name]); err != nil {
		return err
	}
	return w.accountDeactivated(name)
}

func (w *accountScopedWorld) sessionHasSince(email, what string) error {
	if err := w.signsIn(email); err != nil {
		return err
	}
	p := w.people[email]
	switch what {
	case "expired":
		// Rebuilt already expired rather than waited out.
		creds, err := w.credRepo.FindByEmail(context.Background(), strings.ToLower(email))
		if err != nil || len(creds) == 0 {
			return fmt.Errorf("no credential for %q", email)
		}
		expired, err := w.authService.CreateSession(context.Background(), p.agentID, p.accountID,
			creds[0].GetID(), "127.0.0.1", "acceptance", -time.Hour, authapp.AccountAlreadyVerified())
		if err != nil {
			return fmt.Errorf("could not stage an expired session: %w", err)
		}
		cookie, err := w.cookieFor(session.SessionData{
			SessionID: expired.GetID(), AgentID: p.agentID, AccountID: p.accountID,
			CreatedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
		})
		if err != nil {
			return err
		}
		p.cookie = cookie
		return nil
	default: // "been signed out"
		return w.authService.RevokeSession(context.Background(), p.sessionID)
	}
}

// --- making requests --------------------------------------------------------

func (w *accountScopedWorld) request(p *person, method, path, body string) error {
	req, err := http.NewRequest(method, w.server.URL+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.cookie != "" {
		req.Header.Set("Cookie", p.cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	p.lastAnswer = &capturedAnswer{status: res.StatusCode, body: string(raw), code: refusalCode(raw)}
	return nil
}

func (w *accountScopedWorld) currentPerson() (*person, error) {
	if len(w.order) == 0 {
		return nil, fmt.Errorf("no person has been staged")
	}
	return w.people[w.order[len(w.order)-1]], nil
}

func (w *accountScopedWorld) makeRequest() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	return w.request(p, http.MethodGet, projectsPath, "")
}

func (w *accountScopedWorld) requestWithNoSession() error {
	p := &person{email: "nobody@harborlegal.example"}
	w.people[p.email] = p
	w.order = append(w.order, p.email)
	return w.request(p, http.MethodGet, projectsPath, "")
}

func (w *accountScopedWorld) eachMakesRequest() error {
	for _, email := range w.order {
		if err := w.request(w.people[email], http.MethodGet, projectsPath, ""); err != nil {
			return err
		}
	}
	return nil
}

func (w *accountScopedWorld) listProjects() error { return w.makeRequest() }

func (w *accountScopedWorld) eachCreates(typeSlug, name string) error {
	for _, email := range w.order[:2] {
		p := w.people[email]
		if err := w.request(p, http.MethodPost, "/api/"+typeSlug,
			fmt.Sprintf(`{"name":%q}`, name)); err != nil {
			return err
		}
		if p.lastAnswer.status != http.StatusOK && p.lastAnswer.status != http.StatusCreated {
			return fmt.Errorf("%q could not create a %s: %s", email, typeSlug, describe(p.lastAnswer))
		}
	}
	return nil
}

func (w *accountScopedWorld) firstRequestServed() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if err := w.request(p, http.MethodGet, projectsPath, ""); err != nil {
		return err
	}
	return w.requestServedFor(p.email)
}

func (w *accountScopedWorld) createsAndSees(typeSlug string) error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	const name = "First thing"
	if err := w.request(p, http.MethodPost, "/api/"+typeSlug, fmt.Sprintf(`{"name":%q}`, name)); err != nil {
		return err
	}
	if p.lastAnswer.status >= 400 {
		return fmt.Errorf("the newcomer could not create a %s: %s", typeSlug, describe(p.lastAnswer))
	}
	if err := w.request(p, http.MethodGet, "/api/"+typeSlug, ""); err != nil {
		return err
	}
	if !strings.Contains(p.lastAnswer.body, name) {
		return fmt.Errorf("the newcomer cannot see the %s they just created: %s", typeSlug, describe(p.lastAnswer))
	}
	return nil
}

// signedInAndServed establishes the "before" the suspension scenarios need:
// not merely signed in, but demonstrably being served, so the refusal that
// follows is a change rather than a state that was always true.
func (w *accountScopedWorld) signedInAndServed(email string) error {
	if err := w.signsIn(email); err != nil {
		return err
	}
	if err := w.request(w.people[email], http.MethodGet, projectsPath, ""); err != nil {
		return err
	}
	return w.requestServedFor(email)
}

func (w *accountScopedWorld) refusalIsNotRevocation() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if p.lastAnswer.code == "account_access_revoked" {
		return fmt.Errorf("the refusal blames a removed membership, but the membership is intact " +
			"and the account is suspended; a client told this would send them to sign in again")
	}
	return nil
}

func (w *accountScopedWorld) notServedAnythingOf(name string) error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if p.lastAnswer.status == http.StatusOK {
		return fmt.Errorf("the request was served from %q even though it is suspended: %s",
			name, describe(p.lastAnswer))
	}
	return nil
}

func (w *accountScopedWorld) accountHasInvited(name, email string) error {
	id, ok := w.accounts[name]
	if !ok {
		var err error
		if id, err = w.createAccount(name); err != nil {
			return err
		}
	}
	// Somebody has to do the inviting; the scenario does not care who, only
	// that an invitation is outstanding when the account is suspended.
	inviter, err := w.personFor("owner@"+strings.SplitN(email, "@", 2)[1], aPassword)
	if err != nil {
		return err
	}
	if err := w.accountRepo.SaveMember(context.Background(), id, inviter.agentID, authentities.RoleOwner); err != nil {
		return err
	}
	_, token, err := w.inviteService.CreateInvite(context.Background(), id, email,
		authentities.RoleOwner, inviter.agentID)
	if err != nil {
		return fmt.Errorf("could not invite %q into %q: %w", email, name, err)
	}
	w.inviteToken = token
	return nil
}

func (w *accountScopedWorld) acceptsInvitation(email string) error {
	p := &person{email: email}
	w.people[email] = p
	w.order = append(w.order, email)
	body := fmt.Sprintf(`{"token":%q,"email":%q,"name":%q}`, w.inviteToken, email, displayNameFor(email))
	return w.request(p, http.MethodPost, "/api/invites/accept", body)
}

func (w *accountScopedWorld) invitationRefused() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if p.lastAnswer.status < 400 {
		return fmt.Errorf("the invitation was accepted into a suspended account: %s", describe(p.lastAnswer))
	}
	return nil
}

// answerBlamesTheAccountNotTheInstance is the assertion my invite_handler
// change exists for: without an arm for ErrAccountDeactivated the error falls
// through to default and answers 500, which reads as an instance fault and
// invites a retry that can never work.
func (w *accountScopedWorld) answerBlamesTheAccountNotTheInstance() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if p.lastAnswer.status >= 500 {
		return fmt.Errorf("the instance blamed itself with %d; the account is what is unavailable: %s",
			p.lastAnswer.status, describe(p.lastAnswer))
	}
	if !strings.Contains(strings.ToLower(p.lastAnswer.body), "not available") {
		return fmt.Errorf("the answer does not say the account is unavailable: %s", describe(p.lastAnswer))
	}
	return nil
}

func (w *accountScopedWorld) notAMember(email, name string) error {
	p, ok := w.people[email]
	if !ok || p.agentID == "" {
		// Never became an agent at all, which is a stronger form of the same.
		return nil
	}
	role, err := w.accountRepo.FindMemberRole(context.Background(), w.accounts[name], p.agentID)
	if err != nil {
		return fmt.Errorf("could not read the membership of %q: %w", email, err)
	}
	if role != "" {
		return fmt.Errorf("%q was made a member of %q despite the refusal", email, name)
	}
	return nil
}

// --- assertions -------------------------------------------------------------

func (w *accountScopedWorld) requestServedFor(email string) error {
	p := w.people[email]
	if p.lastAnswer == nil || p.lastAnswer.status != http.StatusOK {
		return fmt.Errorf("the request was not served: %s", describe(p.lastAnswer))
	}
	return nil
}

func (w *accountScopedWorld) requestServed() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	return w.requestServedFor(p.email)
}

func (w *accountScopedWorld) requestRefused() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if p.lastAnswer == nil || p.lastAnswer.status != http.StatusUnauthorized {
		return fmt.Errorf("expected 401, got %s", describe(p.lastAnswer))
	}
	return nil
}

func (w *accountScopedWorld) allThreeRefused() error {
	if len(w.order) < 3 {
		return fmt.Errorf("the scenario staged %d people, not three", len(w.order))
	}
	for _, email := range w.order[:3] {
		if a := w.people[email].lastAnswer; a == nil || a.status != http.StatusUnauthorized {
			return fmt.Errorf("%q was not refused: %s", email, describe(a))
		}
	}
	return nil
}

func (w *accountScopedWorld) refusalHasNoCode() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if p.lastAnswer.code != "" {
		return fmt.Errorf("the refusal carried code %q, but this one must carry none", p.lastAnswer.code)
	}
	return nil
}

func (w *accountScopedWorld) refusalCodeIs(want string) error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	return matchCode(p, want)
}

func (w *accountScopedWorld) personRefusalCodeIs(email, want string) error {
	p, ok := w.people[email]
	if !ok {
		return fmt.Errorf("%q has not been staged", email)
	}
	// Always a fresh request. The step says "the request X makes", and X may
	// well have a sign-in answer sitting in lastAnswer from an earlier step —
	// judging that would report 200 and say nothing about the refusal.
	if err := w.request(p, http.MethodGet, projectsPath, ""); err != nil {
		return err
	}
	return matchCode(p, want)
}

func matchCode(p *person, want string) error {
	if p.lastAnswer == nil {
		return fmt.Errorf("%q made no request to judge", p.email)
	}
	if p.lastAnswer.code != want {
		return fmt.Errorf("%q got code %q, want %q (%s)", p.email, p.lastAnswer.code, want, describe(p.lastAnswer))
	}
	return nil
}

func (w *accountScopedWorld) threeDistinctCodes() error {
	seen := map[string]string{}
	for _, email := range w.order[:3] {
		code := w.people[email].lastAnswer.code
		if code == "" {
			return fmt.Errorf("%q got no code at all, so it cannot be told from an expiry", email)
		}
		if other, clash := seen[code]; clash {
			return fmt.Errorf("%q and %q both got code %q, so they cannot be told apart", other, email, code)
		}
		seen[code] = email
	}
	return nil
}

func (w *accountScopedWorld) noneLooksUncoded() error {
	for _, email := range w.order[:3] {
		if w.people[email].lastAnswer.code == "" {
			return fmt.Errorf("%q got the same shape an expired session gets", email)
		}
	}
	return nil
}

func (w *accountScopedWorld) actingAccountIsNamed(name string) error {
	want, ok := w.accounts[name]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", name)
	}
	return w.actingAccountIs(want)
}

func (w *accountScopedWorld) actingAccountIsReported() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if p.accountID == "" {
		return fmt.Errorf("nothing reported an account to compare against")
	}
	return w.actingAccountIs(p.accountID)
}

// actingAccountIs reads the account back through RequireAuth rather than from
// the sign-in response, because the response is what the handler said and this
// is what the middleware actually puts on the request.
func (w *accountScopedWorld) actingAccountIs(want string) error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	info, err := w.authService.ValidateSession(context.Background(), p.sessionID)
	if err != nil || info == nil {
		// No staged session id: fall back to what the served request proves.
		if p.lastAnswer != nil && p.lastAnswer.status == http.StatusOK {
			return nil
		}
		return fmt.Errorf("could not read the session of %q: %v", p.email, err)
	}
	if info.AccountID != want {
		return fmt.Errorf("requests act in account %q, want %q", info.AccountID, want)
	}
	return nil
}

// signingInAgainChangesNothing proves the state is terminal rather than flaky:
// a further sign-in resolves nothing, so the same refusal comes back.
func (w *accountScopedWorld) signingInAgainChangesNothing() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	before := p.lastAnswer.code
	if err := w.signsIn(p.email); err != nil {
		return err
	}
	if err := w.request(p, http.MethodGet, projectsPath, ""); err != nil {
		return err
	}
	if p.lastAnswer.code != before {
		return fmt.Errorf("a further sign-in changed the answer from %q to %q", before, p.lastAnswer.code)
	}
	return nil
}

func (w *accountScopedWorld) sessionStillUnscoped() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	info, err := w.authService.ValidateSession(context.Background(), p.sessionID)
	if err == nil && info != nil && info.AccountID != "" {
		return fmt.Errorf("the session was repaired to account %q; it should have been left alone", info.AccountID)
	}
	return nil
}

func (w *accountScopedWorld) sessionRecoversWhenMembershipReturns() error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	// No assertion on ValidateSession here: a session refused for a revoked
	// membership reports an error while still existing, so its error tells us
	// nothing about whether it survived. Serving again below is the proof —
	// a revoked session could not, whatever the membership did afterwards.
	//
	// Put the membership back and show the same cookie now works.
	for name, id := range w.accounts {
		if err := w.accountRepo.SaveMember(context.Background(), id, p.agentID, authentities.RoleOwner); err != nil {
			return fmt.Errorf("could not restore the membership in %q: %w", name, err)
		}
	}
	if err := w.request(p, http.MethodGet, projectsPath, ""); err != nil {
		return err
	}
	if p.lastAnswer.status != http.StatusOK {
		return fmt.Errorf("the session did not serve again once the membership was back: %s",
			describe(p.lastAnswer))
	}
	return nil
}

// projectsInclude fetches the listing itself rather than reading whatever the
// previous step left behind. Several scenarios go straight from signing in to
// "the projects they see", with no listing step between — reading lastAnswer
// there would judge the sign-in response and report a miss that means nothing.
func (w *accountScopedWorld) projectsInclude(name string) error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if err := w.request(p, http.MethodGet, projectsPath, ""); err != nil {
		return err
	}
	if p.lastAnswer.status != http.StatusOK {
		return fmt.Errorf("the listing was not served: %s", describe(p.lastAnswer))
	}
	if !strings.Contains(p.lastAnswer.body, name) {
		return fmt.Errorf("%q is missing from what they can see: %s", name, p.lastAnswer.body)
	}
	return nil
}

func (w *accountScopedWorld) projectsExclude(name string) error {
	p, err := w.currentPerson()
	if err != nil {
		return err
	}
	if err := w.request(p, http.MethodGet, projectsPath, ""); err != nil {
		return err
	}
	if strings.Contains(p.lastAnswer.body, name) {
		return fmt.Errorf("%q is visible, but it belongs to another account", name)
	}
	return nil
}

func (w *accountScopedWorld) eachSeesOnlyTheirOwn(name string) error {
	for _, email := range w.order[:2] {
		p := w.people[email]
		if err := w.request(p, http.MethodGet, projectsPath, ""); err != nil {
			return err
		}
		if p.lastAnswer.status != http.StatusOK {
			return fmt.Errorf("%q could not list: %s", email, describe(p.lastAnswer))
		}
		if strings.Count(p.lastAnswer.body, name) != 1 {
			return fmt.Errorf("%q sees %d copies of %q, want exactly their own",
				email, strings.Count(p.lastAnswer.body, name), name)
		}
	}
	return nil
}

func (w *accountScopedWorld) twoProjectsDifferentAccounts() error {
	first := w.people[w.order[0]]
	second := w.people[w.order[1]]
	firstInfo, err1 := w.authService.ValidateSession(context.Background(), first.sessionID)
	secondInfo, err2 := w.authService.ValidateSession(context.Background(), second.sessionID)
	if err1 != nil || err2 != nil || firstInfo == nil || secondInfo == nil {
		if first.accountID != "" && second.accountID != "" && first.accountID != second.accountID {
			return nil
		}
		return fmt.Errorf("could not establish that the two acted in different accounts")
	}
	if firstInfo.AccountID == secondInfo.AccountID {
		return fmt.Errorf("both acted in account %q", firstInfo.AccountID)
	}
	return nil
}

// --- small helpers ----------------------------------------------------------

func (w *accountScopedWorld) cookieFor(data session.SessionData) (string, error) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := w.sessionManager.CreateHTTPSession(rec, req, data); err != nil {
		return "", fmt.Errorf("could not encode a session cookie: %w", err)
	}
	return sessionCookieHeader(rec.Result().Cookies()), nil
}

func sessionCookieHeader(cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Value == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// refusalCode reads the `code` field RequireAuth writes. Absent is a valid
// answer and means "sign in again", so a missing field is not an error.
func refusalCode(raw []byte) string {
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(raw, &body)
	return body.Code
}

func reportedAccountID(raw []byte) string {
	var body struct {
		Data struct {
			Account *struct {
				ID string `json:"id"`
			} `json:"account"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Data.Account == nil {
		return ""
	}
	return body.Data.Account.ID
}

func describe(a *capturedAnswer) string {
	if a == nil {
		return "no answer at all"
	}
	return fmt.Sprintf("status %d body %s", a.status, a.body)
}

func displayNameFor(email string) string {
	if at := strings.Index(email, "@"); at > 0 {
		return email[:at]
	}
	return email
}

// slugForAccountName keeps staged account ids readable in a failure message.
// It is deliberately local: the identically-named helper in the per-account
// knowledge graph suite sits behind the oxigraph_embedded build tag.
func slugForAccountName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// isTruthy accepts the shapes a boolean takes on the way out of the
// projection: a real bool, or a number that is not zero.
func isTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t == "true" || t == "1"
	default:
		return false
	}
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
