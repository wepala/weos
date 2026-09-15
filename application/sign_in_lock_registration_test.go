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
	"path/filepath"
	"strings"
	"testing"
	"time"

	weosgorm "github.com/wepala/weos/v3/infrastructure/database/gorm"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"gorm.io/gorm"
)

// pausingRegistration stands in for pericarp's RegisterPassword over the memory
// store: it refuses an email a password credential holds, and otherwise creates
// a person with a password credential. With a pause, the first registration
// that finds the email free pauses before it creates the person, as one that
// has read "this email is not registered" and written nothing.
type pausingRegistration struct {
	storeAuth
	pause *pausePoint
}

func (a pausingRegistration) RegisterPassword(ctx context.Context, email, displayName, _ string) (*entities.Agent, *entities.Credential, *entities.Account, error) {
	subject := strings.ToLower(strings.TrimSpace(email))
	a.s.mu.Lock()
	registered := a.s.findByProvider(entities.ProviderPassword, subject) != nil
	a.s.mu.Unlock()
	if registered {
		return nil, nil, nil, authapp.ErrEmailAlreadyTaken
	}
	if a.pause != nil {
		a.pause.hold()
	}
	return a.FindOrCreateAgent(ctx, authapp.UserInfo{
		Provider: entities.ProviderPassword, ProviderUserID: subject, Email: subject, DisplayName: displayName,
	})
}

// registrationSignIn registers the identity's email with a password, the way
// POST /auth/register does, and reports what it reached as a sign-in.
func registrationSignIn(ctx context.Context, auth authapp.AuthenticationService, id AssertedIdentity) (AssertedSignInResult, error) {
	agent, credential, account, err := auth.RegisterPassword(ctx, id.Email, id.Name, "registration password")
	return AssertedSignInResult{Agent: agent, Credential: credential, Account: account, NewAccount: err == nil}, err
}

// Password registration creates a person outside owner binding. It holds the
// sign-in lock binding holds, so a door assertion for the same email that
// arrives while registration has read "this email is not registered" and
// created nobody waits for it. On an instance whose operator made every
// password account (PasswordOwnersProven), the assertion then links to the
// person registration created.
func TestPasswordRegistrationAndAnAssertionForOneEmailLeaveOnePerson(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := newMemoryAuthStore()
	lock := newSharedSignInLock()
	pause := newPausePoint()
	registration := &newAccountSignalService{
		AuthenticationService: pausingRegistration{storeAuth: storeAuth{s: s}, pause: pause},
		credentials:           storeCredentials{s: s},
		lock:                  lock,
	}
	assertion := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.Lock = lock
		cfg.PasswordOwnersProven = true
	})
	id := dana(OAuthProviderDoor, doorSub)

	a, b := interleaveReplicas(ctx, t, pause,
		func() (AssertedSignInResult, error) { return registrationSignIn(ctx, registration, id) },
		func() (AssertedSignInResult, error) { return assertion.SignIn(ctx, id) },
		func(ctx context.Context) {
			select {
			case <-lock.waiting:
			case <-ctx.Done():
			}
		})

	if s.createCount() != 1 {
		t.Fatalf("a registration and an assertion created %d people for one email, want 1", s.createCount())
	}
	requireOnePersonOneCreate(t, []AssertedSignInResult{a, b})
	if cred := s.credentialFor(OAuthProviderDoor, doorSub); cred == nil || cred.AgentID() != a.Agent.GetID() {
		t.Fatal("the assertion's identity was not linked to the person registration created")
	}
}

// RegisterPassword holds the password identity first and the folded email
// second: the email key an asserted sign-in holds, in the same order.
func TestRegisterPasswordHoldsThePasswordIdentityThenTheFoldedEmail(t *testing.T) {
	s := newMemoryAuthStore()
	lock := newSharedSignInLock()
	svc := &newAccountSignalService{AuthenticationService: pausingRegistration{storeAuth: storeAuth{s: s}}, credentials: storeCredentials{s: s}, lock: lock}
	id := dana(OAuthProviderDoor, doorSub)
	id.Email = "  Dana.Whitfield@HarborLegal.example "

	if _, err := registrationSignIn(context.Background(), svc, id); err != nil {
		t.Fatalf("RegisterPassword: %v", err)
	}
	holds := lock.recordedHolds()
	want := []string{"identity\x00password\x00dana.whitfield@harborlegal.example", "email\x00dana.whitfield@harborlegal.example"}
	if len(holds) != 1 || len(holds[0]) != 2 || holds[0][0] != want[0] || holds[0][1] != want[1] {
		t.Fatalf("holds = %q, want one hold of %q", holds, want)
	}
}

// A registration that cannot hold the lock creates nobody.
func TestRegisterPasswordCreatesNobodyWhenTheLockCannotBeHeld(t *testing.T) {
	s := newMemoryAuthStore()
	svc := &newAccountSignalService{AuthenticationService: pausingRegistration{storeAuth: storeAuth{s: s}}, credentials: storeCredentials{s: s}, lock: failingSignInLock{}}

	if _, err := registrationSignIn(context.Background(), svc, dana(OAuthProviderDoor, doorSub)); err == nil {
		t.Fatal("a RegisterPassword that could not hold the lock succeeded")
	}
	if s.createCount() != 0 {
		t.Fatalf("a RegisterPassword that could not hold the lock created %d people", s.createCount())
	}
}

// A callback or a registration whose request has already ended creates nobody
// on SQLite, although every key is free. A free key and an ended request are
// both ready to the lock, so without its check after the keys are taken each
// attempt here would go on to create a person one time in four.
func TestAnEndedRequestCreatesNobodyThroughTheSQLiteLock(t *testing.T) {
	db, err := gorm.Open(weosgorm.DialectorForDSN(filepath.Join(t.TempDir(), "sign_in_lock.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get the sqlite pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	s := newMemoryAuthStore()
	svc := &newAccountSignalService{
		AuthenticationService: pausingRegistration{storeAuth: storeAuth{s: s}},
		credentials:           storeCredentials{s: s},
		lock:                  weosgorm.ProvideSignInLock(db),
	}
	ended, cancel := context.WithCancel(context.Background())
	cancel()

	for i := range 32 {
		if _, _, _, err := svc.FindOrCreateAgent(ended, userInfoFor(dana(OAuthProviderGoogle, googleSub))); !errors.Is(err, context.Canceled) {
			t.Fatalf("callback %d: FindOrCreateAgent = %v, want the ended request's error", i+1, err)
		}
		if _, err := registrationSignIn(ended, svc, dana(OAuthProviderDoor, doorSub)); !errors.Is(err, context.Canceled) {
			t.Fatalf("registration %d: RegisterPassword = %v, want the ended request's error", i+1, err)
		}
	}
	if s.createCount() != 0 {
		t.Fatalf("requests that had already ended created %d people", s.createCount())
	}
}
