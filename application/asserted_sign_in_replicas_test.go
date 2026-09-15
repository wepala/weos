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
	"os"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"
	weosgorm "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authgorm "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/database/gorm"
	"github.com/segmentio/ksuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// sharedSignInLock stands in for a lock on the database two replicas share. It
// records the keys of every Hold, and signals waiting each time a Hold finds a
// key another holder has.
type sharedSignInLock struct {
	mu      sync.Mutex
	keys    map[string]chan struct{}
	holds   [][]string
	waiting chan struct{}
}

func newSharedSignInLock() *sharedSignInLock {
	return &sharedSignInLock{keys: map[string]chan struct{}{}, waiting: make(chan struct{}, 1)}
}

func (l *sharedSignInLock) Hold(ctx context.Context, keys ...string) (func(), error) {
	l.mu.Lock()
	l.holds = append(l.holds, keys)
	slots := make([]chan struct{}, len(keys))
	for i, key := range keys {
		if l.keys[key] == nil {
			l.keys[key] = make(chan struct{}, 1)
		}
		slots[i] = l.keys[key]
	}
	l.mu.Unlock()

	release := func(taken []chan struct{}) {
		for i := len(taken) - 1; i >= 0; i-- {
			<-taken[i]
		}
	}
	for i, slot := range slots {
		select {
		case slot <- struct{}{}:
			continue
		default:
		}
		select {
		case l.waiting <- struct{}{}:
		default:
		}
		select {
		case slot <- struct{}{}:
		case <-ctx.Done():
			release(slots[:i])
			return nil, ctx.Err()
		}
	}
	return func() { release(slots) }, nil
}

func (l *sharedSignInLock) recordedHolds() [][]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([][]string(nil), l.holds...)
}

// pausingEmails answers the owner look-up, then waits for proceed before it
// returns, once: a replica that has read "nobody holds this email" and not yet
// created anyone.
type pausingEmails struct {
	repositories.CredentialEmailQuery
	once    sync.Once
	paused  chan struct{}
	proceed chan struct{}
}

func newPausingEmails(inner repositories.CredentialEmailQuery) *pausingEmails {
	return &pausingEmails{CredentialEmailQuery: inner, paused: make(chan struct{}), proceed: make(chan struct{})}
}

func (q *pausingEmails) CredentialsByEmail(ctx context.Context, email string) ([]repositories.CredentialEmailMatch, error) {
	matches, err := q.CredentialEmailQuery.CredentialsByEmail(ctx, email)
	q.once.Do(func() {
		close(q.paused)
		<-q.proceed
	})
	return matches, err
}

type signInOutcome struct {
	result AssertedSignInResult
	err    error
}

// interleaveReplicas runs the first replica's sign-in until it has read that
// nobody holds the email, starts the second replica's sign-in, waits for
// waited to report that the second waits on the first, and only then lets the
// first go on. It fails the test when the second replica finishes, or never
// waits, while the first is paused.
func interleaveReplicas(ctx context.Context, t *testing.T, paused *pausingEmails,
	first, second func() (AssertedSignInResult, error), waited func(context.Context),
) (AssertedSignInResult, AssertedSignInResult) {
	t.Helper()
	proceed := sync.OnceFunc(func() { close(paused.proceed) })
	t.Cleanup(proceed)

	firstDone, secondDone := make(chan signInOutcome, 1), make(chan signInOutcome, 1)
	go func() {
		r, err := first()
		firstDone <- signInOutcome{r, err}
	}()
	select {
	case <-paused.paused:
	case o := <-firstDone:
		t.Fatalf("the first replica finished (%v) before it read the owner look-up", o.err)
	case <-ctx.Done():
		t.Fatal("the first replica never reached the owner look-up")
	}

	go func() {
		r, err := second()
		secondDone <- signInOutcome{r, err}
	}()
	waitedCtx, cancelWait := context.WithCancel(ctx)
	waitReturned := make(chan struct{})
	go func() {
		defer close(waitReturned)
		waited(waitedCtx)
	}()
	select {
	case <-waitReturned:
	case o := <-secondDone:
		cancelWait()
		<-waitReturned
		t.Fatalf("the second replica finished (reached %v, err %v) while the first had read that nobody holds the email and created nobody yet: replicas sharing a database each create a person",
			agentIDOf(o.result), o.err)
	}
	cancelWait()
	if ctx.Err() != nil {
		t.Fatal("the second replica never waited on the first")
	}

	proceed()
	var outcomes [2]signInOutcome
	for i, done := range []chan signInOutcome{firstDone, secondDone} {
		select {
		case outcomes[i] = <-done:
		case <-ctx.Done():
			t.Fatalf("replica %d never finished its sign-in", i+1)
		}
		if outcomes[i].err != nil {
			t.Fatalf("replica %d's sign-in: %v", i+1, outcomes[i].err)
		}
	}
	return outcomes[0].result, outcomes[1].result
}

func agentIDOf(r AssertedSignInResult) string {
	if r.Agent == nil {
		return "nobody"
	}
	return r.Agent.GetID()
}

// Two replicas share one store and one database lock, but not their in-process
// locks. The first reads that nobody holds the email and pauses before it
// creates the person. The second, arriving with the owner's other identity,
// must wait for the first, then link to the person the first created.
func TestAssertedSignInReplicasSharingALockLeaveOnePerson(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := newMemoryAuthStore()
	lock := newSharedSignInLock()
	paused := newPausingEmails(storeEmails{s: s})
	first := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.Emails = paused
		cfg.Lock = lock
	})
	second := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) { cfg.Lock = lock })

	a, b := interleaveReplicas(ctx, t, paused,
		func() (AssertedSignInResult, error) { return first.SignIn(ctx, dana("google", googleSub)) },
		func() (AssertedSignInResult, error) { return second.SignIn(ctx, dana("apple", appleSub)) },
		func(ctx context.Context) {
			select {
			case <-lock.waiting:
			case <-ctx.Done():
			}
		})

	if s.createCount() != 1 {
		t.Fatalf("two replicas created %d people for one owner's two identities, want 1", s.createCount())
	}
	requireOnePersonOneCreate(t, []AssertedSignInResult{a, b})
	if cred := s.credentialFor("apple", appleSub); cred == nil || cred.AgentID() != a.Agent.GetID() {
		t.Fatalf("the second replica's identity was not linked to the person the first created")
	}
}

// The lock takes the identity first and the email second, the order the
// in-process locks take them, and the email under the fold the owner look-up
// compares under.
func TestAssertedSignInHoldsTheIdentityThenTheFoldedEmail(t *testing.T) {
	s := newMemoryAuthStore()
	lock := newSharedSignInLock()
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) { cfg.Lock = lock })
	id := dana("google", googleSub)
	id.Email = "  Dana.Whitfield@HarborLegal.example "

	if _, err := svc.SignIn(context.Background(), id); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	holds := lock.recordedHolds()
	want := []string{"identity\x00google\x00" + googleSub, "email\x00dana.whitfield@harborlegal.example"}
	if len(holds) != 1 || len(holds[0]) != 2 || holds[0][0] != want[0] || holds[0][1] != want[1] {
		t.Fatalf("holds = %q, want one hold of %q", holds, want)
	}
}

// The application wires the lock it is given: without it, an instance with
// replicas would be back to serializing in process only.
func TestProvideAssertedSignInWiresTheSignInLock(t *testing.T) {
	s := newMemoryAuthStore()
	lock := newSharedSignInLock()

	if _, err := provideOverWithLock(s, config.Config{}, lock).SignIn(context.Background(), dana("google", googleSub)); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if holds := lock.recordedHolds(); len(holds) != 1 {
		t.Fatalf("the wired sign-in held the lock %d times, want 1", len(holds))
	}
}

type failingSignInLock struct{}

func (failingSignInLock) Hold(context.Context, ...string) (func(), error) {
	return nil, errors.New("connection refused")
}

// A sign-in that cannot hold the lock reads nothing and creates nobody: going
// on without it is the race the lock exists to stop.
func TestAssertedSignInCreatesNobodyWhenTheLockCannotBeHeld(t *testing.T) {
	s := newMemoryAuthStore()
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) { cfg.Lock = failingSignInLock{} })

	if _, err := svc.SignIn(context.Background(), dana("google", googleSub)); err == nil {
		t.Fatal("a sign-in that could not hold the lock succeeded")
	}
	if s.createCount() != 0 {
		t.Fatalf("a sign-in that could not hold the lock created %d people", s.createCount())
	}
}

// The interleaving the test above stands in for, on PostgreSQL, with pericarp's
// real repositories and FindOrCreateAgent: two pools against one database are
// two replicas. Set TEST_POSTGRES_DSN to run it.
func TestAssertedSignInReplicasSharingPostgresLeaveOnePerson_Postgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL replicas are skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		t.Cleanup(func() { _ = sqlDB.Close() })
		return db
	}
	firstDB, secondDB := open(), open()
	if err := authgorm.AutoMigrate(firstDB); err != nil {
		t.Fatalf("migrate the auth tables: %v", err)
	}

	suffix := ksuid.New().String()
	email := "dana.whitfield+" + suffix + "@harborlegal.example"
	replica := func(db *gorm.DB, emails repositories.CredentialEmailQuery) *AssertedSignIn {
		creds, agents := authgorm.NewCredentialRepository(db), authgorm.NewAgentRepository(db)
		return NewAssertedSignIn(AssertedSignInConfig{
			Auth: authapp.NewDefaultAuthenticationService(nil, agents, creds,
				authgorm.NewAuthSessionRepository(db), authgorm.NewAccountRepository(db)),
			Credentials: creds,
			Agents:      agents,
			Emails:      emails,
			Lock:        weosgorm.ProvideSignInLock(db),
		})
	}
	paused := newPausingEmails(weosgorm.ProvideCredentialEmailQuery(firstDB))
	first, second := replica(firstDB, paused), replica(secondDB, weosgorm.ProvideCredentialEmailQuery(secondDB))
	identity := func(provider string) AssertedIdentity {
		return AssertedIdentity{Provider: provider, Subject: provider + "-" + suffix, Email: email, Name: "Dana Whitfield"}
	}

	a, b := interleaveReplicas(ctx, t, paused,
		func() (AssertedSignInResult, error) { return first.SignIn(ctx, identity(OAuthProviderGoogle)) },
		func() (AssertedSignInResult, error) { return second.SignIn(ctx, identity(OAuthProviderApple)) },
		func(ctx context.Context) { waitForAnAdvisoryWait(ctx, t, firstDB) })
	t.Cleanup(func() {
		agentID := a.Agent.GetID()
		for _, stmt := range []struct{ table, column, value string }{
			{"credentials", "agent_id", agentID},
			{"account_members", "agent_id", agentID},
			{"accounts", "id", a.Account.GetID()},
			{"agents", "id", agentID},
		} {
			if err := firstDB.Table(stmt.table).Where(stmt.column+" = ?", stmt.value).Delete(map[string]any{}).Error; err != nil {
				t.Errorf("clean up %s: %v", stmt.table, err)
			}
		}
	})

	requireOnePersonOneCreate(t, []AssertedSignInResult{a, b})
	matches, err := weosgorm.ProvideCredentialEmailQuery(firstDB).CredentialsByEmail(ctx, email)
	if err != nil {
		t.Fatalf("read the credentials holding the email: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("%d credentials hold the email, want the two identities of one person", len(matches))
	}
	for _, m := range matches {
		if m.AgentID != a.Agent.GetID() {
			t.Fatalf("a credential for the email belongs to %s, want %s: the replicas made two people", m.AgentID, a.Agent.GetID())
		}
	}
}

// waitForAnAdvisoryWait returns once a session on db's database waits on an
// advisory lock, or when ctx ends.
func waitForAnAdvisoryWait(ctx context.Context, t *testing.T, db *gorm.DB) {
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting int64
		if err := db.WithContext(ctx).Raw(
			"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND wait_event = 'advisory'",
		).Scan(&waiting).Error; err != nil {
			if ctx.Err() == nil {
				t.Errorf("read PostgreSQL's lock waits: %v", err)
			}
			return
		}
		if waiting > 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
