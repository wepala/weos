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

// wm-sa7wv. Every native sign-in and renewal adds a refresh token row, and a
// renewal keeps the spent one. Rows are purged once they are past their expiry
// by more than the purge horizon; until then a spent row stays, so presenting
// it is still caught as reuse.

// createNativePurgeRow stores a refresh token for the purge tests and revokes
// it when revoked is set.
func createNativePurgeRow(t *testing.T, repo RefreshTokenRepository, raw, clientID string, expiresAt time.Time, revoked bool) {
	t.Helper()
	ctx := context.Background()
	token := &OAuthRefreshToken{AgentID: "agent-ops", AccountID: "acct-harbor", ClientID: clientID, ExpiresAt: expiresAt}
	mustNoErr(t, repo.Create(ctx, token, raw), "create "+raw)
	if revoked {
		mustNoErr(t, repo.Revoke(ctx, token.ID), "revoke "+raw)
	}
}

func TestRefreshTokenRepo_PurgeExpired_RemovesOnlyNativeRowsPastTheHorizon(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	now := time.Now()
	cutoff := now.Add(-NativeRefreshTokenPurgeHorizon)

	createNativePurgeRow(t, repo, "native-long-expired", NativeClientID, cutoff.Add(-time.Hour), false)
	createNativePurgeRow(t, repo, "native-spent-long-expired", NativeClientID, cutoff.Add(-time.Hour), true)
	createNativePurgeRow(t, repo, "native-spent-inside-the-horizon", NativeClientID, cutoff.Add(time.Hour), true)
	createNativePurgeRow(t, repo, "native-live", NativeClientID, now.Add(NativeRefreshTokenTTL), false)
	createNativePurgeRow(t, repo, "native-spent-live", NativeClientID, now.Add(NativeRefreshTokenTTL), true)
	createNativePurgeRow(t, repo, "connector-long-expired", "client-notes", cutoff.Add(-time.Hour), false)

	purged, err := repo.PurgeExpired(ctx, NativeClientID, cutoff)
	mustNoErr(t, err, "purge native refresh tokens past the horizon")
	if purged != 2 {
		t.Errorf("purged %d rows, want 2", purged)
	}
	for raw, wantKept := range map[string]bool{
		"native-long-expired":             false,
		"native-spent-long-expired":       false,
		"native-spent-inside-the-horizon": true,
		"native-live":                     true,
		"native-spent-live":               true,
		"connector-long-expired":          true,
	} {
		_, err := repo.FindByTokenHash(ctx, HashToken(raw))
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatalf("read %s: %v", raw, err)
		}
		if kept := err == nil; kept != wantKept {
			t.Errorf("%s kept = %v, want %v", raw, kept, wantKept)
		}
	}
}

// The purge runs where native refresh tokens are written, so it must stay
// cheap: at most once every NativeRefreshTokenPurgeInterval per process.
func TestNativeRefreshTokenPurger_PurgesAtMostOnceAnInterval(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	clock := time.Now()
	purger := NewNativeRefreshTokenPurger(repo, func() time.Time { return clock })
	aged := func(raw string) {
		t.Helper()
		createNativePurgeRow(t, repo, raw, NativeClientID, clock.Add(-NativeRefreshTokenPurgeHorizon-time.Hour), true)
	}
	stillThere := func(raw string) bool {
		t.Helper()
		_, err := repo.FindByTokenHash(ctx, HashToken(raw))
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatalf("read %s: %v", raw, err)
		}
		return err == nil
	}

	aged("aged-before-the-first-purge")
	if ran, purged, err := purger.MaybePurge(ctx); err != nil || !ran || purged != 1 {
		t.Fatalf("the first purge ran %v and purged %d (error %v), want it to run and purge 1", ran, purged, err)
	}

	aged("aged-inside-the-interval")
	clock = clock.Add(NativeRefreshTokenPurgeInterval / 2)
	if ran, _, err := purger.MaybePurge(ctx); err != nil || ran {
		t.Fatalf("a purge inside the interval ran %v (error %v), want it skipped", ran, err)
	}
	if !stillThere("aged-inside-the-interval") {
		t.Fatal("a purge skipped inside the interval removed a row")
	}

	clock = clock.Add(NativeRefreshTokenPurgeInterval)
	if ran, purged, err := purger.MaybePurge(ctx); err != nil || !ran || purged != 1 {
		t.Fatalf("the purge after the interval ran %v and purged %d (error %v), want it to run and purge 1", ran, purged, err)
	}
}
