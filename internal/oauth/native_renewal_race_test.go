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
	"sync"
	"testing"
)

// wm-tu180. Rotate's conditional update is the only thing that stops one native
// refresh token being spent twice. Eight rotations of the same token at once:
// exactly one succeeds, the rest get ErrNotFound, and the family ends with one
// live token. Run it under -race.
func TestRotateNativeRefreshToken_EightAtOnceSpendTheTokenOnce(t *testing.T) {
	const renewals = 8
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	issued, err := IssueNativeRefreshToken(ctx, repo, "agent-ops", "acct-harbor", "")
	mustNoErr(t, err, "issue a native refresh token")
	stored, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the native refresh token")

	start := make(chan struct{})
	errs := make([]error, renewals)
	var wg sync.WaitGroup
	for i := range renewals {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// Each rotation reads its own copy of the row, as each request does.
			row := *stored
			_, errs[i] = RotateNativeRefreshToken(ctx, repo, &row, issued.Raw, nil)
		}()
	}
	close(start)
	wg.Wait()

	succeeded, spent := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrNotFound):
			spent++
		default:
			t.Errorf("a rotation failed with %v, want success or ErrNotFound", err)
		}
	}
	if succeeded != 1 || spent != renewals-1 {
		t.Fatalf("%d rotations succeeded and %d found the token spent, want 1 and %d", succeeded, spent, renewals-1)
	}

	var live int64
	if err := db.Model(&OAuthRefreshToken{}).
		Where("family_id = ? AND revoked = ?", stored.FamilyID, false).Count(&live).Error; err != nil {
		t.Fatalf("count the family's live tokens: %v", err)
	}
	if live != 1 {
		t.Fatalf("the family has %d live refresh tokens after the race, want 1", live)
	}
}
