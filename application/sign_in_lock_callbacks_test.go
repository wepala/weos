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

package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"
	weosgorm "github.com/wepala/weos/v3/infrastructure/database/gorm"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	authgorm "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/database/gorm"
	esdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/segmentio/ksuid"
	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// pausingAuth is storeAuth behind an OAuth callback: the first time it finds no
// credential for the identity, it pauses before it creates the person, as a
// callback that has read "nobody holds this identity" and written nothing.
type pausingAuth struct {
	storeAuth
	pause *pausePoint
}

func (a pausingAuth) FindOrCreateAgent(ctx context.Context, info authapp.UserInfo) (*entities.Agent, *entities.Credential, *entities.Account, error) {
	a.s.mu.Lock()
	known := a.s.findByProvider(info.Provider, info.ProviderUserID) != nil
	a.s.mu.Unlock()
	if !known {
		a.pause.hold()
	}
	return a.storeAuth.FindOrCreateAgent(ctx, info)
}

// userInfoFor is the profile a provider returns for id, email verified.
func userInfoFor(id AssertedIdentity) authapp.UserInfo {
	return authapp.UserInfo{
		Provider: id.Provider, ProviderUserID: id.Subject,
		Email: id.Email, EmailVerified: true, DisplayName: id.Name,
	}
}

// callbackSignIn calls FindOrCreateAgent the way an OAuth callback does, with
// the new-account flag installed, and reports what it reached as a sign-in.
func callbackSignIn(ctx context.Context, auth authapp.AuthenticationService, id AssertedIdentity) (AssertedSignInResult, error) {
	isNew := new(bool)
	agent, credential, account, err := auth.FindOrCreateAgent(WithNewAccountFlag(ctx, isNew), userInfoFor(id))
	return AssertedSignInResult{Agent: agent, Credential: credential, Account: account, NewAccount: *isNew}, err
}

// An OAuth callback writes the owner's google credential through the
// application's AuthenticationService, outside owner binding. It holds the
// sign-in lock binding holds, so an assertion for the owner's apple identity
// that arrives while the callback has read "nobody holds this identity" and
// created nobody waits for it, then links to the person the callback created.
func TestAnOAuthCallbackAndAnAssertionForOneEmailLeaveOnePerson(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := newMemoryAuthStore()
	lock := newSharedSignInLock()
	pause := newPausePoint()
	callback := &newAccountSignalService{
		AuthenticationService: pausingAuth{storeAuth: storeAuth{s: s}, pause: pause},
		credentials:           storeCredentials{s: s},
		lock:                  lock,
	}
	assertion := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) { cfg.Lock = lock })

	a, b := interleaveReplicas(ctx, t, pause,
		func() (AssertedSignInResult, error) {
			return callbackSignIn(ctx, callback, dana(OAuthProviderGoogle, googleSub))
		},
		func() (AssertedSignInResult, error) { return assertion.SignIn(ctx, dana(OAuthProviderApple, appleSub)) },
		func(ctx context.Context) {
			select {
			case <-lock.waiting:
			case <-ctx.Done():
			}
		})

	if s.createCount() != 1 {
		t.Fatalf("a callback and an assertion created %d people for one owner's two identities, want 1", s.createCount())
	}
	requireOnePersonOneCreate(t, []AssertedSignInResult{a, b})
	if cred := s.credentialFor(OAuthProviderApple, appleSub); cred == nil || cred.AgentID() != a.Agent.GetID() {
		t.Fatal("the assertion's identity was not linked to the person the callback created")
	}
}

// An asserted sign-in reaches FindOrCreateAgent through the application's
// AuthenticationService, which holds the sign-in lock for the callbacks. It
// must not wait on the keys the sign-in already holds.
func TestAssertedSignInThroughTheLockingAuthServiceHoldsTheLockOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := newMemoryAuthStore()
	lock := newSharedSignInLock()
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.Lock = lock
		cfg.Auth = &newAccountSignalService{AuthenticationService: storeAuth{s: s}, credentials: storeCredentials{s: s}, lock: lock}
	})

	if _, err := svc.SignIn(ctx, dana(OAuthProviderGoogle, googleSub)); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if holds := lock.recordedHolds(); len(holds) != 1 {
		t.Fatalf("the sign-in held the lock %d times, want 1", len(holds))
	}
}

// FindOrCreateAgent holds the identity first and the email second, under the
// fold owner binding compares under: the keys an asserted sign-in holds.
func TestFindOrCreateAgentHoldsTheIdentityThenTheFoldedEmail(t *testing.T) {
	s := newMemoryAuthStore()
	lock := newSharedSignInLock()
	svc := &newAccountSignalService{AuthenticationService: storeAuth{s: s}, credentials: storeCredentials{s: s}, lock: lock}
	id := dana(OAuthProviderGoogle, googleSub)
	id.Email = "  Dana.Whitfield@HarborLegal.example "

	if _, _, _, err := svc.FindOrCreateAgent(context.Background(), userInfoFor(id)); err != nil {
		t.Fatalf("FindOrCreateAgent: %v", err)
	}
	holds := lock.recordedHolds()
	want := []string{"identity\x00google\x00" + googleSub, "email\x00dana.whitfield@harborlegal.example"}
	if len(holds) != 1 || len(holds[0]) != 2 || holds[0][0] != want[0] || holds[0][1] != want[1] {
		t.Fatalf("holds = %q, want one hold of %q", holds, want)
	}
}

// A caller that holds the keys for one identity does not hold them for
// another: FindOrCreateAgent for a second identity with the same email still
// takes the lock.
func TestFindOrCreateAgentSkipsOnlyTheKeysItsCallerHolds(t *testing.T) {
	s := newMemoryAuthStore()
	lock := newSharedSignInLock()
	svc := &newAccountSignalService{AuthenticationService: storeAuth{s: s}, credentials: storeCredentials{s: s}, lock: lock}
	held := withSignInKeysHeld(context.Background(),
		signInLockKeys(OAuthProviderGoogle, googleSub, "dana.whitfield@harborlegal.example"))

	if _, _, _, err := svc.FindOrCreateAgent(held, userInfoFor(dana(OAuthProviderGoogle, googleSub))); err != nil {
		t.Fatalf("FindOrCreateAgent for the held identity: %v", err)
	}
	if holds := lock.recordedHolds(); len(holds) != 0 {
		t.Fatalf("FindOrCreateAgent took the lock its caller holds: %q", holds)
	}
	if _, _, _, err := svc.FindOrCreateAgent(held, userInfoFor(dana(OAuthProviderApple, appleSub))); err != nil {
		t.Fatalf("FindOrCreateAgent for another identity: %v", err)
	}
	if holds := lock.recordedHolds(); len(holds) != 1 {
		t.Fatalf("FindOrCreateAgent for an identity its caller does not hold took the lock %d times, want 1", len(holds))
	}
}

// A callback that cannot hold the lock writes nothing: going on without it is
// the race the lock exists to stop.
func TestFindOrCreateAgentCreatesNobodyWhenTheLockCannotBeHeld(t *testing.T) {
	s := newMemoryAuthStore()
	svc := &newAccountSignalService{AuthenticationService: storeAuth{s: s}, credentials: storeCredentials{s: s}, lock: failingSignInLock{}}

	if _, _, _, err := svc.FindOrCreateAgent(context.Background(), userInfoFor(dana(OAuthProviderGoogle, googleSub))); err == nil {
		t.Fatal("a FindOrCreateAgent that could not hold the lock succeeded")
	}
	if s.createCount() != 0 {
		t.Fatalf("a FindOrCreateAgent that could not hold the lock created %d people", s.createCount())
	}
}

// The application's AuthenticationService holds the sign-in lock it is given,
// so both OAuth callbacks, which call FindOrCreateAgent on it, hold it.
func TestProvideAuthenticationServiceWiresTheSignInLock(t *testing.T) {
	svc := ProvideAuthenticationService(struct {
		fx.In
		Registry            authapp.OAuthProviderRegistry
		Agents              authrepos.AgentRepository
		Credentials         authrepos.CredentialRepository
		Sessions            authrepos.AuthSessionRepository
		Accounts            authrepos.AccountRepository
		PasswordCredentials authrepos.PasswordCredentialRepository
		AuthzChecker        *authcasbin.CasbinAuthorizationChecker
		EventStore          esdomain.EventStore       `optional:"true"`
		EventDispatcher     *esdomain.EventDispatcher `optional:"true"`
		JWTService          authapp.JWTService        `optional:"true"`
		SignInLock          repositories.SignInLock   `optional:"true"`
	}{
		Credentials: &fakeCredentialRepo{err: errors.New("the credential store was reached")},
		SignInLock:  failingSignInLock{},
	})

	_, _, _, err := svc.FindOrCreateAgent(context.Background(), userInfoFor(dana(OAuthProviderGoogle, googleSub)))
	if err == nil || !strings.Contains(err.Error(), "hold the sign-in lock") {
		t.Fatalf("FindOrCreateAgent = %v, want the error of a sign-in lock that could not be held", err)
	}
}

// More sign-ins at once than the pools have connections, on two replicas whose
// pools hold eight connections each, all finish, and each email ends with one
// person holding both of its identities. Each sign-in's lock keeps one
// connection while its work needs another, so without a bound on the lock
// holders a pool fills with holders that each wait for a second connection.
// The sign-ins reach FindOrCreateAgent through the locking AuthenticationService,
// as they do in the application. Set TEST_POSTGRES_DSN to run it.
func TestAssertedSignInsBeyondThePoolFinishWithOnePersonPerEmail_Postgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL sign-in burst is skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	open := func() *gorm.DB {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Fatalf("open PostgreSQL: %v", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("get the PostgreSQL pool: %v", err)
		}
		sqlDB.SetMaxOpenConns(8)
		t.Cleanup(func() { _ = sqlDB.Close() })
		return db
	}
	dbs := []*gorm.DB{open(), open()}
	if err := authgorm.AutoMigrate(dbs[0]); err != nil {
		t.Fatalf("migrate the auth tables: %v", err)
	}
	replicas := make([]*AssertedSignIn, len(dbs))
	for i, db := range dbs {
		creds, agents := authgorm.NewCredentialRepository(db), authgorm.NewAgentRepository(db)
		lock := weosgorm.ProvideSignInLock(db)
		replicas[i] = NewAssertedSignIn(AssertedSignInConfig{
			Auth: &newAccountSignalService{
				AuthenticationService: authapp.NewDefaultAuthenticationService(nil, agents, creds,
					authgorm.NewAuthSessionRepository(db), authgorm.NewAccountRepository(db)),
				credentials: creds,
				lock:        lock,
			},
			Credentials: creds,
			Agents:      agents,
			Emails:      weosgorm.ProvideCredentialEmailQuery(db),
			Lock:        lock,
		})
	}

	suffix := ksuid.New().String()
	type attempt struct {
		replica int
		id      AssertedIdentity
	}
	const owners = 12
	emails := make([]string, owners)
	var attempts []attempt
	for o := range emails {
		emails[o] = fmt.Sprintf("owner-%02d+%s@harborlegal.example", o, suffix)
		for i, provider := range []string{OAuthProviderGoogle, OAuthProviderApple, OAuthProviderGoogle, OAuthProviderApple} {
			attempts = append(attempts, attempt{replica: (o + i) % len(replicas), id: AssertedIdentity{
				Provider: provider, Subject: fmt.Sprintf("%s-%02d-%s", provider, o, suffix),
				Email: emails[o], Name: "Dana Whitfield",
			}})
		}
	}

	results := make([]AssertedSignInResult, len(attempts))
	errs := make([]error, len(attempts))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, a := range attempts {
		wg.Add(1)
		go func(i int, a attempt) {
			defer wg.Done()
			<-start
			results[i], errs[i] = replicas[a.replica].SignIn(ctx, a.id)
		}(i, a)
	}
	close(start)
	wg.Wait()
	t.Cleanup(func() {
		seen := map[string]bool{}
		for _, r := range results {
			if r.Agent == nil || seen[r.Agent.GetID()] {
				continue
			}
			seen[r.Agent.GetID()] = true
			agentID := r.Agent.GetID()
			rows := []struct{ table, column, value string }{
				{"credentials", "agent_id", agentID},
				{"account_members", "agent_id", agentID},
			}
			if r.Account != nil {
				rows = append(rows, struct{ table, column, value string }{"accounts", "id", r.Account.GetID()})
			}
			rows = append(rows, struct{ table, column, value string }{"agents", "id", agentID})
			for _, row := range rows {
				if err := dbs[0].Table(row.table).Where(row.column+" = ?", row.value).Delete(map[string]any{}).Error; err != nil {
					t.Errorf("clean up %s: %v", row.table, err)
				}
			}
		}
	})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("sign-in %d of %d: %v", i+1, len(attempts), err)
		}
	}

	byEmail := map[string][]AssertedSignInResult{}
	for i, a := range attempts {
		byEmail[a.id.Email] = append(byEmail[a.id.Email], results[i])
	}
	query := weosgorm.ProvideCredentialEmailQuery(dbs[0])
	for _, email := range emails {
		requireOnePersonOneCreate(t, byEmail[email])
		matches, err := query.CredentialsByEmail(ctx, email)
		if err != nil {
			t.Fatalf("read the credentials holding an email: %v", err)
		}
		if len(matches) != 2 {
			t.Fatalf("%d credentials hold an email, want its two identities", len(matches))
		}
		for _, m := range matches {
			if m.AgentID != byEmail[email][0].Agent.GetID() {
				t.Fatalf("a credential for an email belongs to %s, want %s: the burst made two people", m.AgentID, byEmail[email][0].Agent.GetID())
			}
		}
	}
}
