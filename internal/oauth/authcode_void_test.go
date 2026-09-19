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

// storeCode puts one authorization code in the store.
func storeCode(t *testing.T, repo AuthCodeRepository, code, agentID, status string, expiresAt time.Time) {
	t.Helper()
	mustNoErr(t, repo.Create(context.Background(), &OAuthAuthorizationCode{
		Code:                code,
		ClientID:            "2a4RnKq8cLmPzT",
		AgentID:             agentID,
		AccountID:           "account-1",
		RedirectURI:         "https://notes.example/oauth/callback",
		CodeChallenge:       "a-proof-key-challenge",
		CodeChallengeMethod: "S256",
		Status:              status,
		ExpiresAt:           expiresAt,
	}), "store the authorization code "+code)
}

func statusOf(t *testing.T, repo AuthCodeRepository, code string) string {
	t.Helper()
	stored, err := repo.FindByCode(context.Background(), code)
	mustNoErr(t, err, "read the authorization code "+code)
	return stored.Status
}

// wm-4ke17. A code the person was handed before the reset still exchanges for
// a fresh 30-day refresh token after it: the token endpoint checks the code's
// status, the membership and the agent, and nothing about a revocation. So the
// revocation voids them, and a voided code is refused exactly as a spent one
// is.
func TestVoidUnredeemedForAgentEndsTheCodesNobodyHasRedeemed(t *testing.T) {
	repo := NewAuthCodeRepository(setupTestDB(t))
	ctx := context.Background()
	soon := time.Now().Add(10 * time.Minute)
	storeCode(t, repo, "dana-waiting", "dana", StatusIssued, soon)
	storeCode(t, repo, "dana-second-client", "dana", StatusIssued, soon)
	storeCode(t, repo, "dana-spent", "dana", StatusExchanged, soon)
	storeCode(t, repo, "dana-expired", "dana", StatusIssued, time.Now().Add(-time.Minute))
	storeCode(t, repo, "morgan-waiting", "morgan", StatusIssued, soon)

	voided, err := repo.VoidUnredeemedForAgent(ctx, "dana")
	mustNoErr(t, err, "void the person's unredeemed authorization codes")

	if voided != 2 {
		t.Fatalf("voided %d codes, want the person's 2 that were still redeemable", voided)
	}
	for _, code := range []string{"dana-waiting", "dana-second-client"} {
		if got := statusOf(t, repo, code); got != StatusVoided {
			t.Fatalf("%s is %q, want %q", code, got, StatusVoided)
		}
		// A voided code is refused where a redeemable one is taken.
		if err := repo.MarkExchanged(ctx, code); err != ErrNotFound {
			t.Fatalf("exchanging the voided %s answered %v, want it refused", code, err)
		}
	}
	// A spent code keeps saying it was really used, an expired one is left as
	// it was, and nobody else's code is touched.
	if got := statusOf(t, repo, "dana-spent"); got != StatusExchanged {
		t.Fatalf("the spent code is %q, want %q", got, StatusExchanged)
	}
	if got := statusOf(t, repo, "dana-expired"); got != StatusIssued {
		t.Fatalf("the expired code is %q, want it left as it was", got)
	}
	if got := statusOf(t, repo, "morgan-waiting"); got != StatusIssued {
		t.Fatalf("another person's code is %q, want it untouched", got)
	}
}

// The door repeats a call whose answer it could not read, and a code still
// pending belongs to nobody yet — the sign-in that binds it has not happened —
// so an empty id must match no row rather than every row.
func TestVoidUnredeemedForAgentWithNothingToVoid(t *testing.T) {
	repo := NewAuthCodeRepository(setupTestDB(t))
	ctx := context.Background()
	soon := time.Now().Add(10 * time.Minute)
	storeCode(t, repo, "dana-waiting", "dana", StatusIssued, soon)
	storeCode(t, repo, "nobodys-yet", "", StatusPending, soon)

	first, err := repo.VoidUnredeemedForAgent(ctx, "dana")
	mustNoErr(t, err, "void the person's codes")
	if first != 1 {
		t.Fatalf("voided %d codes, want the 1 that was redeemable", first)
	}
	second, err := repo.VoidUnredeemedForAgent(ctx, "dana")
	mustNoErr(t, err, "void the person's codes again")
	if second != 0 {
		t.Fatalf("the second call voided %d codes, want none left", second)
	}

	for name, agentID := range map[string]string{"nobody": "morgan", "no id at all": ""} {
		t.Run(name, func(t *testing.T) {
			voided, err := repo.VoidUnredeemedForAgent(ctx, agentID)
			mustNoErr(t, err, "void for "+name)
			if voided != 0 {
				t.Fatalf("voided %d codes for %s", voided, name)
			}
			if got := statusOf(t, repo, "nobodys-yet"); got != StatusPending {
				t.Fatalf("a code bound to nobody is %q, want it left pending", got)
			}
		})
	}
}
