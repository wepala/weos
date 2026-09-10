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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/domain/services"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/cucumber/godog"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	gormlib "gorm.io/gorm"
)

// TestAccountDeletion drives the scenarios that let a person delete their
// account from the app, and that decide what remains — nothing — and what
// the people around them meet afterwards.
//
// It composes three instances the other suites already know how to boot: the
// session-authenticated instance of account_scoped_sessions (with story 2's
// upload routes), and the demo instance of oauth_authorize_session for the
// connector scenarios. Which one a scenario runs against is decided by its
// first Given.
//
// Three tags are excluded, each for a reason stated on the scenario itself:
//
//	@wip                      the contract is promoted by the local gate
//	@requires-embedded-graph  needs -tags oxigraph_embedded and its static library
//	@unit-pinned              pinned by unit tests the suite cannot stage over HTTP
//
// Everything else runs, and godog runs Strict, so an unimplemented step fails
// the suite rather than passing quietly.
func TestAccountDeletion(t *testing.T) {
	tags := "~@wip && ~@requires-embedded-graph && ~@unit-pinned"
	if v := os.Getenv("GODOG_TAGS"); v != "" {
		tags = v
	}
	suite := godog.TestSuite{
		ScenarioInitializer: initAccountDeletionScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/account_deletion.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("account deletion acceptance scenarios failed")
	}
}

const (
	accountPath       = "/api/account"
	accountExportPath = "/api/account/export"
	mePath            = "/api/auth/me"
	impersonatePath   = "/api/admin/impersonate"
	// grantableFeature is declared on the instance so "a feature grant" is a
	// real row of a feature the instance knows.
	grantableFeature = "recipe-export"
)

// onceFailingFileService stands in for the external store the suite can make
// fail: it refuses the next folder removal it is asked for, once, and behaves
// like the real backend otherwise.
type onceFailingFileService struct {
	services.FileService
	refuseNext bool
	refused    int
}

func (f *onceFailingFileService) DeleteAccountFolder(ctx context.Context, accountID string) error {
	if f.refuseNext {
		f.refuseNext = false
		f.refused++
		return errors.New("the file store refused to remove the folder")
	}
	return f.FileService.DeleteAccountFolder(ctx, accountID)
}

// notedIdentifiers is everything an account was known by before it was
// deleted, recorded so "nothing remains" can query every store by them rather
// than ask the deletion what it removed.
type notedIdentifiers struct {
	accountID    string
	resourceURNs []string
	eventIDs     []string
	agentIDs     []string
}

type deletionWorld struct {
	*uploadFoldersWorld
	oauth *oauthWorld

	db              *gormlib.DB
	projMgr         repositories.ProjectionManager
	locks           repositories.AccountErasureLocks
	featureGrants   repositories.FeatureGrantRepository
	featureSettings repositories.FeatureSettingsRepository
	eventStore      pericarpdomain.EventStore
	dispatcher      *pericarpdomain.EventDispatcher
	erasure         *application.AccountErasureService
	members         repositories.AccountMemberQuery
	resourceRepo    repositories.ResourceRepository
	files           *onceFailingFileService

	// owners maps an account name to the email of the person who owns it, so
	// "the owner of X" and "X has a stored photo" know who acts.
	owners map[string]string
	// actor is the person "they" refers to: the last one a step named.
	actor string
	noted map[string]*notedIdentifiers
	// deletedAccountID is the account the last accepted deletion removed,
	// kept for "not the one they deleted".
	deletedAccountID string
	// lastCookies is every cookie the last deletion answer set.
	lastCookies []*http.Cookie
	// secondDevice is the cookie of a second sign-in by the same person.
	secondDevice string
	export       *capturedAnswer
	lastRun      commandRun
	// connectorStatuses records the HTTP status of each MCP request Claude
	// made after the deletion.
	connectorStatuses []int
	// firstToken keeps the first connector's token while a second person
	// authorizes their own.
	firstToken string
}

func initAccountDeletionScenario(sc *godog.ScenarioContext) {
	w := &deletionWorld{
		uploadFoldersWorld: &uploadFoldersWorld{
			accountScopedWorld: &accountScopedWorld{
				accounts: map[string]string{},
				people:   map[string]*person{},
			},
			photos:  map[string]*storedPhoto{},
			legacy:  map[string]*storedPhoto{},
			recipes: map[string]string{},
		},
		owners: map[string]string{},
		noted:  map[string]*notedIdentifiers{},
	}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		if w.oauth != nil {
			w.oauth.teardown()
		}
		if w.tmpDir != "" {
			_ = os.RemoveAll(w.tmpDir)
		}
		return ctx, nil
	})

	// --- instances ---
	sc.Step(`^a WeOS instance where password sign-in is enabled and requests are authenticated by their session$`,
		func() error { return w.bootDeletion(false) })
	sc.Step(`^a WeOS instance where password sign-in and registration are enabled and requests are authenticated by their session$`,
		func() error { return w.bootDeletion(true) })
	sc.Step(`^a demo instance where password sign-in is enabled and no Google provider is configured$`, w.bootDemo)
	sc.Step(`^the meal-planning preset is installed$`, w.mealPlanningInstalled)

	// --- accounts and people ---
	sc.Step(`^the account "([^"]*)", whose owner "([^"]*)" signs in with password "([^"]*)"$`, w.accountOwnedBy)
	sc.Step(`^"([^"]*)" also belongs to the account "([^"]*)"$`, w.alsoBelongsTo)
	sc.Step(`^"([^"]*)" was also added to the account "([^"]*)" and is signed in to it$`, w.alsoAddedAndSignedInTo)
	sc.Step(`^"([^"]*)" belongs to "([^"]*)" as an ordinary member$`, func(email, name string) error {
		return w.memberWithRole(email, name, "member")
	})
	sc.Step(`^"([^"]*)" also belongs to "([^"]*)" with the role "([^"]*)"$`, w.memberWithRole)
	sc.Step(`^"([^"]*)" is signed in to "([^"]*)"$`, w.signedInTo)
	sc.Step(`^"([^"]*)" is an instance admin$`, w.isInstanceAdmin)
	sc.Step(`^"([^"]*)" has a "([^"]*)" named "([^"]*)" in "([^"]*)"$`, w.personHasResourceIn)
	sc.Step(`^"([^"]*)" has been deactivated$`, w.accountDeactivated)
	sc.Step(`^"([^"]*)" is signed in and their requests are being served$`, w.signedInAndServedBy)
	sc.Step(`^"([^"]*)" signs in through the provider "([^"]*)" and owns the account "([^"]*)"$`, w.signsInThroughProvider)
	sc.Step(`^"([^"]*)" signs in again through the provider "([^"]*)"$`, w.signsInAgainThroughProvider)
	sc.Step(`^"([^"]*)" has a pantry holding "([^"]*)" and a stored photo named "([^"]*)"$`, w.pantryAndPhoto)
	sc.Step(`^"([^"]*)" has a stored photo named "([^"]*)"$`, w.accountHasStoredPhoto)
	sc.Step(`^a photo "([^"]*)" was stored at its flat URL before files were kept by account$`, w.legacyFlatPhoto)
	sc.Step(`^"([^"]*)" holds one of everything an account can own: .*$`, w.holdsOneOfEverything)
	sc.Step(`^every identifier of "([^"]*)" has been noted: .*$`, w.noteIdentifiers)
	sc.Step(`^"([^"]*)" is signed in on a second device as well$`, w.signedInOnSecondDevice)

	// --- invitations ---
	sc.Step(`^the account "([^"]*)" has invited "([^"]*)", who accepted and now also belongs to it$`, w.invitedAndAccepted)
	sc.Step(`^the account "([^"]*)" has invited "([^"]*)", who accepted with password "([^"]*)" and belongs to nothing else$`,
		w.invitedAndAcceptedWithPassword)
	sc.Step(`^the account "([^"]*)" has invited "([^"]*)", who has not accepted yet$`, w.invitedNotAccepted)
	sc.Step(`^"([^"]*)" accepts the invitation$`, w.acceptsInvitationAs)
	sc.Step(`^the invitation is refused$`, w.invitationRefused)
	sc.Step(`^the answer says the invitation is not one the instance knows rather than that the instance failed$`, w.invitationNotKnown)
	sc.Step(`^"([^"]*)" is not a member of "([^"]*)"$`, w.notAMember)
	sc.Step(`^"([^"]*)" is still a member of "([^"]*)"$`, w.stillAMember)

	// --- signing in ---
	sc.Step(`^"([^"]*)" signs in$`, w.signsInAs)
	sc.Step(`^"([^"]*)" signs in again$`, w.signsInAs)
	sc.Step(`^"([^"]*)" signs in with password "([^"]*)"$`, w.signsInWithPassword)
	sc.Step(`^"([^"]*)" registers with password "([^"]*)"$`, w.registersAs)
	sc.Step(`^the sign-in succeeds$`, w.lastSucceededFor)
	sc.Step(`^the registration succeeds$`, w.lastSucceededFor)
	sc.Step(`^the sign-in is refused as if the person had never registered$`, w.signInRefusedAsUnknown)
	sc.Step(`^the sign-in says the account's deletion is unfinished$`, w.signInSaysErasurePending)
	sc.Step(`^"([^"]*)" can sign in with password "([^"]*)"$`, w.canSignInWith)
	sc.Step(`^"([^"]*)" cannot sign in with password "([^"]*)"$`, w.cannotSignInWith)

	// --- deleting ---
	sc.Step(`^"([^"]*)" deletes their account, confirming with "DELETE"$`, w.deletesAccount)
	// The body carries quotes of its own, so it is matched loosely.
	sc.Step(`^"([^"]*)" asks to delete their account sending the body "(.*)"$`, w.asksToDeleteWithBody)
	sc.Step(`^someone carrying no session asks to delete an account, confirming with "DELETE"$`, w.anonymousDelete)
	sc.Step(`^they ask to delete the account they are acting in, confirming with "DELETE"$`, w.actorDeletes)
	sc.Step(`^the owner of "([^"]*)" deletes it, confirming with "DELETE"$`, w.ownerDeletes)
	sc.Step(`^"([^"]*)" has deleted their account$`, w.hasDeleted)
	sc.Step(`^"([^"]*)" has deleted their account, keeping the cookie they held before$`, w.hasDeleted)
	sc.Step(`^the account "([^"]*)" has been deleted by its owner$`, w.deletedByOwner)
	sc.Step(`^"([^"]*)" deletes their account again with that cookie, confirming with "DELETE"$`, w.deletesAgainWithOldCookie)
	sc.Step(`^the file store will refuse to remove the folder of "([^"]*)" the first time it is asked$`, w.fileStoreWillRefuse)
	sc.Step(`^a deletion of "([^"]*)" failed after the lock was taken, leaving the account locked$`, w.deletionFailedLeavingLock)
	sc.Step(`^"([^"]*)" impersonates "([^"]*)"$`, w.impersonates)

	// --- the operator ---
	sc.Step(`^the operator runs "([^"]*)"$`, w.operatorRuns)
	sc.Step(`^the command exits successfully$`, w.commandSucceeded)
	sc.Step(`^the command exits with a failure$`, w.commandFailed)
	sc.Step(`^the failure names the confirm flag as what was missing$`, func() error { return w.failureNames("--confirm") })
	sc.Step(`^the failure names the account id it could not find$`, func() error { return w.failureNames("2Zq7mQb0Xn9YpR4sT1vW8kLcE3dA") })

	// --- requests afterwards ---
	sc.Step(`^they make a request with the session they already held$`, w.actorRequests)
	sc.Step(`^"([^"]*)" makes a request with the session they already held$`, w.personRequests)
	sc.Step(`^they list the projects they can see$`, w.actorRequests)
	sc.Step(`^they make a request with the session on the second device$`, w.requestOnSecondDevice)
	sc.Step(`^they read who they are signed in as$`, w.readsWhoTheyAre)
	sc.Step(`^they read who they are signed in as on the second device$`, w.readsWhoTheyAreOnSecondDevice)
	sc.Step(`^the projects "([^"]*)" sees with the session they already held include "([^"]*)"$`, w.projectsSeenInclude)
	sc.Step(`^"([^"]*)" can still read its photo "([^"]*)" by its URL$`, w.accountReadsPhoto)
	sc.Step(`^"([^"]*)" requests "([^"]*)" at its flat URL$`, w.personRequestsFlatPhoto)
	sc.Step(`^the photo is served$`, w.photoServed)
	sc.Step(`^the photo's old URL is refused as not found$`, w.oldPhotoNotFound)
	sc.Step(`^the projections are rebuilt from event history$`, w.rebuildProjections)

	// --- what came back ---
	sc.Step(`^the deletion is accepted$`, func() error { return w.actorStatusIs(http.StatusOK) })
	sc.Step(`^the request is served$`, func() error { return w.actorStatusIs(http.StatusOK) })
	sc.Step(`^the request is refused as a bad request$`, func() error { return w.actorStatusIs(http.StatusBadRequest) })
	sc.Step(`^the request is refused as not authenticated$`, func() error { return w.actorStatusIs(http.StatusUnauthorized) })
	sc.Step(`^the request is refused as forbidden$`, func() error { return w.actorStatusIs(http.StatusForbidden) })
	sc.Step(`^the request is refused as not found$`, func() error { return w.actorStatusIs(http.StatusNotFound) })
	sc.Step(`^the refusal carries no code$`, func() error { return w.actorCodeIs("") })
	sc.Step(`^the refusal says their access to the account was taken away$`, func() error { return w.actorCodeIs(apimw.CodeAccountAccessRevoked) })
	sc.Step(`^the refusal says the account itself is not available$`, func() error { return w.actorCodeIs(apimw.CodeAccountDeactivated) })
	sc.Step(`^the refusal says the account's deletion is unfinished$`, func() error { return w.actorCodeIs(apimw.CodeAccountErasurePending) })
	sc.Step(`^the refusal does not say the account's deletion is unfinished$`, w.refusalIsNotErasurePending)
	sc.Step(`^the answer clears the session cookie and the token cookie$`, w.answerClearsCookies)
	sc.Step(`^the answer reports that (\d+) (?:person|people) lost the account$`, w.answerReportsMembersLost)
	sc.Step(`^the answer says the deletion did not finish and can be run again$`, w.answerSaysUnfinished)
	sc.Step(`^the answer says the account they act in has (\d+) members$`, w.answerSaysMemberCount)
	sc.Step(`^the account their requests act in is "([^"]*)"$`, w.actingAccountIs)
	sc.Step(`^the account their requests act in is not "([^"]*)"$`, w.actingAccountIsNot)
	sc.Step(`^the account their requests act in is not the one they deleted$`, w.actingAccountIsNotTheDeleted)
	sc.Step(`^their pantry holds nothing$`, w.pantryHoldsNothing)
	sc.Step(`^the recipes they see include "([^"]*)"$`, func(name string) error { return w.recipesSeen(name, true) })
	sc.Step(`^the recipes they see exclude "([^"]*)"$`, func(name string) error { return w.recipesSeen(name, false) })
	sc.Step(`^the recipe "([^"]*)" is still there for "([^"]*)"$`, w.recipeStillThere)
	sc.Step(`^the recipe "([^"]*)" is still stored for "([^"]*)"$`, w.recipeStillStored)
	sc.Step(`^the account "([^"]*)" is still stored, marked as being erased$`, w.accountStoredAndLocked)
	sc.Step(`^nothing of "([^"]*)" remains in any store on the instance:$`, w.nothingRemainsTable)
	sc.Step(`^nothing of "([^"]*)" remains in any store on the instance$`, w.nothingRemains)

	// --- the connector, on the demo instance ---
	sc.Step(`^the bootstrap account "([^"]*)" with password "([^"]*)"$`, w.demoAccount)
	sc.Step(`^a second account "([^"]*)" with password "([^"]*)"$`, w.demoAccount)
	sc.Step(`^Claude has been authorized as a connector by "([^"]*)" signing in$`, w.claudeAuthorizedBy)
	sc.Step(`^Claude has also been authorized as a connector by "([^"]*)" signing in$`, w.claudeAlsoAuthorizedBy)
	sc.Step(`^Claude asks the instance which tools it offers$`, w.claudeListsTools)
	sc.Step(`^Claude creates the task "([^"]*)" through the instance's tools$`, w.claudeCreatesTask)
	sc.Step(`^Claude, acting as "([^"]*)", asks the instance which tools it offers$`, w.claudeActingAsListsTools)
	sc.Step(`^Claude presents the refresh token it was issued for a new access token$`, w.claudePresentsRefreshToken)
	sc.Step(`^both of Claude's requests are refused as not authenticated$`, w.bothConnectorRequestsRefused)
	sc.Step(`^the instance lists the tools it offers$`, w.instanceListedTools)
	sc.Step(`^the exchange is refused$`, w.exchangeRefused)
	sc.Step(`^no access token is issued$`, w.noAccessTokenIssued)
	sc.Step(`^Claude is still registered as a connector on that instance$`, w.claudeStillRegistered)
	sc.Step(`^no row and no graph store directory names the deleted account afterwards$`, w.noRowNamesDeletedAccount)

	// --- exporting ---
	sc.Step(`^"([^"]*)" exports their account$`, w.exportsAccount)
	sc.Step(`^someone carrying no session asks for an account export$`, w.anonymousExport)
	sc.Step(`^the export is served as JSON-LD with a context and a graph$`, w.exportIsJSONLD)
	sc.Step(`^the export's graph holds the recipes "([^"]*)" and "([^"]*)"$`, func(a, b string) error { return w.exportHolds(a, b) })
	sc.Step(`^the export's graph holds the recipes "([^"]*)"$`, func(a string) error { return w.exportHolds(a) })
	sc.Step(`^the export's graph does not hold the recipe "([^"]*)"$`, w.exportDoesNotHold)
	sc.Step(`^the export's graph holds no pantry, no food item and no photo$`, w.exportHoldsNoPantryOrPhoto)
	sc.Step(`^the export's graph is empty$`, w.exportIsEmpty)
	sc.Step(`^the export says of itself that it holds recipes and nothing else$`, w.exportSaysRecipesOnly)
}

// --- instances --------------------------------------------------------------

// bootDeletion boots the session-authenticated instance with story 2's upload
// routes and this story's account routes, the erasure guard in front of every
// protected route the way serve.go mounts it, and a file service that can be
// told to refuse one folder removal.
func (w *deletionWorld) bootDeletion(registration bool) error {
	dir, err := os.MkdirTemp("", "weos-account-deletion-e2e-")
	if err != nil {
		return fmt.Errorf("could not create a temp dir: %w", err)
	}
	w.tmpDir = dir
	w.dsn = filepath.Join(dir, "deletion.db")
	w.uploadDir = filepath.Join(dir, "uploads")
	w.setEnv("STORAGE_LOCAL_PATH", ptr(w.uploadDir))
	w.setEnv("STORAGE_GCS_BUCKET", nil)
	w.setEnv("STORAGE_S3_BUCKET", nil)
	w.setEnv("FEATURES", ptr(fmt.Sprintf(
		`[{"key":%q,"displayName":"Recipe export","description":"declared for the erasure scenarios","default":false,"manageable":true,"grantable":true}]`,
		grantableFeature)))

	w.files = &onceFailingFileService{}
	w.extraOptions = []fx.Option{
		fx.Decorate(func(inner services.FileService) services.FileService {
			w.files.FileService = inner
			return w.files
		}),
		fx.Populate(&w.fileService, &w.permissionService),
		fx.Populate(&w.db, &w.projMgr, &w.locks, &w.featureGrants, &w.featureSettings),
		fx.Populate(&w.eventStore, &w.dispatcher, &w.erasure, &w.members, &w.resourceRepo),
	}
	w.mountExtraRoutes = w.mountDeletionRoutes
	return w.boot(registration)
}

// mountDeletionRoutes mounts, through the same constructors serve.go uses,
// the upload routes, the account routes, the identity read and the
// impersonation start.
func (w *deletionWorld) mountDeletionRoutes(api *echo.Group, guards []echo.MiddlewareFunc) {
	w.mountUploadRoutes(api, guards)

	accountHandler := handlers.NewAccountHandler(handlers.AccountHandlerConfig{
		Erasure:        w.erasure,
		Accounts:       w.accountRepo,
		Resources:      w.resourceRepo,
		ResourceTypes:  w.resourceTypeService,
		SessionManager: w.sessionManager,
		Store:          w.sessionStore,
		SecureCookies:  false,
		Logger:         w.logger,
	})
	api.GET("/account/export", accountHandler.Export, guards...)
	api.DELETE("/account", accountHandler.Delete,
		apimw.SessionAuthForErasure(w.sessionManager, w.authService, w.accountRepo, w.locks, w.logger))

	impersonation := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store: w.sessionStore, AccountRepo: w.accountRepo, AgentRepo: w.agentRepo,
		CredRepo: w.credRepo, Members: w.members, SessionManager: w.sessionManager,
		AuthService: w.authService, ErasureLocks: w.locks, Logger: w.logger,
	})
	authHandlers := authhttp.NewAuthHandlers(authhttp.HandlerConfig{
		AuthService: w.authService, SessionManager: w.sessionManager, Credentials: w.credRepo, Logger: w.logger,
	})
	api.GET("/auth/me", impersonation.Me(authHandlers))
	api.POST("/admin/impersonate", impersonation.Start, guards...)
}

// --- people and staging -----------------------------------------------------

func (w *deletionWorld) accountOwnedBy(name, email, password string) error {
	w.owners[name] = email
	w.actor = email
	return w.accountWithOwner(name, email, password)
}

func (w *deletionWorld) alsoAddedAndSignedInTo(email, name string) error {
	w.actor = email
	return w.alsoAddedAndSignedIn(email, name)
}

func (w *deletionWorld) memberWithRole(email, name, role string) error {
	p, err := w.personFor(email, aPassword)
	if err != nil {
		return err
	}
	id, ok := w.accounts[name]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", name)
	}
	if err := w.accountRepo.SaveMember(context.Background(), id, p.agentID, role); err != nil {
		return fmt.Errorf("could not add %q to %q as %s: %w", email, name, role, err)
	}
	return nil
}

func (w *deletionWorld) signedInTo(email, name string) error {
	id, ok := w.accounts[name]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", name)
	}
	w.actor = email
	return w.stageSession(email, id)
}

// isInstanceAdmin is deliberately empty: an owner of an account already holds
// the role the impersonation route requires, so there is nothing to stage.
func (w *deletionWorld) isInstanceAdmin(string) error { return nil }

func (w *deletionWorld) signedInAndServedBy(email string) error {
	w.actor = email
	return w.signedInAndServed(email)
}

func (w *deletionWorld) signsInAs(email string) error {
	w.actor = email
	if w.oauth != nil {
		return w.oauth.signedInAs(email)
	}
	return w.signsIn(email)
}

func (w *deletionWorld) signsInWithPassword(email, password string) error {
	w.actor = email
	p, ok := w.people[email]
	if !ok {
		p = &person{email: email}
		w.people[email] = p
		w.order = append(w.order, email)
	}
	p.password = password
	return w.signsIn(email)
}

func (w *deletionWorld) registersAs(email, password string) error {
	if err := w.registers(email, password); err != nil {
		return err
	}
	w.actor = email
	return nil
}

func (w *deletionWorld) current() (*person, error) {
	if w.actor != "" {
		if p, ok := w.people[w.actor]; ok {
			return p, nil
		}
		return nil, fmt.Errorf("%q acted but was never staged", w.actor)
	}
	return w.currentPerson()
}

func (w *deletionWorld) personNamed(email string) (*person, error) {
	p, ok := w.people[email]
	if !ok {
		return nil, fmt.Errorf("%q has not been staged", email)
	}
	return p, nil
}

// ownerOf finds the person who owns the account. An account a scenario
// created without naming an owner — "counsel was also added to Cedar Realty"
// — is given one here, so "the owner of Cedar Realty" is somebody real whose
// sign-in lands in that account rather than in a personal account of their
// own.
func (w *deletionWorld) ownerOf(name string) (*person, error) {
	if email, ok := w.owners[name]; ok {
		return w.personNamed(email)
	}
	id, ok := w.accounts[name]
	if !ok {
		return nil, fmt.Errorf("no account named %q has been staged", name)
	}
	email := ownerEmailFor(name)
	p, err := w.personFor(email, aPassword)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	if err := w.accountRepo.SaveMember(ctx, id, p.agentID, authentities.RoleOwner); err != nil {
		return nil, fmt.Errorf("could not make %q the owner of %q: %w", email, name, err)
	}
	personal, err := w.accountRepo.FindPersonalByMember(ctx, p.agentID)
	if err != nil {
		return nil, fmt.Errorf("could not read the personal account of %q: %w", email, err)
	}
	if personal != nil && personal.GetID() != id {
		if err := w.deactivate(personal.GetID()); err != nil {
			return nil, err
		}
	}
	w.owners[name] = email
	return p, nil
}

// ensureSignedIn gives the person a cookie if they hold none.
func (w *deletionWorld) ensureSignedIn(p *person) error {
	if p.cookie != "" {
		return nil
	}
	if err := w.signsIn(p.email); err != nil {
		return err
	}
	if p.cookie == "" {
		return fmt.Errorf("%q could not sign in: %s", p.email, describe(p.lastAnswer))
	}
	return nil
}

func (w *deletionWorld) lastSucceededFor() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if p.lastAnswer == nil || p.lastAnswer.status != http.StatusOK {
		return fmt.Errorf("expected success, got %s", describe(p.lastAnswer))
	}
	return nil
}

func (w *deletionWorld) signInRefusedAsUnknown() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if p.lastAnswer == nil || p.lastAnswer.status != http.StatusUnauthorized {
		return fmt.Errorf("expected the sign-in refused with 401, got %s", describe(p.lastAnswer))
	}
	if !strings.Contains(p.lastAnswer.body, "invalid email or password") {
		return fmt.Errorf("the refusal is not the one an unknown person gets: %s", describe(p.lastAnswer))
	}
	return nil
}

func (w *deletionWorld) signInSaysErasurePending() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	var body struct {
		Data struct {
			ErasurePending bool   `json:"erasure_pending"`
			Code           string `json:"code"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(p.lastAnswer.body), &body); err != nil {
		return fmt.Errorf("the sign-in answer is not the envelope: %s", describe(p.lastAnswer))
	}
	if !body.Data.ErasurePending || body.Data.Code != apimw.CodeAccountErasurePending {
		return fmt.Errorf("the sign-in does not say the deletion is unfinished: %s", describe(p.lastAnswer))
	}
	return nil
}

// attemptSignIn tries a password sign-in without disturbing the person's
// current cookie, and reports the status.
func (w *deletionWorld) attemptSignIn(email, password string) (int, error) {
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	res, err := http.Post(w.server.URL+signInPath, "application/json", strings.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("sign-in for %q failed: %w", email, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode, nil
}

func (w *deletionWorld) canSignInWith(email, password string) error {
	status, err := w.attemptSignIn(email, password)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("expected %q to sign in, got %d", email, status)
	}
	return nil
}

func (w *deletionWorld) cannotSignInWith(email, password string) error {
	status, err := w.attemptSignIn(email, password)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return fmt.Errorf("expected %q not to sign in any more, but the password still works", email)
	}
	return nil
}

// --- provider sign-in -------------------------------------------------------

func providerUserID(email string) string { return "google-" + email }

// signsInThroughProvider stages a person the way the OAuth callback does —
// FindOrCreateAgent with the provider's user id — and gives them a session in
// the account it minted, renamed to the name the scenario uses.
func (w *deletionWorld) signsInThroughProvider(email, provider, name string) error {
	ctx := context.Background()
	agent, _, account, err := w.authService.FindOrCreateAgent(ctx, authapp.UserInfo{
		Provider: provider, ProviderUserID: providerUserID(email), Email: email, DisplayName: displayNameFor(email),
	})
	if err != nil {
		return fmt.Errorf("could not sign %q in through %s: %w", email, provider, err)
	}
	if account == nil {
		return fmt.Errorf("the %s sign-in of %q resolved no account", provider, email)
	}
	p := &person{email: email, agentID: agent.GetID(), accountID: account.GetID()}
	w.people[email] = p
	w.order = append(w.order, email)
	w.actor = email
	if err := w.renameAccount(account.GetID(), name); err != nil {
		return err
	}
	w.accounts[name] = account.GetID()
	w.owners[name] = email
	return w.stageSession(email, account.GetID())
}

// signsInAgainThroughProvider signs the person in a second time the way the
// callback does. A provider sign-in passes over an inactive account, so for a
// person whose account is locked it resolves none, and the session it makes
// names none — which is exactly the session the deletion route has to admit.
func (w *deletionWorld) signsInAgainThroughProvider(email, provider string) error {
	ctx := context.Background()
	agent, _, account, err := w.authService.FindOrCreateAgent(ctx, authapp.UserInfo{
		Provider: provider, ProviderUserID: providerUserID(email), Email: email, DisplayName: displayNameFor(email),
	})
	if err != nil {
		return fmt.Errorf("could not sign %q in again through %s: %w", email, provider, err)
	}
	p, err := w.personNamed(email)
	if err != nil {
		return err
	}
	p.agentID = agent.GetID()
	accountID := ""
	if account != nil {
		accountID = account.GetID()
	}
	p.accountID = accountID
	w.actor = email
	return w.stageSession(email, accountID)
}

// --- pantries, photos, and one of everything --------------------------------

func (w *deletionWorld) accountHasStoredPhoto(name, filename string) error {
	owner, err := w.ownerOf(name)
	if err != nil {
		return err
	}
	return w.storePhotoAs(owner, name, filename)
}

// storePhotoAs uploads a photo as the person, over HTTP, and remembers it
// under the account's name.
func (w *deletionWorld) storePhotoAs(p *person, accountName, filename string) error {
	if err := w.ensureSignedIn(p); err != nil {
		return err
	}
	body := photoBytes(accountName, filename)
	answer, err := w.upload(p.cookie, filename, body)
	if err != nil {
		return err
	}
	if answer.status != http.StatusCreated {
		return fmt.Errorf("%q could not store %q: %s", accountName, filename, describe(answer))
	}
	stored, err := uploadedPath(answer)
	if err != nil {
		return err
	}
	w.photos[accountName] = &storedPhoto{path: stored, body: body}
	return nil
}

// stagePantry gives the account a pantry holding one food item, through the
// resource service as the owner.
func (w *deletionWorld) stagePantry(name, item string) error {
	owner, err := w.ownerOf(name)
	if err != nil {
		return err
	}
	if err := w.mealPlanningInstalled(); err != nil {
		return err
	}
	ctx := w.identityContext(owner, w.accounts[name])
	pantry, err := w.resourceService.Create(ctx, application.CreateResourceCommand{
		TypeSlug: "pantry", Data: json.RawMessage(`{"name":"Kitchen","isDefault":true}`),
	})
	if err != nil {
		return fmt.Errorf("could not stage a pantry for %q: %w", name, err)
	}
	ingredient, err := w.resourceService.Create(ctx, application.CreateResourceCommand{
		TypeSlug: "ingredient", Data: json.RawMessage(fmt.Sprintf(`{"name":%q}`, item)),
	})
	if err != nil {
		return fmt.Errorf("could not stage the ingredient %q for %q: %w", item, name, err)
	}
	data, _ := json.Marshal(map[string]any{
		"name": item, "quantity": 1, "unit": "kg", "ingredient": ingredient.GetID(), "pantry": pantry.GetID(),
	})
	if _, err := w.resourceService.Create(ctx, application.CreateResourceCommand{TypeSlug: "food-item", Data: data}); err != nil {
		return fmt.Errorf("could not stage the food item %q for %q: %w", item, name, err)
	}
	return nil
}

func (w *deletionWorld) identityContext(p *person, accountID string) context.Context {
	return authIdentityContext(p.agentID, accountID)
}

func (w *deletionWorld) pantryAndPhoto(name, item, filename string) error {
	if err := w.stagePantry(name, item); err != nil {
		return err
	}
	return w.accountHasStoredPhoto(name, filename)
}

// holdsOneOfEverything stages, for the account, every kind of thing the
// erasure has to remove: a recipe, a pantry item, a stored photo, a feature
// grant, an authorized connector and an outstanding invitation — and the
// per-account settings rows beside them.
func (w *deletionWorld) holdsOneOfEverything(name string) error {
	owner, err := w.ownerOf(name)
	if err != nil {
		return err
	}
	accountID := w.accounts[name]
	if err := w.mealPlanningInstalled(); err != nil {
		return err
	}
	if err := w.personHasResourceIn(owner.email, "recipe", "Sunday Lasagna", name); err != nil {
		return err
	}
	if err := w.stagePantry(name, "Basmati rice"); err != nil {
		return err
	}
	if err := w.storePhotoAs(owner, name, "lasagna.jpg"); err != nil {
		return err
	}
	ctx := context.Background()
	if err := w.featureGrants.Grant(ctx, entityGrant(owner.agentID, accountID)); err != nil {
		return fmt.Errorf("could not stage a feature grant for %q: %w", name, err)
	}
	if err := w.featureSettings.SetOverride(ctx, repositories.FeatureScopeAccount, accountID, grantableFeature, true); err != nil {
		return fmt.Errorf("could not stage a feature setting for %q: %w", name, err)
	}
	if err := w.behaviorSettings.SaveByAccountAndType(ctx, accountID, "pantry", []string{}); err != nil {
		return fmt.Errorf("could not stage a behavior setting for %q: %w", name, err)
	}
	if err := w.stageConnector(accountID, owner.agentID); err != nil {
		return err
	}
	_, _, err = w.inviteService.CreateInvite(ctx, accountID, "invitee@"+slugForAccountName(name)+".example",
		"member", owner.agentID)
	if err != nil {
		return fmt.Errorf("could not stage an outstanding invitation into %q: %w", name, err)
	}
	return nil
}

func (w *deletionWorld) signedInOnSecondDevice(email string) error {
	p, err := w.personNamed(email)
	if err != nil {
		return err
	}
	if err := w.ensureSignedIn(p); err != nil {
		return err
	}
	first := p.cookie
	firstSession := p.sessionID
	if err := w.signsIn(email); err != nil {
		return err
	}
	if p.cookie == "" || p.cookie == first {
		return fmt.Errorf("%q could not sign in on a second device: %s", email, describe(p.lastAnswer))
	}
	w.secondDevice = p.cookie
	p.cookie = first
	p.sessionID = firstSession
	w.actor = email
	return nil
}

// --- invitations ------------------------------------------------------------

// inviteInto creates an invitation from the account's recorded owner, staging
// the account and an owner for it when the scenario has not yet.
func (w *deletionWorld) inviteInto(name, email string) (string, error) {
	if _, ok := w.accounts[name]; !ok {
		if err := w.accountOwnedBy(name, ownerEmailFor(name), aPassword); err != nil {
			return "", err
		}
	}
	owner, err := w.ownerOf(name)
	if err != nil {
		return "", err
	}
	_, token, err := w.inviteService.CreateInvite(context.Background(), w.accounts[name], email, "member", owner.agentID)
	if err != nil {
		return "", fmt.Errorf("could not invite %q into %q: %w", email, name, err)
	}
	w.inviteToken = token
	return token, nil
}

// acceptOverHTTP accepts the outstanding invitation as an anonymous caller
// and reports the agent the instance activated for it.
func (w *deletionWorld) acceptOverHTTP(email string) (string, error) {
	body := fmt.Sprintf(`{"token":%q,"email":%q,"name":%q}`, w.inviteToken, email, displayNameFor(email))
	probe := &person{email: "accepting:" + email}
	if err := w.request(probe, http.MethodPost, "/api/invites/accept", body); err != nil {
		return "", err
	}
	if probe.lastAnswer.status != http.StatusOK {
		return "", fmt.Errorf("%q could not accept the invitation: %s", email, describe(probe.lastAnswer))
	}
	var answer struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(probe.lastAnswer.body), &answer); err != nil || answer.Data.ID == "" {
		return "", fmt.Errorf("the acceptance named no agent: %s", describe(probe.lastAnswer))
	}
	return answer.Data.ID, nil
}

// invitedAndAccepted runs the invitation through the instance — the transaction
// that commits the invitee agent, its credential and the membership together
// is the one wm-atptf is about — and then also records the person's own agent
// as a member, so they belong to the account as themselves.
func (w *deletionWorld) invitedAndAccepted(name, email string) error {
	if _, err := w.inviteInto(name, email); err != nil {
		return err
	}
	if _, err := w.acceptOverHTTP(email); err != nil {
		return err
	}
	if p, ok := w.people[email]; ok {
		if err := w.accountRepo.SaveMember(context.Background(), w.accounts[name], p.agentID, "member"); err != nil {
			return fmt.Errorf("could not record %q as a member of %q: %w", email, name, err)
		}
	}
	return nil
}

// invitedAndAcceptedWithPassword stages a person whose only way in was the
// invitation: the agent the acceptance activated is given a password, and
// belongs to nothing but the account.
func (w *deletionWorld) invitedAndAcceptedWithPassword(name, email, password string) error {
	if _, err := w.inviteInto(name, email); err != nil {
		return err
	}
	agentID, err := w.acceptOverHTTP(email)
	if err != nil {
		return err
	}
	hash, err := bcryptHash(password)
	if err != nil {
		return err
	}
	err = w.authService.ImportPasswordCredential(context.Background(), email, displayNameFor(email), hash,
		agentID, w.accounts[name])
	if err != nil {
		return fmt.Errorf("could not give %q a password: %w", email, err)
	}
	w.people[email] = &person{email: email, password: password, agentID: agentID, accountID: w.accounts[name]}
	w.order = append(w.order, email)
	return nil
}

func (w *deletionWorld) invitedNotAccepted(name, email string) error {
	_, err := w.inviteInto(name, email)
	return err
}

func (w *deletionWorld) acceptsInvitationAs(email string) error {
	w.actor = email
	return w.acceptsInvitation(email)
}

func (w *deletionWorld) invitationNotKnown() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if p.lastAnswer.status >= 500 {
		return fmt.Errorf("the instance blamed itself with %d; the invitation is what is gone: %s",
			p.lastAnswer.status, describe(p.lastAnswer))
	}
	if p.lastAnswer.status < 400 {
		return fmt.Errorf("the invitation was accepted: %s", describe(p.lastAnswer))
	}
	return nil
}
