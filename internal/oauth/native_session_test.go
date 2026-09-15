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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/labstack/echo/v4"
)

// wm-lnimb. A native sign-in hands back a refresh token beside its one-hour
// access token. It lives in the OAuth refresh token store under a client id no
// registration can have, so rotation and family revocation are the store's
// own. No failure message prints a raw token.

// withinASecond reports whether got is want give or take a second: SQLite does
// not keep a time's monotonic reading, and may round what it keeps.
func withinASecond(got, want time.Time) bool {
	d := got.Sub(want)
	return d > -time.Second && d < time.Second
}

func TestIssueNativeRefreshToken_KeepsOnlyTheHashUnderTheNativeClient(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	before := time.Now()
	issued, err := IssueNativeRefreshToken(ctx, repo, "agent-ops", "acct-harbor", "")
	mustNoErr(t, err, "issue a native refresh token")
	after := time.Now()
	if issued.Raw == "" {
		t.Fatal("the issued native refresh token is empty")
	}

	stored, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the native refresh token back by its hash")
	if stored.ClientID != NativeClientID || stored.AgentID != "agent-ops" || stored.AccountID != "acct-harbor" {
		t.Fatalf("stored client %q agent %q account %q, want %q agent-ops acct-harbor",
			stored.ClientID, stored.AgentID, stored.AccountID, NativeClientID)
	}
	if stored.FamilyID != stored.ID || stored.Revoked {
		t.Fatalf("a first issuance must start its own live family: family is its own id %v, revoked %v",
			stored.FamilyID == stored.ID, stored.Revoked)
	}
	if stored.TokenHash == issued.Raw {
		t.Fatal("the store kept the raw refresh token, not its hash")
	}
	if !IsNativeRefreshToken(stored) {
		t.Fatal("IsNativeRefreshToken does not recognize a native sign-in's refresh token")
	}
	if stored.ExpiresAt.Before(before.Add(NativeRefreshTokenTTL).Add(-time.Second)) ||
		stored.ExpiresAt.After(after.Add(NativeRefreshTokenTTL).Add(time.Second)) {
		t.Fatalf("the refresh token expires at %v, want %v after issuance", stored.ExpiresAt, NativeRefreshTokenTTL)
	}
	if !withinASecond(issued.ExpiresAt, stored.ExpiresAt) {
		t.Fatalf("the issuance reports expiry %v, the store keeps %v", issued.ExpiresAt, stored.ExpiresAt)
	}
}

func TestRotateNativeRefreshToken_KeepsTheFamilyAndSlidesTheExpiry(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	old := &OAuthRefreshToken{
		AgentID: "agent-ops", AccountID: "acct-harbor", ClientID: NativeClientID,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	mustNoErr(t, repo.Create(ctx, old, "raw-native-refresh-token"), "create the refresh token")

	before := time.Now()
	rotated, err := RotateNativeRefreshToken(ctx, repo, old)
	mustNoErr(t, err, "rotate the native refresh token")
	if rotated.Raw == "" || rotated.Raw == "raw-native-refresh-token" {
		t.Fatal("the rotation did not hand back a new refresh token")
	}

	was, err := repo.FindByTokenHash(ctx, HashToken("raw-native-refresh-token"))
	mustNoErr(t, err, "read the old refresh token back")
	if !was.Revoked {
		t.Fatal("the rotated refresh token is still live")
	}
	next, err := repo.FindByTokenHash(ctx, HashToken(rotated.Raw))
	mustNoErr(t, err, "read the new refresh token back")
	if next.Revoked || next.FamilyID != old.FamilyID || next.ClientID != NativeClientID ||
		next.AgentID != old.AgentID || next.AccountID != old.AccountID {
		t.Fatalf("the new refresh token is revoked %v, same family %v, client %q, same person and account %v",
			next.Revoked, next.FamilyID == old.FamilyID, next.ClientID,
			next.AgentID == old.AgentID && next.AccountID == old.AccountID)
	}
	if next.ExpiresAt.Before(before.Add(NativeRefreshTokenTTL).Add(-time.Second)) {
		t.Fatalf("the new refresh token expires at %v; a renewal must give it a full %v", next.ExpiresAt, NativeRefreshTokenTTL)
	}

	if _, err := RotateNativeRefreshToken(ctx, repo, old); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rotating the already rotated token again returned %v, want ErrNotFound", err)
	}
}

func TestRefreshTokenRepo_RevokeForAgent_RevokesOnlyThatPersonsTokensForThatClient(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	create := func(raw, agentID, clientID string) {
		t.Helper()
		mustNoErr(t, repo.Create(ctx, &OAuthRefreshToken{AgentID: agentID, AccountID: "acct-harbor", ClientID: clientID}, raw),
			"create "+raw)
	}
	create("phone", "agent-ops", NativeClientID)
	create("tablet", "agent-ops", NativeClientID)
	create("notes-connector", "agent-ops", "client-notes")
	create("rosa-phone", "agent-rosa", NativeClientID)

	mustNoErr(t, repo.RevokeForAgent(ctx, "agent-ops", NativeClientID), "revoke agent-ops's native refresh tokens")

	for raw, wantRevoked := range map[string]bool{
		"phone": true, "tablet": true, "notes-connector": false, "rosa-phone": false,
	} {
		stored, err := repo.FindByTokenHash(ctx, HashToken(raw))
		mustNoErr(t, err, "read "+raw)
		if stored.Revoked != wantRevoked {
			t.Errorf("%s revoked = %v, want %v", raw, stored.Revoked, wantRevoked)
		}
	}
}

// A native session's access token names its refresh token family, so a
// sign-out with that token can end that session and no other (wm-utb5c). The
// claim is added only for a native session: every other token is as it was.
func TestNativeSessionClaims_NameTheFamilyOnlyForANativeSession(t *testing.T) {
	ctx := context.Background()

	extras, err := NativeSessionClaims(ctx, nil, nil, "acct-harbor")
	mustNoErr(t, err, "claims for a token issued outside a native session")
	if len(extras) != 0 {
		t.Fatalf("a token issued outside a native session gets extra claims %v, want none", extras)
	}

	extras, err = NativeSessionClaims(WithNativeSession(ctx, "family-phone"), nil, nil, "acct-harbor")
	mustNoErr(t, err, "claims for a native session's token")
	if got, _ := extras[NativeSessionClaim].(string); got != "family-phone" {
		t.Fatalf("a native session's token names family %q, want family-phone", got)
	}

	if got := NativeSessionOf(&authapp.PericarpClaims{Extras: map[string]any{NativeSessionClaim: "family-phone"}}); got != "family-phone" {
		t.Fatalf("NativeSessionOf read %q, want family-phone", got)
	}
	if NativeSessionOf(&authapp.PericarpClaims{}) != "" || NativeSessionOf(nil) != "" {
		t.Fatal("NativeSessionOf names a family for a token that carries none")
	}
}

// A native refresh token presented at /oauth/token is refused, and left as it
// was: the token endpoint issues connector tokens, and a native session renews
// only through the native route.
func TestTokenHandler_RefreshToken_RefusesANativeRefreshToken(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	issued, err := IssueNativeRefreshToken(ctx, repo, "agent-ops", "acct-harbor", "")
	mustNoErr(t, err, "issue a native refresh token")

	agent, err := (&authentities.Agent{}).With("agent-ops", "Dana Whitfield", authentities.AgentTypePerson)
	mustNoErr(t, err, "make the agent")
	jwt := &issuing{}
	handler := Token(jwt, NewAuthCodeRepository(db), repo, oneAgent{agent: agent},
		membership{role: authentities.RoleOwner}, noopLogger{})

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(newTokenRequest(tokenForm(map[string]string{
		"grant_type": "refresh_token", "refresh_token": issued.Raw, "client_id": NativeClientID,
	})), rec)
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"invalid_grant"`) || jwt.issued {
		t.Fatalf("got %d (issued %v), want 400 invalid_grant and no token", rec.Code, jwt.issued)
	}
	stored, err := repo.FindByTokenHash(ctx, HashToken(issued.Raw))
	mustNoErr(t, err, "read the native refresh token back")
	if stored.Revoked {
		t.Fatal("the token endpoint revoked a native refresh token it refused")
	}
}
