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
	"testing"
	"time"
)

// wm-3dgs0. An app whose renewal answer was lost sends the same refresh token
// again. Inside a short grace window that repeat gets the successor the first
// renewal made, not a family revocation. No failure message prints a raw token.

// nativeGraceTestKey is the successor key the grace tests derive successors with.
var nativeGraceTestKey = []byte("the successor key a grace test holds")

// spendNativeRefreshTokenForGrace issues a native refresh token, rotates it with
// nativeGraceTestKey, and answers the spent row as the store now keeps it, its
// raw value, and the successor.
func spendNativeRefreshTokenForGrace(t *testing.T, repo RefreshTokenRepository) (*OAuthRefreshToken, string, NativeRefreshToken) {
	t.Helper()
	ctx := context.Background()
	issued, err := IssueNativeRefreshToken(ctx, repo, "agent-ops", "acct-harbor", "")
	mustNoErr(t, err, "issue a native refresh token")
	stored, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the native refresh token")
	next, err := RotateNativeRefreshToken(ctx, repo, stored, issued.Raw, nativeGraceTestKey)
	mustNoErr(t, err, "rotate the native refresh token")
	spent, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the spent refresh token")
	return spent, issued.Raw, next
}

func TestNativeRefreshSuccessorInGrace_AnswersTheSameSuccessorOnlyInsideTheWindow(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	spent, raw, next := spendNativeRefreshTokenForGrace(t, repo)

	if !spent.Revoked || spent.RotatedAt == nil || spent.SuccessorID == "" {
		t.Fatalf("the spent token is revoked %v, records when it was rotated %v, names its successor %v; want all three",
			spent.Revoked, spent.RotatedAt != nil, spent.SuccessorID != "")
	}
	successorRow, err := repo.FindByTokenHash(ctx, HashToken(next.Raw))
	mustNoErr(t, err, "read the successor by its hash")
	if successorRow.ID != spent.SuccessorID {
		t.Fatal("the spent token names a successor other than the one the rotation made")
	}
	if successorRow.TokenHash == next.Raw || spent.SuccessorID == next.Raw {
		t.Fatal("the store keeps the successor's raw value; it must keep only its hash and its id")
	}

	rotatedAt := *spent.RotatedAt
	inside := rotatedAt.Add(NativeRefreshGraceWindow - time.Second)
	outside := rotatedAt.Add(NativeRefreshGraceWindow + time.Second)

	successor, refresh, ok, err := NativeRefreshSuccessorInGrace(ctx, repo, spent, raw, nativeGraceTestKey, inside, NativeRefreshGraceWindow)
	mustNoErr(t, err, "look for the successor inside the window")
	if !ok || refresh.Raw != next.Raw || successor == nil || successor.ID != successorRow.ID {
		t.Fatalf("inside the window: found %v, same refresh token %v, same successor %v; want all three",
			ok, refresh.Raw == next.Raw, successor != nil && successor.ID == successorRow.ID)
	}
	if !withinASecond(refresh.ExpiresAt, successorRow.ExpiresAt) {
		t.Fatalf("the repeat reports expiry %v, the successor expires at %v", refresh.ExpiresAt, successorRow.ExpiresAt)
	}

	if _, _, ok, err := NativeRefreshSuccessorInGrace(ctx, repo, spent, raw, nativeGraceTestKey, outside, NativeRefreshGraceWindow); ok || err != nil {
		t.Fatalf("outside the window: found %v (error %v), want not found", ok, err)
	}
	if _, _, ok, err := NativeRefreshSuccessorInGrace(ctx, repo, spent, raw, []byte("another key"), inside, NativeRefreshGraceWindow); ok || err != nil {
		t.Fatalf("with another successor key: found %v (error %v), want not found", ok, err)
	}
	if _, _, ok, err := NativeRefreshSuccessorInGrace(ctx, repo, spent, raw, nativeGraceTestKey, rotatedAt, 0); ok || err != nil {
		t.Fatalf("with no grace window: found %v (error %v), want not found", ok, err)
	}

	// A family that was revoked gets no successor, however soon the repeat.
	mustNoErr(t, repo.RevokeFamily(ctx, spent.FamilyID), "revoke the family")
	if _, _, ok, err := NativeRefreshSuccessorInGrace(ctx, repo, spent, raw, nativeGraceTestKey, inside, NativeRefreshGraceWindow); ok || err != nil {
		t.Fatalf("for a revoked family: found %v (error %v), want not found", ok, err)
	}
}

// A successor that was itself spent is not handed out again: the repeat of the
// token before it is reuse, however soon it comes.
func TestNativeRefreshSuccessorInGrace_NeverAnswersASuccessorThatWasItselfSpent(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	spent, raw, next := spendNativeRefreshTokenForGrace(t, repo)

	successorRow, err := repo.FindByTokenHash(ctx, HashToken(next.Raw))
	mustNoErr(t, err, "read the successor")
	if _, err := RotateNativeRefreshToken(ctx, repo, successorRow, next.Raw, nativeGraceTestKey); err != nil {
		t.Fatalf("rotate the successor: %v", err)
	}
	if _, _, ok, err := NativeRefreshSuccessorInGrace(ctx, repo, spent, raw, nativeGraceTestKey, *spent.RotatedAt, NativeRefreshGraceWindow); ok || err != nil {
		t.Fatalf("for a successor that was spent: found %v (error %v), want not found", ok, err)
	}
}
