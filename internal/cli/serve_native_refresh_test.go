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
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"go.uber.org/fx"
)

// wm-lnimb. A native sign-in's access token lasts an hour, and an app in a
// native shell holds nothing else, so without a way to renew it the person is
// signed out every hour. Both native sign-ins — the door's assertion and the
// password — hand back a refresh token beside the access token, and the app
// renews at POST /api/auth/refresh. These tests boot serve's own routes. No
// failure message prints a token, a refresh token or a sign-in's body.

const (
	nativeRefreshTTL = 30 * 24 * time.Hour
	nativeAccessTTL  = time.Hour
)

// nativeSession is what a native sign-in or a renewal hands back.
type nativeSession struct {
	token            string
	tokenExpiresAt   time.Time
	refreshToken     string
	refreshExpiresAt time.Time
	agentID          string
	accountID        string
	erasurePending   bool
	code             string
}

// decodeNativeSession reads a 200 answer from a native sign-in or a renewal.
func decodeNativeSession(t *testing.T, answer serveAnswer, what string) nativeSession {
	t.Helper()
	if answer.status != http.StatusOK {
		t.Fatalf("%s answered %d code %q, want 200", what, answer.status, refusalCode(answer.body))
	}
	var decoded struct {
		Data struct {
			Token                 string    `json:"token"`
			TokenExpiresAt        time.Time `json:"token_expires_at"`
			RefreshToken          string    `json:"refresh_token"`
			RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
			ErasurePending        bool      `json:"erasure_pending"`
			Code                  string    `json:"code"`
			Agent                 struct {
				ID string `json:"id"`
			} `json:"agent"`
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &decoded); err != nil {
		t.Fatalf("decode %s: %v", what, err)
	}
	d := decoded.Data
	return nativeSession{
		token: d.Token, tokenExpiresAt: d.TokenExpiresAt,
		refreshToken: d.RefreshToken, refreshExpiresAt: d.RefreshTokenExpiresAt,
		agentID: d.Agent.ID, accountID: d.Account.ID,
		erasurePending: d.ErasurePending, code: d.Code,
	}
}

// requireBothTokens checks that s carries an access token and a refresh token
// with the lifetimes a native session gets, counted from issuedAfter.
func requireBothTokens(t *testing.T, s nativeSession, issuedAfter time.Time, what string) {
	t.Helper()
	if s.token == "" || s.refreshToken == "" || s.token == s.refreshToken {
		t.Fatalf("%s carries token %v and refresh token %v (distinct %v), want both",
			what, s.token != "", s.refreshToken != "", s.token != s.refreshToken)
	}
	now := time.Now()
	if s.refreshExpiresAt.Before(issuedAfter.Add(nativeRefreshTTL).Add(-time.Minute)) ||
		s.refreshExpiresAt.After(now.Add(nativeRefreshTTL).Add(time.Minute)) {
		t.Fatalf("%s says the refresh token expires at %v, want %v from now", what, s.refreshExpiresAt, nativeRefreshTTL)
	}
	if s.tokenExpiresAt.Before(issuedAfter.Add(nativeAccessTTL).Add(-time.Minute)) ||
		s.tokenExpiresAt.After(now.Add(nativeAccessTTL).Add(time.Minute)) {
		t.Fatalf("%s says the access token expires at %v, want %v from now", what, s.tokenExpiresAt, nativeAccessTTL)
	}
}

// withNativeSessionFlag adds "session":"native" to a sign-in's JSON body: the
// field an app in a native shell sends to ask for a native session, and so for
// a refresh token (wm-nybvk).
func withNativeSessionFlag(t *testing.T, body string) string {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatalf("decode the sign-in body: %v", err)
	}
	fields["session"] = "native"
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("encode the sign-in body: %v", err)
	}
	return string(encoded)
}

func signInNativelyThroughTheDoor(t *testing.T, srv *httptest.Server, door *bootDoor, email, subject, name string) nativeSession {
	t.Helper()
	body := withNativeSessionFlag(t, door.assertionBodyFor(t, email, subject, name))
	answer := serveCall(t, srv, http.MethodPost, "/api/auth/assert", body, nil)
	return decodeNativeSession(t, answer, "POST /api/auth/assert for "+email)
}

// requireNoNativeSessionFields checks that a 200 sign-in answer carries a token
// and none of the fields a native session adds.
func requireNoNativeSessionFields(t *testing.T, answer serveAnswer, what string) {
	t.Helper()
	if answer.status != http.StatusOK && answer.status != http.StatusCreated {
		t.Fatalf("%s answered %d code %q, want 200", what, answer.status, refusalCode(answer.body))
	}
	var decoded struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &decoded); err != nil {
		t.Fatalf("decode %s: %v", what, err)
	}
	if _, ok := decoded.Data["token"]; !ok {
		t.Fatalf("%s carries no token; a browser sign-in still gets one", what)
	}
	for _, field := range []string{"refresh_token", "refresh_token_expires_at", "token_expires_at"} {
		if _, ok := decoded.Data[field]; ok {
			t.Errorf("%s carries %s; only a sign-in that asks for a native session gets it", what, field)
		}
	}
}

// renewNativeSession presents refreshToken at the renewal route, as an app in a
// native shell does: no cookie and no Authorization header.
func renewNativeSession(t *testing.T, srv *httptest.Server, refreshToken string) serveAnswer {
	t.Helper()
	body, err := json.Marshal(map[string]string{"refresh_token": refreshToken})
	if err != nil {
		t.Fatalf("encode the renewal: %v", err)
	}
	return serveCall(t, srv, http.MethodPost, "/api/auth/refresh", string(body), nil)
}

// requireRenewalRefused checks that a renewal answered 401 with code.
func requireRenewalRefused(t *testing.T, answer serveAnswer, code, what string) {
	t.Helper()
	if answer.status != http.StatusUnauthorized || refusalCode(answer.body) != code {
		t.Fatalf("%s answered %d code %q, want 401 %s", what, answer.status, refusalCode(answer.body), code)
	}
	if strings.Contains(answer.body, `"refresh_token"`) || strings.Contains(answer.body, `"token"`) {
		t.Fatalf("%s was refused but its answer carries a token", what)
	}
}

// carriesTheConnectorMark reports whether the access token's claims carry
// token_use, the mark /oauth/token puts on a connector's token.
func carriesTheConnectorMark(t *testing.T, token string) bool {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the access token has %d parts, want 3", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode the access token's claims: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("read the access token's claims: %v", err)
	}
	_, marked := claims["token_use"]
	return marked
}

// Every sign-in that asks for a native session — the door's assertion,
// registration and the password — hands back a refresh token beside the access
// token, with the lifetime of each.
func TestServe_NativeSignInsHandBackARefreshTokenBesideTheToken(t *testing.T) {
	door := newBootDoor(t)
	srv := bootServe(t, connectorConfig(door))

	start := time.Now()
	byDoor := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
	requireBothTokens(t, byDoor, start, "the door's sign-in")

	start = time.Now()
	registered := decodeNativeSession(t, serveCall(t, srv, http.MethodPost, "/api/auth/register",
		withNativeSessionFlag(t, `{"email":"`+bootPasswordEmail+`","password":"`+bootPasswordSecret+`","display_name":"Rosa Calder"}`), nil),
		"the registration")
	requireBothTokens(t, registered, start, "the registration")

	start = time.Now()
	byPassword := decodeNativeSession(t, serveCall(t, srv, http.MethodPost, "/api/auth/password-login",
		withNativeSessionFlag(t, `{"email":"`+bootPasswordEmail+`","password":"`+bootPasswordSecret+`"}`), nil), "the password sign-in")
	requireBothTokens(t, byPassword, start, "the password sign-in")
}

// A sign-in that does not ask for a native session — a browser's — answers as
// it did before refresh tokens existed: a token, and no refresh token and
// neither expiry that goes with one (wm-nybvk). A session value other than
// "native" is a browser's too.
func TestServe_ABrowserSignInHandsBackNoRefreshToken(t *testing.T) {
	door := newBootDoor(t)
	srv := bootServe(t, connectorConfig(door))

	requireNoNativeSessionFields(t, serveCall(t, srv, http.MethodPost, "/api/auth/assert",
		door.assertionBodyFor(t, bootOwnerEmail, bootOwnerSubject, bootOwnerName), nil), "the door's browser sign-in")
	requireNoNativeSessionFields(t, serveCall(t, srv, http.MethodPost, "/api/auth/register",
		`{"email":"`+bootPasswordEmail+`","password":"`+bootPasswordSecret+`","display_name":"Rosa Calder"}`, nil),
		"a browser registration")
	requireNoNativeSessionFields(t, serveCall(t, srv, http.MethodPost, "/api/auth/password-login",
		`{"email":"`+bootPasswordEmail+`","password":"`+bootPasswordSecret+`"}`, nil), "a browser password sign-in")
	requireNoNativeSessionFields(t, serveCall(t, srv, http.MethodPost, "/api/auth/password-login",
		`{"email":"`+bootPasswordEmail+`","password":"`+bootPasswordSecret+`","session":"web"}`, nil),
		"a password sign-in naming a session other than native")
}

// A renewal hands back a new access token for the same person and account, and
// a new refresh token in place of the one presented. The new access token is a
// native token: it reaches the protected API, which refuses a connector's. A
// refresh token is good once; presenting a rotated one again revokes its whole
// family, so the token rotated from it is refused too, while a session begun
// by another sign-in is untouched.
func TestServe_ARefreshTokenRenewsTheNativeSessionOnce(t *testing.T) {
	door := newBootDoor(t)
	srv := bootServe(t, trustedIssuerConfig(door))
	signIn := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)

	start := time.Now()
	first := decodeNativeSession(t, renewNativeSession(t, srv, signIn.refreshToken), "the first renewal")
	requireBothTokens(t, first, start, "the first renewal")
	if first.refreshToken == signIn.refreshToken {
		t.Fatal("the renewal handed back the refresh token it was given, not a new one")
	}
	if first.accountID != signIn.accountID {
		t.Fatalf("the renewal names account %q, the sign-in %q", first.accountID, signIn.accountID)
	}
	if carriesTheConnectorMark(t, first.token) {
		t.Fatal("the renewed access token carries the connector mark; a native token must not")
	}
	if got := serveRequest(t, srv, http.MethodGet, "/api/resource-types", "", first.token, nil); got.status != http.StatusOK {
		t.Fatalf("GET /api/resource-types with the renewed token answered %d code %q, want 200", got.status, refusalCode(got.body))
	}
	if me := serveRequest(t, srv, http.MethodGet, "/api/auth/me", "", first.token, nil); me.status != http.StatusOK ||
		!strings.Contains(me.body, bootOwnerEmail) {
		t.Fatalf("GET /api/auth/me with the renewed token answered %d, want 200 naming %s", me.status, bootOwnerEmail)
	}

	second := decodeNativeSession(t, renewNativeSession(t, srv, first.refreshToken), "the second renewal")

	requireRenewalRefused(t, renewNativeSession(t, srv, signIn.refreshToken), "invalid_refresh_token",
		"presenting the sign-in's refresh token after it was rotated")
	requireRenewalRefused(t, renewNativeSession(t, srv, second.refreshToken), "invalid_refresh_token",
		"the newest refresh token of a family whose rotated token was presented again")

	other := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
	decodeNativeSession(t, renewNativeSession(t, srv, other.refreshToken), "a renewal of a session begun by another sign-in")

	requireRenewalRefused(t, renewNativeSession(t, srv, "not-a-refresh-token-this-instance-issued"), "invalid_refresh_token",
		"a refresh token the instance never issued")
	if missing := serveCall(t, srv, http.MethodPost, "/api/auth/refresh", `{}`, nil); missing.status != http.StatusBadRequest {
		t.Fatalf("a renewal with no refresh token answered %d, want 400", missing.status)
	}
}

// A renewal refuses what the token path refuses, with the code it gives: a
// person no longer in the account, a suspended account, and a deleted one. An
// account whose deletion did not finish renews only for an owner or admin, for
// the deletion alone, as a sign-in to it does. A connector's refresh token does
// not renew a native session, and a native refresh token gets nothing from
// /oauth/token.
func TestServe_ARenewalRefusesWhatTheTokenPathRefuses(t *testing.T) {
	door := newBootDoor(t)
	var accounts authrepos.AccountRepository
	var locks repositories.AccountErasureLocks
	srv := bootServe(t, connectorConfig(door), fx.Populate(&accounts, &locks))
	ctx := context.Background()

	people := []struct{ email, subject, name string }{
		{"ines.moreau@harborlegal.example", "301234567801", "Ines Moreau"},
		{"theo.brandt@harborlegal.example", "301234567802", "Theo Brandt"},
		{"amara.okafor@harborlegal.example", "301234567803", "Amara Okafor"},
		{"lucia.ferro@harborlegal.example", "301234567804", "Lucia Ferro"},
		{"jonas.weber@harborlegal.example", "301234567805", "Jonas Weber"},
	}
	person := func(i int) nativeSession {
		t.Helper()
		p := people[i]
		return signInNativelyThroughTheDoor(t, srv, door, p.email, p.subject, p.name)
	}

	t.Run("a person removed from the account", func(t *testing.T) {
		p := person(0)
		if err := accounts.RemoveMember(ctx, p.accountID, p.agentID); err != nil {
			t.Fatalf("remove the person: %v", err)
		}
		requireRenewalRefused(t, renewNativeSession(t, srv, p.refreshToken), "account_access_revoked",
			"a renewal for a person removed from the account")
		// The refusal revoked the refresh token: adding the person back does not
		// revive it. A new sign-in is the way back in.
		if err := accounts.SaveMember(ctx, p.accountID, p.agentID, authentities.RoleOwner); err != nil {
			t.Fatalf("add the person back: %v", err)
		}
		requireRenewalRefused(t, renewNativeSession(t, srv, p.refreshToken), "invalid_refresh_token",
			"the refused refresh token, presented again after the person was added back")
	})

	t.Run("a suspended account", func(t *testing.T) {
		p := person(1)
		account, err := accounts.FindByID(ctx, p.accountID)
		if err != nil || account == nil {
			t.Fatalf("read the account: %v", err)
		}
		if err := account.Deactivate(); err != nil {
			t.Fatalf("suspend the account: %v", err)
		}
		if err := accounts.Save(ctx, account); err != nil {
			t.Fatalf("save the suspended account: %v", err)
		}
		requireRenewalRefused(t, renewNativeSession(t, srv, p.refreshToken), "account_deactivated",
			"a renewal for a suspended account")
	})

	t.Run("a deleted account", func(t *testing.T) {
		p := person(2)
		if got := serveRequest(t, srv, http.MethodDelete, "/api/account", confirmDeletion, p.token, nil); got.status != http.StatusOK {
			t.Fatalf("DELETE /api/account with the token answered %d code %q, want 200", got.status, refusalCode(got.body))
		}
		requireRenewalRefused(t, renewNativeSession(t, srv, p.refreshToken), "invalid_refresh_token",
			"a renewal for a deleted account")
	})

	t.Run("an owner of an account whose deletion did not finish", func(t *testing.T) {
		p := person(3)
		lockForErasure(t, accounts, locks, p.accountID, p.agentID)
		renewed := decodeNativeSession(t, renewNativeSession(t, srv, p.refreshToken), "a renewal for a locked account's owner")
		if !renewed.erasurePending || renewed.code != "account_erasure_pending" || renewed.accountID != p.accountID {
			t.Fatalf("the renewal answered erasure_pending=%v code=%q for the locked account %v, want account_erasure_pending",
				renewed.erasurePending, renewed.code, renewed.accountID == p.accountID)
		}
		if got := serveRequest(t, srv, http.MethodGet, "/api/resource-types", "", renewed.token, nil); got.status != http.StatusUnauthorized ||
			refusalCode(got.body) != "account_erasure_pending" {
			t.Fatalf("GET /api/resource-types with the renewed token answered %d code %q, want 401 account_erasure_pending",
				got.status, refusalCode(got.body))
		}
		if got := serveRequest(t, srv, http.MethodDelete, "/api/account", confirmDeletion, renewed.token, nil); got.status != http.StatusOK {
			t.Fatalf("DELETE /api/account with the renewed token answered %d code %q, want 200 finishing the deletion",
				got.status, refusalCode(got.body))
		}
	})

	t.Run("a plain member of an account whose deletion did not finish", func(t *testing.T) {
		p := person(4)
		if err := accounts.SaveMember(ctx, p.accountID, p.agentID, authentities.RoleMember); err != nil {
			t.Fatalf("make the person a plain member: %v", err)
		}
		lockForErasure(t, accounts, locks, p.accountID, p.agentID)
		requireRenewalRefused(t, renewNativeSession(t, srv, p.refreshToken), "account_erasure_pending",
			"a renewal for a plain member of a locked account")
	})

	t.Run("a connector's refresh token, and a native one at the token endpoint", func(t *testing.T) {
		owner := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		browser := serveCall(t, srv, http.MethodPost, "/api/auth/assert",
			door.assertionBodyFor(t, bootOwnerEmail, bootOwnerSubject, bootOwnerName), nil)
		connector := connectThroughOAuth(t, srv, browser.cookies)
		requireRenewalRefused(t, renewNativeSession(t, srv, connector.refreshToken), "invalid_refresh_token",
			"a renewal with a connector's refresh token")

		form := url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", owner.refreshToken)
		form.Set("client_id", "weos-native")
		got := postTokenForm(t, srv, form)
		if got.status != http.StatusBadRequest || oauthError(got.body) != "invalid_grant" {
			t.Fatalf("POST /oauth/token with a native refresh token answered %d %q, want 400 invalid_grant", got.status, oauthError(got.body))
		}
		// The token endpoint left it alone: the native session still renews.
		decodeNativeSession(t, renewNativeSession(t, srv, owner.refreshToken), "a renewal after the token endpoint refused the token")
	})
}

// A native app signs out with the token it holds, or with its refresh token in
// the body. Either ends every native refresh token the person holds, so no
// device of theirs renews afterwards. The access token itself is stateless and
// lasts out its hour (see TestServe_SignOutDoesNotEndTheTokenTheSignInIssued).
func TestServe_ANativeSignOutEndsThePersonsRefreshTokens(t *testing.T) {
	door := newBootDoor(t)
	srv := bootServe(t, trustedIssuerConfig(door))

	t.Run("with the access token", func(t *testing.T) {
		phone := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		tablet := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		if got := serveRequest(t, srv, http.MethodPost, "/api/auth/logout", "", phone.token, nil); got.status != http.StatusOK {
			t.Fatalf("POST /api/auth/logout with the token answered %d, want 200", got.status)
		}
		requireRenewalRefused(t, renewNativeSession(t, srv, phone.refreshToken), "invalid_refresh_token",
			"a renewal after the app signed out with its token")
		requireRenewalRefused(t, renewNativeSession(t, srv, tablet.refreshToken), "invalid_refresh_token",
			"a renewal on the person's other device after they signed out")
	})

	t.Run("with the refresh token", func(t *testing.T) {
		app := signInNativelyThroughTheDoor(t, srv, door, bootMemberEmail, bootMemberSubject, bootMemberName)
		body, err := json.Marshal(map[string]string{"refresh_token": app.refreshToken})
		if err != nil {
			t.Fatalf("encode the sign-out: %v", err)
		}
		if got := serveCall(t, srv, http.MethodPost, "/api/auth/logout", string(body), nil); got.status != http.StatusOK {
			t.Fatalf("POST /api/auth/logout with the refresh token answered %d, want 200", got.status)
		}
		requireRenewalRefused(t, renewNativeSession(t, srv, app.refreshToken), "invalid_refresh_token",
			"a renewal after the app signed out with its refresh token")
	})
}
