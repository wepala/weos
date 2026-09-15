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

package gorm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/ksuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// On SQLite the lock serializes the holders of a key in this process and opens
// no transaction: a holder of other keys is not kept waiting, a second holder of
// the same key waits until the first releases it and gives up when its context
// ends, and a write through the gated database lands while the key is held.
func TestSignInLockOnSQLiteSerializesInProcessAndStallsNoWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := gorm.Open(DialectorForDSN(filepath.Join(t.TempDir(), "sign_in_lock.db")), gormConfig())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get the sqlite pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`CREATE TABLE credentials (id TEXT PRIMARY KEY, email TEXT)`).Error; err != nil {
		t.Fatalf("create credentials: %v", err)
	}
	lock := ProvideSignInLock(db)
	email := "email\x00dana.whitfield@harborlegal.example"

	releaseFirst, err := lock.Hold(ctx, "identity\x00google\x00108234917650023841257", email)
	if err != nil {
		t.Fatalf("first Hold: %v", err)
	}
	firstHeld := true
	defer func() {
		if firstHeld {
			releaseFirst()
		}
	}()

	releaseOther, err := lock.Hold(ctx, "identity\x00google\x00118930044712", "email\x00ren.okafor@harborlegal.example")
	if err != nil {
		t.Fatalf("Hold on another identity and email: %v", err)
	}
	releaseOther()

	short, cancelShort := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelShort()
	if release, err := lock.Hold(short, "identity\x00apple\x00001482.7f3c", email); err == nil {
		release()
		t.Fatal("a second Hold on an email this process holds returned before the first released it")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a waiter whose context ended: %v, want the context's error", err)
	}

	written := make(chan error, 1)
	go func() {
		written <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO credentials (id, email) VALUES ('cred-dana', 'dana.whitfield@harborlegal.example')`).Error
		})
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("a write while the lock was held: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("a write waited on a SQLite sign-in lock")
	}

	held := make(chan func(), 1)
	failed := make(chan error, 1)
	go func() {
		release, err := lock.Hold(ctx, "identity\x00apple\x00001482.7f3c", email)
		if err != nil {
			failed <- err
			return
		}
		held <- release
	}()
	select {
	case release := <-held:
		release()
		t.Fatal("a second holder took the email while the first held it")
	case err := <-failed:
		t.Fatalf("the second holder's Hold: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	releaseFirst()
	firstHeld = false
	select {
	case release := <-held:
		release()
	case err := <-failed:
		t.Fatalf("the second holder's Hold: %v", err)
	case <-ctx.Done():
		t.Fatal("the second holder never took the email after the first released it")
	}

	signInLock := lock.(*SignInLock)
	signInLock.keys.mu.Lock()
	left := len(signInLock.keys.locks)
	signInLock.keys.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d keys are still tracked after every holder released and every waiter gave up", left)
	}
}

// A holder whose context has already ended holds nothing on SQLite, even when
// every key is free. A free key and an ended context are both ready, so without
// a check after the keys are taken a canceled sign-in would hold the lock and go
// on to write: each attempt here would then succeed one time in four.
func TestSignInLockOnSQLiteHoldsNothingForAnEndedContext(t *testing.T) {
	db, err := gorm.Open(DialectorForDSN(filepath.Join(t.TempDir(), "sign_in_lock.db")), gormConfig())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get the sqlite pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	lock := ProvideSignInLock(db)
	keys := []string{"identity\x00google\x00108234917650023841257", "email\x00dana.whitfield@harborlegal.example"}

	ended, cancel := context.WithCancel(context.Background())
	cancel()
	for i := range 64 {
		release, err := lock.Hold(ended, keys...)
		if err == nil {
			release()
			t.Fatalf("attempt %d: a Hold whose context had ended held the lock", i+1)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("attempt %d: %v, want the context's error", i+1, err)
		}
	}

	signInLock := lock.(*SignInLock)
	signInLock.keys.mu.Lock()
	left := len(signInLock.keys.locks)
	signInLock.keys.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d keys are still tracked after every Hold whose context had ended", left)
	}

	live, cancelLive := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelLive()
	release, err := lock.Hold(live, keys...)
	if err != nil {
		t.Fatalf("a Hold after the ended ones: %v", err)
	}
	release()
}

// Many holders of distinct and shared keys at once all finish, each shared key
// is held by one of them at a time, and nothing is left tracked.
func TestSignInLockSerializesEachKeyAcrossManyHolders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := gorm.Open(DialectorForDSN(filepath.Join(t.TempDir(), "sign_in_lock_many.db")), gormConfig())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get the sqlite pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	lock := ProvideSignInLock(db).(*SignInLock)

	var mu sync.Mutex
	inside := map[string]int{}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			email := fmt.Sprintf("email\x00owner-%d@harborlegal.example", i%4)
			release, err := lock.Hold(ctx, fmt.Sprintf("identity\x00google\x00%d", i), email)
			if err != nil {
				errs <- err
				return
			}
			mu.Lock()
			inside[email]++
			if inside[email] > 1 {
				errs <- fmt.Errorf("two holders held %q at once", email)
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			inside[email]--
			mu.Unlock()
			release()
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	lock.keys.mu.Lock()
	defer lock.keys.mu.Unlock()
	if len(lock.keys.locks) != 0 {
		t.Fatalf("%d keys are still tracked after every holder released", len(lock.keys.locks))
	}
}

// Advisory lock holders are bounded to a quarter of the pool, and at least one,
// so each holder's work always finds a connection. A pool of one connection
// has no room for a holder beside its work.
func TestSignInLockHoldersLeaveThePoolRoomForTheWork(t *testing.T) {
	for maxOpen, want := range map[int]int{0: 25, 1: 0, 2: 1, 7: 1, 8: 2, 10: 2, 100: 25} {
		if got := signInLockHolders(maxOpen); got != want {
			t.Errorf("signInLockHolders(%d) = %d, want %d", maxOpen, got, want)
		}
	}
}

// Two pools against one PostgreSQL database stand in for two replicas. A second
// replica taking a key the first holds waits until the first releases it, a
// replica taking another key does not wait, and a waiter whose context ends
// gives up holding nothing. Set TEST_POSTGRES_DSN to run it.
func TestSignInLockOnPostgresMakesASecondReplicaWait_Postgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL sign-in lock is skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first, second := openPostgresReplica(t, dsn), openPostgresReplica(t, dsn)
	suffix := ksuid.New().String()
	email := "email\x00dana.whitfield+" + suffix + "@harborlegal.example"

	releaseFirst, err := ProvideSignInLock(first).Hold(ctx, "identity\x00google\x00"+suffix, email)
	if err != nil {
		t.Fatalf("first replica's Hold: %v", err)
	}
	firstHeld := true
	defer func() {
		if firstHeld {
			releaseFirst()
		}
	}()

	// Another email is not kept waiting.
	releaseOther, err := ProvideSignInLock(second).Hold(ctx, "email\x00ren.okafor+"+suffix+"@harborlegal.example")
	if err != nil {
		t.Fatalf("Hold on another email: %v", err)
	}
	releaseOther()

	// A waiter whose context ends gives up and holds nothing.
	short, cancelShort := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancelShort()
	if release, err := ProvideSignInLock(second).Hold(short, "identity\x00apple\x00"+suffix, email); err == nil {
		release()
		t.Fatal("a Hold on a key another replica holds returned before that replica released it")
	}

	held := make(chan func(), 1)
	failed := make(chan error, 1)
	go func() {
		release, err := ProvideSignInLock(second).Hold(ctx, "identity\x00apple\x00"+suffix, email)
		if err != nil {
			failed <- err
			return
		}
		held <- release
	}()
	waitForAnAdvisoryLockWait(ctx, t, first)
	select {
	case release := <-held:
		release()
		t.Fatal("the second replica took the email while the first held it")
	case err := <-failed:
		t.Fatalf("the second replica's Hold: %v", err)
	default:
	}

	releaseFirst()
	firstHeld = false
	select {
	case release := <-held:
		release()
	case err := <-failed:
		t.Fatalf("the second replica's Hold: %v", err)
	case <-ctx.Done():
		t.Fatal("the second replica never took the email after the first released it")
	}
}

// On a pool of eight connections two sign-ins may hold advisory locks. A third
// waits holding no connection, and gives up when its context ends; the six
// connections the holders do not keep are all free for their work; and once a
// holder releases, the third takes its keys. A pool of one connection refuses
// to hold at all. Set TEST_POSTGRES_DSN to run it.
func TestSignInLockOnPostgresLeavesThePoolRoomForTheWork_Postgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL sign-in lock is skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := openPostgresReplica(t, dsn)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get the PostgreSQL pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	lock := ProvideSignInLock(db)
	suffix := ksuid.New().String()
	email := func(n int) string { return fmt.Sprintf("email\x00holder-%d+%s@harborlegal.example", n, suffix) }

	releaseFirst, err := lock.Hold(ctx, email(1))
	if err != nil {
		t.Fatalf("first Hold: %v", err)
	}
	firstHeld := true
	defer func() {
		if firstHeld {
			releaseFirst()
		}
	}()
	releaseSecond, err := lock.Hold(ctx, email(2))
	if err != nil {
		t.Fatalf("second Hold: %v", err)
	}
	defer releaseSecond()

	short, cancelShort := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancelShort()
	if release, err := lock.Hold(short, email(3)); err == nil {
		release()
		t.Fatal("a third sign-in held advisory locks on a pool that allows two holders")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a third sign-in whose context ended: %v, want the context's error", err)
	}
	if inUse := sqlDB.Stats().InUse; inUse != 2 {
		t.Fatalf("%d connections are in use with two holders and one waiter, want the holders' 2", inUse)
	}

	work, cancelWork := context.WithTimeout(ctx, 5*time.Second)
	defer cancelWork()
	conns := make([]interface{ Close() error }, 0, 6)
	for i := 0; i < 6; i++ {
		conn, err := sqlDB.Conn(work)
		if err != nil {
			t.Fatalf("work connection %d of the 6 the holders leave free: %v", i+1, err)
		}
		conns = append(conns, conn)
	}
	for _, conn := range conns {
		_ = conn.Close()
	}

	held := make(chan func(), 1)
	failed := make(chan error, 1)
	go func() {
		release, err := lock.Hold(ctx, email(3))
		if err != nil {
			failed <- err
			return
		}
		held <- release
	}()
	releaseFirst()
	firstHeld = false
	select {
	case release := <-held:
		release()
	case err := <-failed:
		t.Fatalf("the third sign-in's Hold: %v", err)
	case <-ctx.Done():
		t.Fatal("the third sign-in never held its keys after a holder released")
	}

	one := openPostgresReplica(t, dsn)
	oneDB, err := one.DB()
	if err != nil {
		t.Fatalf("get the one-connection pool: %v", err)
	}
	oneDB.SetMaxOpenConns(1)
	if release, err := ProvideSignInLock(one).Hold(ctx, email(4)); err == nil {
		release()
		t.Fatal("a pool of one connection held a sign-in lock its sign-in could not work beside")
	}
}

// openPostgresReplica opens its own pool against dsn, as a second replica does.
func openPostgresReplica(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
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

// waitForAnAdvisoryLockWait returns once a session on db's database waits on an
// advisory lock, and fails the test when ctx ends first.
func waitForAnAdvisoryLockWait(ctx context.Context, t *testing.T, db *gorm.DB) {
	t.Helper()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting int64
		if err := db.WithContext(ctx).Raw(
			"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND wait_event = 'advisory'",
		).Scan(&waiting).Error; err != nil {
			t.Fatalf("read PostgreSQL's lock waits: %v", err)
		}
		if waiting > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("no session waited on the sign-in lock")
		case <-tick.C:
		}
	}
}
