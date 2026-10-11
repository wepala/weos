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
	"errors"
	"testing"
	"time"
)

// wm-xbd5h. A sign-in whose session has a fixed end — a downstream binary's
// 24-hour sign-in — may cap the native refresh token family it starts. The
// first token expires at the cap, and no rotation hands back a successor that
// outlives it: renewing keeps the hour-long access token alive inside the
// session, never past it.

func TestIssueNativeRefreshTokenUntil_ExpiresTheFamilyAtTheCap(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	capAt := time.Now().Add(24 * time.Hour)

	issued, err := IssueNativeRefreshTokenUntil(ctx, repo, "agent-ops", "acct-harbor", "", capAt)
	mustNoErr(t, err, "issue a capped native refresh token")
	if !withinASecond(issued.ExpiresAt, capAt) {
		t.Fatalf("the capped refresh token expires at %v, want the cap %v", issued.ExpiresAt, capAt)
	}
	stored, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the capped refresh token back")
	if !withinASecond(stored.ExpiresAt, capAt) {
		t.Fatalf("the store keeps expiry %v, want the cap %v", stored.ExpiresAt, capAt)
	}
	if stored.FamilyExpiresAt == nil || !withinASecond(*stored.FamilyExpiresAt, capAt) {
		t.Fatalf("the store keeps family cap %v, want %v", stored.FamilyExpiresAt, capAt)
	}
}

func TestRotateNativeRefreshToken_TheSuccessorInheritsTheFamilyCap(t *testing.T) {
	for _, withKey := range []bool{false, true} {
		name := "random successor"
		var key []byte
		if withKey {
			name, key = "derived successor", []byte("a-successor-key-for-this-test")
		}
		t.Run(name, func(t *testing.T) {
			db := setupTestDB(t)
			repo := NewRefreshTokenRepository(db)
			ctx := context.Background()
			capAt := time.Now().Add(24 * time.Hour)

			issued, err := IssueNativeRefreshTokenUntil(ctx, repo, "agent-ops", "acct-harbor", "", capAt)
			mustNoErr(t, err, "issue a capped native refresh token")
			raw := issued.Raw
			for i := range 3 {
				stored, err := repo.FindByTokenHash(ctx, HashToken(raw))
				mustNoErr(t, err, "read the refresh token before its rotation")
				next, err := RotateNativeRefreshToken(ctx, repo, stored, raw, key)
				mustNoErr(t, err, "rotate the capped refresh token")
				if !withinASecond(next.ExpiresAt, capAt) {
					t.Fatalf("rotation %d handed back a successor expiring at %v, want the family cap %v, not %v from now",
						i+1, next.ExpiresAt, capAt, NativeRefreshTokenTTL)
				}
				successor, err := repo.FindByTokenHash(ctx, HashToken(next.Raw))
				mustNoErr(t, err, "read the successor back")
				if successor.FamilyExpiresAt == nil || !withinASecond(*successor.FamilyExpiresAt, capAt) {
					t.Fatalf("rotation %d stored family cap %v on the successor, want %v", i+1, successor.FamilyExpiresAt, capAt)
				}
				if !withinASecond(successor.ExpiresAt, capAt) {
					t.Fatalf("rotation %d stored expiry %v on the successor, want %v", i+1, successor.ExpiresAt, capAt)
				}
				raw = next.Raw
			}
		})
	}
}

// A family with no cap renews as it always did: each successor gets the full
// lifetime from its rotation, and carries no cap.
func TestRotateNativeRefreshToken_AnUncappedFamilyKeepsTheFullLifetime(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	issued, err := IssueNativeRefreshToken(ctx, repo, "agent-ops", "acct-harbor", "")
	mustNoErr(t, err, "issue a native refresh token")
	stored, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the refresh token back")
	if stored.FamilyExpiresAt != nil {
		t.Fatalf("an uncapped issuance stored family cap %v, want none", *stored.FamilyExpiresAt)
	}
	before := time.Now()
	next, err := RotateNativeRefreshToken(ctx, repo, stored, issued.Raw, nil)
	mustNoErr(t, err, "rotate the refresh token")
	if next.ExpiresAt.Before(before.Add(NativeRefreshTokenTTL).Add(-time.Second)) {
		t.Fatalf("the uncapped successor expires at %v, want %v from the rotation", next.ExpiresAt, NativeRefreshTokenTTL)
	}
	successor, err := repo.FindByTokenHash(ctx, HashToken(next.Raw))
	mustNoErr(t, err, "read the successor back")
	if successor.FamilyExpiresAt != nil {
		t.Fatalf("the uncapped successor carries family cap %v, want none", *successor.FamilyExpiresAt)
	}
}

// A cap further off than the full lifetime does not lengthen the token: it
// expires after the full lifetime, and the cap is still kept for the family.
func TestIssueNativeRefreshTokenUntil_ACapPastTheLifetimeDoesNotLengthenTheToken(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	capAt := time.Now().Add(NativeRefreshTokenTTL + 10*24*time.Hour)

	before := time.Now()
	issued, err := IssueNativeRefreshTokenUntil(ctx, repo, "agent-ops", "acct-harbor", "", capAt)
	mustNoErr(t, err, "issue a native refresh token with a distant cap")
	if !withinASecond(issued.ExpiresAt, before.Add(NativeRefreshTokenTTL)) {
		t.Fatalf("the token expires at %v, want the full lifetime %v from now", issued.ExpiresAt, NativeRefreshTokenTTL)
	}
	stored, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the refresh token back")
	if stored.FamilyExpiresAt == nil || !withinASecond(*stored.FamilyExpiresAt, capAt) {
		t.Fatalf("the store keeps family cap %v, want %v", stored.FamilyExpiresAt, capAt)
	}
}

// A cap that has already passed would start a family that can never renew,
// so nothing is issued.
func TestIssueNativeRefreshTokenUntil_RefusesACapThatHasPassed(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	if _, err := IssueNativeRefreshTokenUntil(ctx, repo, "agent-ops", "acct-harbor", "rt-late", time.Now().Add(-time.Minute)); err == nil {
		t.Fatal("a refresh token was issued with a cap that has passed")
	}
	var rows int64
	if err := db.Model(&OAuthRefreshToken{}).Count(&rows).Error; err != nil {
		t.Fatalf("count the refresh tokens: %v", err)
	}
	if rows != 0 {
		t.Fatalf("the store holds %d refresh token(s) after a refused issuance, want 0", rows)
	}
}

// PR 576 review. A rotation that runs after its family's cap has passed writes
// no successor — one would be dead on arrival — and answers
// ErrNativeRefreshCapPassed, so the caller refuses the renewal and nothing is
// spent.
func TestRotateNativeRefreshToken_RefusesAFamilyWhoseCapHasPassed(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	capAt := time.Now().Add(200 * time.Millisecond)

	issued, err := IssueNativeRefreshTokenUntil(ctx, repo, "agent-ops", "acct-harbor", "", capAt)
	mustNoErr(t, err, "issue a capped native refresh token")
	stored, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the capped refresh token back")
	time.Sleep(time.Until(capAt) + 20*time.Millisecond)

	if _, err := RotateNativeRefreshToken(ctx, repo, stored, issued.Raw, nil); !errors.Is(err, ErrNativeRefreshCapPassed) {
		t.Fatalf("rotating past the family cap answered %v, want ErrNativeRefreshCapPassed", err)
	}
	again, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the refresh token after the refused rotation")
	if again.Revoked {
		t.Fatal("a rotation refused past the family cap spent the refresh token")
	}
	var rows int64
	mustNoErr(t, db.Model(&OAuthRefreshToken{}).Count(&rows).Error, "count refresh tokens")
	if rows != 1 {
		t.Fatalf("the store holds %d refresh token(s), want the presented one alone", rows)
	}
}
