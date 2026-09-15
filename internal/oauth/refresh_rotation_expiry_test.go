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

// wm-lnimb. A renewal checks a refresh token's expiry when it arrives and then
// reads the account, so its rotation can land after the token expired: queued,
// or waiting on the row's lock. A rotation spends the token only while it is
// unexpired by the clock read inside the rotation's own transaction. Otherwise
// it answers ErrNotFound, spends nothing and saves no successor.
func TestRefreshTokenRepo_Rotate_RefusesATokenThatExpiredBeforeTheRotationLanded(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	cases := []struct {
		name      string
		readings  []time.Time
		wantSpent bool
	}{
		{"unexpired when the rotation landed", []time.Time{expiresAt.Add(-time.Second)}, true},
		{"expired before the rotation's update", []time.Time{expiresAt.Add(time.Second)}, false},
		{"expired while the rotation's update waited on the row",
			[]time.Time{expiresAt.Add(-time.Second), expiresAt.Add(time.Second)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			ctx := context.Background()
			readings := tc.readings
			repo := &gormRefreshTokenRepo{db: db, now: func() time.Time {
				now := readings[0]
				if len(readings) > 1 {
					readings = readings[1:]
				}
				return now
			}}
			mustNoErr(t, repo.Create(ctx, &OAuthRefreshToken{
				ID: "rt-phone", AgentID: "agent-ops", AccountID: "acct-harbor", ClientID: NativeClientID, ExpiresAt: expiresAt,
			}, "phone-refresh-token"), "issue the phone's refresh token")

			err := repo.Rotate(ctx, "rt-phone", &OAuthRefreshToken{
				AgentID: "agent-ops", AccountID: "acct-harbor", ClientID: NativeClientID, FamilyID: "rt-phone",
				ExpiresAt: expiresAt.Add(NativeRefreshTokenTTL),
			}, "phone-next-refresh-token")

			stored, lookupErr := repo.FindByTokenHash(ctx, HashToken("phone-refresh-token"))
			mustNoErr(t, lookupErr, "read the phone's refresh token")
			var rows int64
			mustNoErr(t, db.Model(&OAuthRefreshToken{}).Where("family_id = ?", "rt-phone").Count(&rows).Error,
				"count the family's refresh tokens")

			if tc.wantSpent {
				mustNoErr(t, err, "rotate an unexpired refresh token")
				if !stored.Revoked || rows != 2 {
					t.Fatalf("the rotation left revoked=%v and %d rows in the family, want the token spent and 2 rows",
						stored.Revoked, rows)
				}
				return
			}
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("rotating a refresh token that expired before the rotation landed returned %v, want ErrNotFound", err)
			}
			if stored.Revoked || stored.SuccessorID != "" || stored.RotatedAt != nil {
				t.Fatalf("the refused rotation spent the token: revoked=%v, names a successor=%v, rotated=%v",
					stored.Revoked, stored.SuccessorID != "", stored.RotatedAt != nil)
			}
			if rows != 1 {
				t.Fatalf("the family has %d refresh tokens after a refused rotation, want 1: no successor is saved", rows)
			}
		})
	}
}
