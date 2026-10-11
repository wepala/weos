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

	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
)

// wm-vdax2, wm-5u9q7. A capped native session ends at its cap: the access
// token a sign-in or a renewal hands back expires at the cap when the cap comes
// before its hour, and a cap that a pre-cap binary's rotation left off a row is
// read back from the rest of the family.

func capTestAgentAndAccount(t *testing.T) (*entities.Agent, *entities.Account) {
	t.Helper()
	agent, err := new(entities.Agent).With("agent-dana", "Dana Whitfield", entities.AgentTypePerson)
	mustNoErr(t, err, "build the agent")
	account, err := new(entities.Account).With("account-dana", "Dana Whitfield's Account", entities.AccountTypePersonal)
	mustNoErr(t, err, "build the account")
	return agent, account
}

func TestInstanceJWTService_ClampsTheAccessTokenToTheNotAfterInItsContext(t *testing.T) {
	svc, err := ProvideJWTService(config.Config{})
	mustNoErr(t, err, "build the token service")
	agent, account := capTestAgentAndAccount(t)
	notAfter := time.Now().Add(10 * time.Minute)
	ctx := WithAccessTokenNotAfter(WithNativeSession(context.Background(), "family-capped"), notAfter)

	token, err := svc.IssueToken(ctx, agent, []*entities.Account{account}, account.GetID(), nil,
		map[string]any{NativeSessionClaim: "family-capped"})
	mustNoErr(t, err, "issue a token with a not-after")
	claims, err := svc.ValidateToken(context.Background(), token)
	mustNoErr(t, err, "the clamped token does not validate on the service that issued it")
	if claims.ExpiresAt == nil || claims.ExpiresAt.After(notAfter) || notAfter.Sub(claims.ExpiresAt.Time) > time.Second {
		t.Fatalf("the token expires at %v, want the not-after %v", claims.ExpiresAt, notAfter)
	}
	if NativeSessionOf(claims) != "family-capped" || claims.ActiveAccountID != account.GetID() || claims.Subject != agent.GetID() {
		t.Fatalf("the clamped token lost its claims: session %q, account %q, subject %q",
			NativeSessionOf(claims), claims.ActiveAccountID, claims.Subject)
	}
}

func TestInstanceJWTService_ANotAfterPastTheHourOrNoneLeavesTheHour(t *testing.T) {
	svc, err := ProvideJWTService(config.Config{})
	mustNoErr(t, err, "build the token service")
	agent, account := capTestAgentAndAccount(t)
	for name, ctx := range map[string]context.Context{
		"no not-after":           context.Background(),
		"a not-after a day away": WithAccessTokenNotAfter(context.Background(), time.Now().Add(24*time.Hour)),
	} {
		t.Run(name, func(t *testing.T) {
			before := time.Now()
			token, err := svc.IssueToken(ctx, agent, []*entities.Account{account}, account.GetID(), nil, nil)
			mustNoErr(t, err, "issue a token")
			claims, err := svc.ValidateToken(context.Background(), token)
			mustNoErr(t, err, "validate the token")
			if claims.ExpiresAt == nil || !withinASecond(claims.ExpiresAt.Time, before.Add(defaultAccessTokenTTL)) {
				t.Fatalf("the token expires at %v, want %v from now", claims.ExpiresAt, defaultAccessTokenTTL)
			}
		})
	}
}

func TestRefreshTokenRepository_FamilyCapReadsTheCapOfAnyRowOfTheFamily(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	capAt := time.Now().Add(6 * time.Hour).Truncate(time.Second)

	mustNoErr(t, repo.Create(ctx, &OAuthRefreshToken{
		ID: "family-a", FamilyID: "family-a", AgentID: "agent-dana", ClientID: NativeClientID,
		ExpiresAt: capAt, FamilyExpiresAt: &capAt, Revoked: true,
	}, "raw-a-1"), "store the capped root")
	mustNoErr(t, repo.Create(ctx, &OAuthRefreshToken{
		FamilyID: "family-a", AgentID: "agent-dana", ClientID: NativeClientID,
		ExpiresAt: time.Now().Add(NativeRefreshTokenTTL),
	}, "raw-a-2"), "store an uncapped successor")
	mustNoErr(t, repo.Create(ctx, &OAuthRefreshToken{
		ID: "family-b", FamilyID: "family-b", AgentID: "agent-dana", ClientID: NativeClientID,
		ExpiresAt: time.Now().Add(NativeRefreshTokenTTL),
	}, "raw-b-1"), "store an uncapped family")

	got, err := repo.FamilyCap(ctx, "family-a")
	mustNoErr(t, err, "read family-a's cap")
	if got == nil || !withinASecond(*got, capAt) {
		t.Fatalf("family-a's cap reads %v, want %v", got, capAt)
	}
	got, err = repo.FamilyCap(ctx, "family-b")
	mustNoErr(t, err, "read family-b's cap")
	if got != nil {
		t.Fatalf("an uncapped family's cap reads %v, want none", *got)
	}
}

// A renewal served by a binary from before the cap copies no cap: its successor
// carries none and lasts 30 days. NativeFamilyCap reads the cap back from the
// rest of the family, and a rotation of a row given that cap writes it on again.
func TestNativeFamilyCap_RecoversACapAPreCapRotationLeftOff(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	capAt := time.Now().Add(24 * time.Hour)

	issued, err := IssueNativeRefreshTokenUntil(ctx, repo, "agent-ops", "acct-harbor", "", capAt)
	mustNoErr(t, err, "issue a capped native refresh token")
	root, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the capped refresh token back")
	stripped := *root
	stripped.FamilyExpiresAt = nil // what a pre-cap binary reads and copies
	oldBinary, err := RotateNativeRefreshToken(ctx, repo, &stripped, issued.Raw, nil)
	mustNoErr(t, err, "rotate as a pre-cap binary")
	uncapped, err := repo.FindByTokenHash(ctx, HashToken(oldBinary.Raw))
	mustNoErr(t, err, "read the pre-cap successor back")
	if uncapped.FamilyExpiresAt != nil {
		t.Fatalf("the test's pre-cap successor carries cap %v; it should carry none", *uncapped.FamilyExpiresAt)
	}

	got, err := NativeFamilyCap(ctx, repo, uncapped)
	mustNoErr(t, err, "read the family's cap from the uncapped successor")
	if got == nil || !withinASecond(*got, capAt) {
		t.Fatalf("the family's cap reads %v from the uncapped successor, want %v", got, capAt)
	}

	healed := *uncapped
	healed.FamilyExpiresAt = got
	next, err := RotateNativeRefreshToken(ctx, repo, &healed, oldBinary.Raw, nil)
	mustNoErr(t, err, "rotate the uncapped successor with the family's cap")
	if !withinASecond(next.ExpiresAt, capAt) {
		t.Fatalf("the healed successor expires at %v, want the family cap %v", next.ExpiresAt, capAt)
	}
	successor, err := repo.FindByTokenHash(ctx, HashToken(next.Raw))
	mustNoErr(t, err, "read the healed successor back")
	if successor.FamilyExpiresAt == nil || !withinASecond(*successor.FamilyExpiresAt, capAt) {
		t.Fatalf("the healed successor carries cap %v, want %v", successor.FamilyExpiresAt, capAt)
	}
}

// A row that carries its cap is answered from the row: no query is made.
func TestNativeFamilyCap_ARowWithItsCapIsAnsweredFromTheRow(t *testing.T) {
	capAt := time.Now().Add(time.Hour)
	got, err := NativeFamilyCap(context.Background(), nil, &OAuthRefreshToken{FamilyID: "f", FamilyExpiresAt: &capAt})
	mustNoErr(t, err, "read the cap from the row")
	if got == nil || !got.Equal(capAt) {
		t.Fatalf("the cap reads %v, want %v", got, capAt)
	}
}

// PR 576 review. A pre-cap binary's successor carries no cap and lives 30 days;
// its capped ancestor is purged a week after its capped expiry. The purge writes
// the family's cap onto the rows it leaves behind first, so the cap outlives the
// row that carried it and a later renewal of the successor still keeps to it.
func TestPurgeExpired_KeepsTheFamilyCapOnTheRowsItLeaves(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	now := time.Now()
	capAt := now.Add(-8 * 24 * time.Hour).Truncate(time.Second)

	mustNoErr(t, repo.Create(ctx, &OAuthRefreshToken{
		ID: "family-capped", FamilyID: "family-capped", AgentID: "agent-dana", ClientID: NativeClientID,
		ExpiresAt: capAt, FamilyExpiresAt: &capAt, Revoked: true,
	}, "raw-capped-root"), "store the capped root")
	mustNoErr(t, repo.Create(ctx, &OAuthRefreshToken{
		FamilyID: "family-capped", AgentID: "agent-dana", ClientID: NativeClientID,
		ExpiresAt: now.Add(20 * 24 * time.Hour),
	}, "raw-pre-cap-successor"), "store the pre-cap successor")
	mustNoErr(t, repo.Create(ctx, &OAuthRefreshToken{
		ID: "family-open", FamilyID: "family-open", AgentID: "agent-dana", ClientID: NativeClientID,
		ExpiresAt: now.Add(20 * 24 * time.Hour),
	}, "raw-uncapped"), "store an uncapped family")

	purged, err := repo.PurgeExpired(ctx, NativeClientID, now.Add(-NativeRefreshTokenPurgeHorizon))
	mustNoErr(t, err, "purge")
	if purged != 1 {
		t.Fatalf("the purge deleted %d rows, want the capped root alone", purged)
	}

	successor, err := repo.FindByTokenHash(ctx, HashToken("raw-pre-cap-successor"))
	mustNoErr(t, err, "read the pre-cap successor back")
	if successor.FamilyExpiresAt == nil || !withinASecond(*successor.FamilyExpiresAt, capAt) {
		t.Fatalf("after the purge the pre-cap successor carries cap %v, want the family cap %v",
			successor.FamilyExpiresAt, capAt)
	}
	got, err := NativeFamilyCap(ctx, repo, successor)
	mustNoErr(t, err, "read the family's cap")
	if got == nil || !withinASecond(*got, capAt) {
		t.Fatalf("after the purge the family's cap reads %v, want %v", got, capAt)
	}
	open, err := repo.FindByTokenHash(ctx, HashToken("raw-uncapped"))
	mustNoErr(t, err, "read the uncapped family back")
	if open.FamilyExpiresAt != nil {
		t.Fatalf("the purge gave an uncapped family cap %v, want none", *open.FamilyExpiresAt)
	}
}

// PR 576 review. CapExpiry gives a row its family's cap: the cap is written on,
// and an expiry past it is moved back to it. An earlier cap or expiry is kept.
func TestRefreshTokenRepository_CapExpiryEndsTheRowByTheCap(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	now := time.Now()
	capAt := now.Add(2 * time.Hour).Truncate(time.Second)
	earlier := now.Add(time.Hour).Truncate(time.Second)

	mustNoErr(t, repo.Create(ctx, &OAuthRefreshToken{
		ID: "row-uncapped", FamilyID: "family-x", AgentID: "agent-dana", ClientID: NativeClientID,
		ExpiresAt: now.Add(NativeRefreshTokenTTL),
	}, "raw-uncapped-row"), "store an uncapped row")
	mustNoErr(t, repo.Create(ctx, &OAuthRefreshToken{
		ID: "row-earlier", FamilyID: "family-y", AgentID: "agent-dana", ClientID: NativeClientID,
		ExpiresAt: earlier, FamilyExpiresAt: &earlier,
	}, "raw-earlier-row"), "store a row capped earlier")

	mustNoErr(t, repo.CapExpiry(ctx, "row-uncapped", capAt), "cap the uncapped row")
	mustNoErr(t, repo.CapExpiry(ctx, "row-earlier", capAt), "cap the earlier row")

	row, err := repo.FindByTokenHash(ctx, HashToken("raw-uncapped-row"))
	mustNoErr(t, err, "read the capped row back")
	if row.FamilyExpiresAt == nil || !withinASecond(*row.FamilyExpiresAt, capAt) || !withinASecond(row.ExpiresAt, capAt) {
		t.Fatalf("the row carries cap %v and expires at %v, want both at %v", row.FamilyExpiresAt, row.ExpiresAt, capAt)
	}
	row, err = repo.FindByTokenHash(ctx, HashToken("raw-earlier-row"))
	mustNoErr(t, err, "read the earlier row back")
	if row.FamilyExpiresAt == nil || !withinASecond(*row.FamilyExpiresAt, earlier) || !withinASecond(row.ExpiresAt, earlier) {
		t.Fatalf("the row capped earlier carries cap %v and expires at %v, want both kept at %v",
			row.FamilyExpiresAt, row.ExpiresAt, earlier)
	}
}
