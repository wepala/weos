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

package oauth

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/segmentio/ksuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// revocationsUnderTest are the two revocations a native sign-out makes: one
// device's session, and every native session of the person.
var revocationsUnderTest = []struct {
	name   string
	revoke func(ctx context.Context, repo RefreshTokenRepository, familyID, agentID string) error
}{
	{"RevokeFamily", func(ctx context.Context, repo RefreshTokenRepository, familyID, _ string) error {
		return repo.RevokeFamily(ctx, familyID)
	}},
	{"RevokeForAgent", func(ctx context.Context, repo RefreshTokenRepository, _, agentID string) error {
		return repo.RevokeForAgent(ctx, agentID, NativeClientID)
	}},
}

// activeInFamily counts the family's refresh tokens that are not revoked.
func activeInFamily(t *testing.T, db *gorm.DB, familyID string) int64 {
	t.Helper()
	var active int64
	mustNoErr(t, db.Model(&OAuthRefreshToken{}).Where("family_id = ? AND revoked = ?", familyID, false).Count(&active).Error,
		"count the family's active refresh tokens")
	return active
}

// wm-lnimb. A revocation returns only once no refresh token it names is active,
// even when a rotation saves a successor while the revocation runs. On
// PostgreSQL an update that waits on the row a rotation is spending never sees
// the successor that rotation commits (the _Postgres test below). SQLite runs
// one writer at a time, so here the successor is saved by the revocation's own
// update transaction, right after its update: what the caller sees is the same,
// an update that is done and an active successor.
func TestRefreshTokenRepo_RevocationEndsASuccessorSavedWhileItRan(t *testing.T) {
	for _, tc := range revocationsUnderTest {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			repo := NewRefreshTokenRepository(db)
			ctx := context.Background()
			_, err := IssueNativeRefreshToken(ctx, repo, "agent-ops", "acct-harbor", "rt-phone")
			mustNoErr(t, err, "issue the phone's refresh token")

			var pending atomic.Bool
			pending.Store(true)
			mustNoErr(t, db.Callback().Update().After("gorm:update").Register("test:successor-saved-mid-revocation",
				func(tx *gorm.DB) {
					if tx.Error != nil || !pending.CompareAndSwap(true, false) {
						return
					}
					successor := &OAuthRefreshToken{
						ID: "rt-phone-next", FamilyID: "rt-phone", AgentID: "agent-ops", AccountID: "acct-harbor",
						ClientID: NativeClientID, TokenHash: HashToken("phone-next-refresh-token"),
						ExpiresAt: time.Now().Add(NativeRefreshTokenTTL),
					}
					if err := tx.Session(&gorm.Session{NewDB: true}).Create(successor).Error; err != nil {
						_ = tx.AddError(err)
					}
				}), "register the successor saved mid-revocation")

			mustNoErr(t, tc.revoke(ctx, repo, "rt-phone", "agent-ops"), tc.name)
			if pending.Load() {
				t.Fatal("no successor was saved during the revocation: the test did not interleave")
			}
			if active := activeInFamily(t, db, "rt-phone"); active != 0 {
				t.Fatalf("%s returned with %d active refresh tokens in the family, want 0", tc.name, active)
			}
		})
	}
}

// wm-lnimb. The interleaving the SQLite test above stands in for, on
// PostgreSQL. A rotation spends the phone's refresh token and stops before it
// saves the successor, holding the spent row. A revocation starts and waits on
// that row. The rotation then commits its successor. The revocation must not
// return while that successor is active. Set TEST_POSTGRES_DSN to run it.
func TestRefreshTokenRepo_RevocationEndsASuccessorARotationCommitsWhileItWaits_Postgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL interleaving is skipped")
	}
	for _, tc := range revocationsUnderTest {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
			mustNoErr(t, err, "open PostgreSQL")
			sqlDB, err := db.DB()
			mustNoErr(t, err, "get the PostgreSQL pool")
			t.Cleanup(func() { _ = sqlDB.Close() })
			mustNoErr(t, db.AutoMigrate(&OAuthRefreshToken{}), "migrate the refresh token table")

			suffix := ksuid.New().String()
			familyID, agentID := "rt-phone-"+suffix, "agent-ops-"+suffix
			t.Cleanup(func() { _ = db.Where("family_id = ?", familyID).Delete(&OAuthRefreshToken{}).Error })
			repo := NewRefreshTokenRepository(db)
			issued, err := IssueNativeRefreshToken(ctx, repo, agentID, "acct-harbor", familyID)
			mustNoErr(t, err, "issue the phone's refresh token")
			stored, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
			mustNoErr(t, err, "read the phone's refresh token")

			holding, release := make(chan struct{}), make(chan struct{})
			releaseRotation := sync.OnceFunc(func() { close(release) })
			t.Cleanup(releaseRotation)
			var pending atomic.Bool
			pending.Store(true)
			mustNoErr(t, db.Callback().Create().Before("gorm:create").Register("test:hold-rotation",
				func(*gorm.DB) {
					if pending.CompareAndSwap(true, false) {
						close(holding)
						<-release
					}
				}), "register the held rotation")

			rotated := make(chan error, 1)
			go func() {
				_, err := RotateNativeRefreshToken(ctx, repo, stored, issued.Raw, nil)
				rotated <- err
			}()
			select {
			case <-holding:
			case err := <-rotated:
				t.Fatalf("the rotation finished (%v) before it held the spent row", err)
			case <-ctx.Done():
				t.Fatal("the rotation never reached its successor")
			}

			revoked := make(chan error, 1)
			go func() { revoked <- tc.revoke(ctx, repo, familyID, agentID) }()
			waitForALockWait(ctx, t, db, revoked)
			releaseRotation()

			mustNoErr(t, receiveWithin(ctx, t, rotated, "the held rotation"), "the held rotation")
			mustNoErr(t, receiveWithin(ctx, t, revoked, tc.name), tc.name)
			if active := activeInFamily(t, db, familyID); active != 0 {
				t.Fatalf("%s returned with %d active refresh tokens in the family, want 0", tc.name, active)
			}
		})
	}
}

// waitForALockWait returns once a statement on db's database waits on a lock.
// It fails the test when done delivers first — the statement never waited — or
// ctx ends.
func waitForALockWait(ctx context.Context, t *testing.T, db *gorm.DB, done <-chan error) {
	t.Helper()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting int64
		mustNoErr(t, db.WithContext(ctx).Raw(
			"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'",
		).Scan(&waiting).Error, "read PostgreSQL's lock waits")
		if waiting > 0 {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("the revocation finished (%v) without waiting on the rotation's row", err)
		case <-ctx.Done():
			t.Fatal("no statement waited on the rotation's row")
		case <-tick.C:
		}
	}
}

// receiveWithin answers what ch delivers, failing the test when ctx ends first.
func receiveWithin(ctx context.Context, t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		t.Fatalf("%s did not finish", what)
		return nil
	}
}
