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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/segmentio/ksuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// On SQLite the lock holds nothing: a second holder of the same key is not
// kept waiting, and a write through the gated database lands while both hold
// it, because no transaction was opened to take the write gate.
func TestSignInLockOnSQLiteHoldsNothingAndStallsNoWrite(t *testing.T) {
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
	defer releaseFirst()
	releaseSecond, err := lock.Hold(ctx, "identity\x00apple\x00001482.7f3c", email)
	if err != nil {
		t.Fatalf("second Hold: %v", err)
	}
	defer releaseSecond()

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
