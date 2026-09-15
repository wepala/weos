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
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/internal/config"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"go.uber.org/fx"
)

// wm-8i8ln. The protected API and the account deletion take the token a
// native sign-in hands back. They do not take the token a third-party
// connector got from /oauth/token: that token is for the MCP and agent routes
// the person agreed to connect, and a leaked one must not be able to delete
// the account. These tests boot serve's own routes and get the connector's
// token the way a connector does. No failure message prints a token.

const (
	connectorRedirectURI = "https://notes.example/oauth/callback"
	connectorVerifier    = "a-proof-key-verifier-long-enough-to-satisfy-pkce-0123456789"
	bootPasswordEmail    = "rosa.calder@harborlegal.example"
	bootPasswordSecret   = "correct horse battery staple 42"
)

// connectorGrant is what a connector holds after it is connected.
type connectorGrant struct {
	clientID     string
	accessToken  string
	refreshToken string
}

// connectorConfig is trustedIssuerConfig with dynamic client registration and
// password sign-in turned on, so a connector can register itself and a person
// can sign in with a password.
func connectorConfig(door *bootDoor) config.Config {
	cfg := trustedIssuerConfig(door)
	cfg.OAuth.DynamicRegistration = true
	cfg.PasswordAuthEnabled = true
	cfg.PasswordRegistrationEnabled = true
	return cfg
}

// connectThroughOAuth registers a connector, authorizes it with the browser
// session in cookies, and exchanges the code for its tokens.
func connectThroughOAuth(t *testing.T, srv *httptest.Server, cookies []*http.Cookie) connectorGrant {
	t.Helper()
	registered := serveCall(t, srv, http.MethodPost, "/oauth/register",
		`{"client_name":"Harbor Notes","redirect_uris":["`+connectorRedirectURI+`"],"grant_types":["authorization_code","refresh_token"]}`, nil)
	var client struct {
		ClientID string `json:"client_id"`
	}
	if registered.status != http.StatusCreated || json.Unmarshal([]byte(registered.body), &client) != nil || client.ClientID == "" {
		t.Fatalf("POST /oauth/register answered %d %s, want 201 with a client id", registered.status, registered.body)
	}

	sum := sha256.Sum256([]byte(connectorVerifier))
	q := url.Values{}
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", connectorRedirectURI)
	q.Set("response_type", "code")
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("state", "st-8i8ln")
	q.Set("scope", "mcp:read mcp:write")
	authorized := serveCall(t, srv, http.MethodGet, "/oauth/authorize?"+q.Encode(), "", cookies)
	location, err := url.Parse(authorized.header.Get("Location"))
	if authorized.status != http.StatusFound || err != nil || location.Query().Get("code") == "" {
		t.Fatalf("GET /oauth/authorize with the session answered %d, want a redirect carrying a code", authorized.status)
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", location.Query().Get("code"))
	form.Set("redirect_uri", connectorRedirectURI)
	form.Set("client_id", client.ClientID)
	form.Set("code_verifier", connectorVerifier)
	exchanged := postTokenForm(t, srv, form)
	grant := decodeTokenAnswer(t, exchanged, "the code exchange")
	grant.clientID = client.ClientID
	return grant
}

// oauthError is the error an OAuth endpoint answered with, or "" for a body
// that carries none. A failure message prints it rather than the body, which
// on success carries tokens.
func oauthError(body string) string {
	var answer struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(body), &answer) != nil {
		return ""
	}
	return answer.Error
}

// refreshConnector presents the connector's refresh token.
func refreshConnector(t *testing.T, srv *httptest.Server, grant connectorGrant) serveAnswer {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", grant.refreshToken)
	form.Set("client_id", grant.clientID)
	return postTokenForm(t, srv, form)
}

func postTokenForm(t *testing.T, srv *httptest.Server, form url.Values) serveAnswer {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/oauth/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build POST /oauth/token: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return serveSend(t, req)
}

func decodeTokenAnswer(t *testing.T, answer serveAnswer, what string) connectorGrant {
	t.Helper()
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if answer.status != http.StatusOK || json.Unmarshal([]byte(answer.body), &tokens) != nil ||
		tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("%s answered %d, want 200 with an access and a refresh token", what, answer.status)
	}
	return connectorGrant{accessToken: tokens.AccessToken, refreshToken: tokens.RefreshToken}
}

// signInWithPassword registers a person with a password and signs them in,
// and answers with the token and account the sign-in handed back.
func signInWithPassword(t *testing.T, srv *httptest.Server, email, password string) doorSignIn {
	t.Helper()
	registered := serveCall(t, srv, http.MethodPost, "/api/auth/register",
		`{"email":"`+email+`","password":"`+password+`","display_name":"Rosa Calder"}`, nil)
	if registered.status != http.StatusOK && registered.status != http.StatusCreated {
		t.Fatalf("POST /api/auth/register for %s answered %d %s", email, registered.status, registered.body)
	}
	return passwordLogin(t, srv, email, password)
}

// passwordLogin signs a registered person in with their password.
func passwordLogin(t *testing.T, srv *httptest.Server, email, password string) doorSignIn {
	t.Helper()
	answer := serveCall(t, srv, http.MethodPost, "/api/auth/password-login",
		`{"email":"`+email+`","password":"`+password+`"}`, nil)
	if answer.status != http.StatusOK {
		t.Fatalf("POST /api/auth/password-login for %s answered %d %s", email, answer.status, answer.body)
	}
	var decoded struct {
		Data struct {
			Token string `json:"token"`
			Agent struct {
				ID string `json:"id"`
			} `json:"agent"`
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &decoded); err != nil {
		t.Fatalf("decode the password sign-in for %s: %v", email, err)
	}
	return doorSignIn{
		token:     decoded.Data.Token,
		cookies:   answer.cookies,
		agentID:   decoded.Data.Agent.ID,
		accountID: decoded.Data.Account.ID,
	}
}

// A connector's token answers on the MCP route and is refused on the protected
// API and on the deletion, where a native sign-in's token answers. The refusal
// is 403 insufficient_scope with the code token_not_allowed: the token is
// valid, so a 401 invalid_token would only send a connector to refresh it and
// try again, for a token that refreshing can never make acceptable.
func TestServe_AConnectorsTokenIsRefusedOnTheAccountAPIButNotOnMCP(t *testing.T) {
	door := newBootDoor(t)
	srv := bootServe(t, connectorConfig(door))
	owner := signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
	connector := connectThroughOAuth(t, srv, owner.cookies)
	refreshed := decodeTokenAnswer(t, refreshConnector(t, srv, connector), "the refresh grant")

	for _, held := range []struct{ name, token string }{
		{"the code exchange's token", connector.accessToken},
		{"the refreshed token", refreshed.accessToken},
	} {
		name, token := held.name, held.token
		if got := bearerMCPCall(t, srv, token); got.status != http.StatusOK {
			t.Errorf("POST /api/mcp with %s answered %d, want 200", name, got.status)
		}
		for _, call := range []struct{ method, path, body string }{
			{http.MethodGet, "/api/resource-types", ""},
			{http.MethodGet, "/api/notifications", ""},
			{http.MethodPost, "/api/invites", `{"email":"new.member@harborlegal.example","role":"admin"}`},
			{http.MethodDelete, "/api/account", confirmDeletion},
		} {
			got := serveRequest(t, srv, call.method, call.path, call.body, token, nil)
			if got.status != http.StatusForbidden || refusalCode(got.body) != "token_not_allowed" {
				// The body is not printed: an invite's answer carries its token.
				t.Errorf("%s %s with %s answered %d code %q, want 403 token_not_allowed", call.method, call.path, name, got.status, refusalCode(got.body))
			}
			if challenge := got.header.Get("WWW-Authenticate"); !strings.Contains(challenge, `error="insufficient_scope"`) {
				t.Errorf("%s %s with %s carries the challenge %q, want insufficient_scope", call.method, call.path, name, challenge)
			}
		}
	}

	// The deletion was refused, so the account is still there: the owner's
	// native token, which carries no mark and so stands for every token issued
	// before connectors' tokens were marked, still answers.
	if got := serveRequest(t, srv, http.MethodGet, "/api/resource-types", "", owner.token, nil); got.status != http.StatusOK {
		t.Fatalf("GET /api/resource-types with the door's token answered %d %s, want 200", got.status, got.body)
	}

	// A password sign-in's token is a native token too.
	rosa := signInWithPassword(t, srv, bootPasswordEmail, bootPasswordSecret)
	if rosa.token == "" {
		t.Fatal("the password sign-in handed back no token")
	}
	if got := serveRequest(t, srv, http.MethodGet, "/api/resource-types", "", rosa.token, nil); got.status != http.StatusOK {
		t.Errorf("GET /api/resource-types with the password sign-in's token answered %d %s, want 200", got.status, got.body)
	}
	if got := bearerMCPCall(t, srv, rosa.token); got.status != http.StatusOK {
		t.Errorf("POST /api/mcp with the password sign-in's token answered %d %s, want 200", got.status, got.body)
	}
	if got := serveRequest(t, srv, http.MethodDelete, "/api/account", confirmDeletion, rosa.token, nil); got.status != http.StatusOK {
		t.Errorf("DELETE /api/account with the password sign-in's token answered %d %s, want 200", got.status, got.body)
	}
	if got := serveRequest(t, srv, http.MethodDelete, "/api/account", confirmDeletion, owner.token, nil); got.status != http.StatusOK {
		t.Errorf("DELETE /api/account with the door's token answered %d %s, want 200", got.status, got.body)
	}
}

// wm-mo1bp. A person removed from the account a connector was authorized for
// is refused by every route the connector calls, and each refusal tells the
// connector its token is invalid, so it refreshes. The refresh grant must not
// hand it another token for that account: it answers invalid_grant, which ends
// the loop, and revokes the refresh token, so the same token stays refused even
// if the person is added back.
func TestServe_TheRefreshGrantRefusesAPersonRemovedFromTheAccount(t *testing.T) {
	door := newBootDoor(t)
	var accounts authrepos.AccountRepository
	srv := bootServe(t, connectorConfig(door), fx.Populate(&accounts))
	owner := signInThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
	connector := connectThroughOAuth(t, srv, owner.cookies)

	// A member still in the account refreshes as before.
	rotated := decodeTokenAnswer(t, refreshConnector(t, srv, connector), "the refresh grant for a member")
	rotated.clientID = connector.clientID

	ctx := context.Background()
	if err := accounts.RemoveMember(ctx, owner.accountID, owner.agentID); err != nil {
		t.Fatalf("remove the person from the account: %v", err)
	}
	refused := refreshConnector(t, srv, rotated)
	if refused.status != http.StatusBadRequest || !strings.Contains(refused.body, `"invalid_grant"`) {
		t.Fatalf("the refresh grant for a removed person answered %d %q, want 400 invalid_grant", refused.status, oauthError(refused.body))
	}

	// The refusal revoked the refresh token: with the membership restored it
	// is still refused.
	if err := accounts.SaveMember(ctx, owner.accountID, owner.agentID, authentities.RoleOwner); err != nil {
		t.Fatalf("add the person back: %v", err)
	}
	again := refreshConnector(t, srv, rotated)
	if again.status != http.StatusBadRequest || !strings.Contains(again.body, `"invalid_grant"`) {
		t.Fatalf("the refused refresh token, presented again after the person was added back, answered %d %q, want 400 invalid_grant", again.status, oauthError(again.body))
	}
}
