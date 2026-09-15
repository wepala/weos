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

package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"

	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"go.uber.org/fx"
)

// wm-xsvas. An account deletion that failed part-way leaves the account locked
// and inactive. An app in a native shell holds no cookie, only a token, and the
// token it had expires an hour later; the deletion can then be finished from the
// app only if signing in again hands back a token for the locked account. That
// token must still be refused everywhere except the deletion.

// lockedSignIn is what a sign-in to a locked account answers.
type lockedSignIn struct {
	token          string
	accountID      string
	erasurePending bool
	code           string
}

func decodeLockedSignIn(t *testing.T, answer serveAnswer, what string) lockedSignIn {
	t.Helper()
	if answer.status != http.StatusOK {
		t.Fatalf("%s answered %d %s, want 200", what, answer.status, answer.body)
	}
	var decoded struct {
		Data struct {
			Token   string `json:"token"`
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
			ErasurePending bool   `json:"erasure_pending"`
			Code           string `json:"code"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &decoded); err != nil {
		t.Fatalf("decode %s: %v", what, err)
	}
	return lockedSignIn{
		token:          decoded.Data.Token,
		accountID:      decoded.Data.Account.ID,
		erasurePending: decoded.Data.ErasurePending,
		code:           decoded.Data.Code,
	}
}

// lockForErasure takes the erasure lock on accountID and deactivates it, which
// is where a deletion that failed part-way leaves an account.
func lockForErasure(t *testing.T, accounts authrepos.AccountRepository, locks repositories.AccountErasureLocks, accountID, agentID string) {
	t.Helper()
	ctx := context.Background()
	if err := locks.Lock(ctx, accountID, agentID); err != nil {
		t.Fatalf("lock the account: %v", err)
	}
	account, err := accounts.FindByID(ctx, accountID)
	if err != nil || account == nil {
		t.Fatalf("read the account: %v", err)
	}
	if err := account.Deactivate(); err != nil {
		t.Fatalf("deactivate the account: %v", err)
	}
	if err := accounts.Save(ctx, account); err != nil {
		t.Fatalf("save the deactivated account: %v", err)
	}
}

func TestServe_ASignInToALockedAccountHandsBackATokenForTheDeletionOnly(t *testing.T) {
	door := newBootDoor(t)
	var accounts authrepos.AccountRepository
	var locks repositories.AccountErasureLocks
	srv := bootServe(t, connectorConfig(door), withProtectedPresetProbe(), fx.Populate(&accounts, &locks))

	cases := []struct {
		name   string
		first  func() doorSignIn
		signIn func() serveAnswer
	}{
		{
			name:  "password sign-in",
			first: func() doorSignIn { return signInWithPassword(t, srv, bootPasswordEmail, bootPasswordSecret) },
			signIn: func() serveAnswer {
				return serveCall(t, srv, http.MethodPost, "/api/auth/password-login",
					`{"email":"`+bootPasswordEmail+`","password":"`+bootPasswordSecret+`"}`, nil)
			},
		},
		{
			name: "the door",
			first: func() doorSignIn {
				return signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
			},
			signIn: func() serveAnswer {
				return serveCall(t, srv, http.MethodPost, "/api/auth/assert",
					door.assertionBodyFor(t, bootOwnerEmail, bootOwnerSubject, bootOwnerName), nil)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			person := tc.first()
			lockForErasure(t, accounts, locks, person.accountID, person.agentID)

			again := decodeLockedSignIn(t, tc.signIn(), "the sign-in to the locked account")
			if !again.erasurePending || again.code != "account_erasure_pending" || again.accountID != person.accountID {
				t.Fatalf("the sign-in answered erasure_pending=%v code=%q for account %q, want the locked account with account_erasure_pending",
					again.erasurePending, again.code, again.accountID)
			}
			if again.token == "" {
				t.Fatal("the sign-in to the locked account handed back no token, so an app with no cookie cannot finish the deletion")
			}

			for _, path := range []string{"/api/resource-types", "/api" + presetProbePath, "/api/auth/me"} {
				got := serveRequest(t, srv, http.MethodGet, path, "", again.token, nil)
				if got.status != http.StatusUnauthorized || refusalCode(got.body) != "account_erasure_pending" {
					t.Errorf("GET %s with the locked sign-in's token answered %d %s, want 401 account_erasure_pending", path, got.status, got.body)
				}
			}
			if got := bearerMCPCall(t, srv, again.token); got.status != http.StatusUnauthorized || refusalCode(got.body) != "account_erasure_pending" {
				t.Errorf("POST /api/mcp with the locked sign-in's token answered %d %s, want 401 account_erasure_pending", got.status, got.body)
			}

			finish := serveRequest(t, srv, http.MethodDelete, "/api/account", confirmDeletion, again.token, nil)
			if finish.status != http.StatusOK || !strings.Contains(finish.body, person.accountID) {
				t.Fatalf("DELETE /api/account with the locked sign-in's token answered %d %s, want 200 finishing the deletion", finish.status, finish.body)
			}
		})
	}
}
