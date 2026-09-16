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

// storeToken puts one refresh token in the store and answers the row.
func storeToken(t *testing.T, repo RefreshTokenRepository, id, agentID, clientID, familyID string, revoked bool) *OAuthRefreshToken {
	t.Helper()
	token := &OAuthRefreshToken{
		ID:        id,
		AgentID:   agentID,
		ClientID:  clientID,
		FamilyID:  familyID,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		Revoked:   revoked,
	}
	mustNoErr(t, repo.Create(context.Background(), token, "raw-"+id), "store the refresh token")
	return token
}

// activeTokensOf is how many of the agent's tokens still renew.
func activeTokensOf(t *testing.T, repo RefreshTokenRepository, ids ...string) int {
	t.Helper()
	active := 0
	for _, id := range ids {
		stored, err := repo.FindByTokenHash(context.Background(), HashToken("raw-"+id))
		mustNoErr(t, err, "read the refresh token "+id)
		if !stored.Revoked {
			active++
		}
	}
	return active
}

// A password reset at the door ends the person's token access, and the person
// holds more than one kind of token: an MCP connector's, under the client id
// that connector registered, and a native app session's, under weos-native.
// Neither can be told from an intruder's, so every one of them goes.
func TestRevokeAllForAgentRevokesEveryClientAndEveryFamily(t *testing.T) {
	repo := NewRefreshTokenRepository(setupTestDB(t))
	ctx := context.Background()

	storeToken(t, repo, "connector-1", "dana", "2a4RnKq8cLmPzT", "connector-1", false)
	storeToken(t, repo, "connector-2", "dana", "9bXsW3vNhYtQ2L", "connector-2", false)
	storeToken(t, repo, "phone", "dana", NativeClientID, "phone", false)
	storeToken(t, repo, "laptop", "dana", NativeClientID, "laptop", false)
	storeToken(t, repo, "someone-else", "morgan", NativeClientID, "someone-else", false)

	revoked, err := repo.RevokeAllForAgent(ctx, "dana")
	mustNoErr(t, err, "revoke every token of the person")
	if revoked != 4 {
		t.Fatalf("revoked %d tokens, want the person's 4", revoked)
	}
	if active := activeTokensOf(t, repo, "connector-1", "connector-2", "phone", "laptop"); active != 0 {
		t.Fatalf("%d of the person's tokens still renew", active)
	}
	if active := activeTokensOf(t, repo, "someone-else"); active != 1 {
		t.Fatalf("another person's token was revoked too")
	}
}

// The door repeats a call it could not read the answer to, and a reset may be
// the second one a person asks for. Neither may fail, and neither revokes
// anything it has already revoked.
func TestRevokeAllForAgentIsIdempotent(t *testing.T) {
	repo := NewRefreshTokenRepository(setupTestDB(t))
	ctx := context.Background()
	storeToken(t, repo, "phone", "dana", NativeClientID, "phone", false)
	storeToken(t, repo, "spent", "dana", NativeClientID, "phone", true)

	first, err := repo.RevokeAllForAgent(ctx, "dana")
	mustNoErr(t, err, "revoke every token of the person")
	if first != 1 {
		t.Fatalf("revoked %d tokens, want the 1 that still renewed", first)
	}
	second, err := repo.RevokeAllForAgent(ctx, "dana")
	mustNoErr(t, err, "revoke every token of the person again")
	if second != 0 {
		t.Fatalf("the second call revoked %d tokens, want none left to revoke", second)
	}
}

// A person the instance has never seen, and an empty id from a caller that
// resolved nobody, revoke nothing rather than matching every row with an empty
// agent id.
func TestRevokeAllForAgentWithNothingToRevoke(t *testing.T) {
	repo := NewRefreshTokenRepository(setupTestDB(t))
	ctx := context.Background()
	storeToken(t, repo, "phone", "dana", NativeClientID, "phone", false)

	for name, agentID := range map[string]string{"nobody": "morgan", "no id at all": ""} {
		t.Run(name, func(t *testing.T) {
			revoked, err := repo.RevokeAllForAgent(ctx, agentID)
			mustNoErr(t, err, "revoke for "+name)
			if revoked != 0 {
				t.Fatalf("revoked %d tokens for %s", revoked, name)
			}
			if active := activeTokensOf(t, repo, "phone"); active != 1 {
				t.Fatalf("somebody else's token was revoked")
			}
		})
	}
}
