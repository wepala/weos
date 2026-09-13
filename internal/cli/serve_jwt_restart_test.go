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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"go.uber.org/fx"
)

// bootedServe is one run of serve over a database that outlives it, so a
// second run over the same directory is a restart.
type bootedServe struct {
	srv     *httptest.Server
	app     *fx.App
	stopped bool
}

func (b *bootedServe) stop(t *testing.T) {
	t.Helper()
	if b.stopped {
		return
	}
	b.stopped = true
	b.srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := b.app.Stop(ctx); err != nil {
		t.Logf("stop the application: %v", err)
	}
}

// bootServeOn starts serve's application and routes for cfg on the database in
// dir.
func bootServeOn(t *testing.T, cfg config.Config, dir string, extra ...fx.Option) *bootedServe {
	t.Helper()
	cfg.DatabaseDSN = filepath.Join(dir, "weos.db")
	cfg.Storage.LocalPath = filepath.Join(dir, "uploads")
	cfg.LogLevel = "error"
	e, app, err := buildServer(cfg, extra...)
	if err != nil {
		t.Fatalf("build the server: %v", err)
	}
	b := &bootedServe{srv: httptest.NewServer(e), app: app}
	t.Cleanup(func() { b.stop(t) })
	return b
}

func testJWTSigningKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate the signing key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// bearerMCPCall posts an MCP request carrying token and answers with the status
// and body.
func bearerMCPCall(t *testing.T, srv *httptest.Server, token string) serveAnswer {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/api/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatalf("build the MCP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST /api/mcp: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read POST /api/mcp: %v", err)
	}
	return serveAnswer{status: resp.StatusCode, body: strings.TrimSpace(string(raw))}
}

// A fleet instance whose only sign-in is the door is stopped when idle and
// started on demand. The bearer tokens its authorization server gave MCP
// connectors must survive that, as long as JWT_SIGNING_KEY is set; with no key
// every restart logs every connector out, as it always has.
func TestServe_BearerTokenIssuedBeforeARestartStillWorksWithTheSameSigningKey(t *testing.T) {
	door := newBootDoor(t)
	key := testJWTSigningKey(t)
	cases := map[string]struct {
		signingKey string
		survives   bool
	}{
		"JWT_SIGNING_KEY set": {key, true},
		"no JWT_SIGNING_KEY":  {"", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			cfg.SessionSecret = bootOwnSecret
			cfg.TrustedIssuer = door.settings()
			cfg.OAuth.JWTSigningKey = c.signingKey
			if cfg.OAuthEnabled() || cfg.PasswordAuthEnabled {
				t.Fatalf("the instance must have no OAuth provider and no password sign-in")
			}
			dir := t.TempDir()

			var jwtService authapp.JWTService
			var agents authrepos.AgentRepository
			var accounts authrepos.AccountRepository
			first := bootServeOn(t, cfg, dir, fx.Populate(&jwtService, &agents, &accounts))
			token := issueBearerTokenThroughTheDoor(t, first.srv, door, jwtService, agents, accounts)
			if got := bearerMCPCall(t, first.srv, token); got.status == http.StatusUnauthorized {
				t.Fatalf("the token is refused before any restart: %d %s", got.status, got.body)
			}
			first.stop(t)

			second := bootServeOn(t, cfg, dir)
			got := bearerMCPCall(t, second.srv, token)
			if c.survives && got.status == http.StatusUnauthorized {
				t.Fatalf("a token issued before the restart is refused after it, with the same JWT_SIGNING_KEY: %d %s", got.status, got.body)
			}
			if !c.survives && (got.status != http.StatusUnauthorized || !strings.Contains(got.body, "invalid_token")) {
				t.Fatalf("with no JWT_SIGNING_KEY a token from before the restart answered %d %s, want 401 invalid_token", got.status, got.body)
			}
		})
	}
}

// issueBearerTokenThroughTheDoor signs a person in with the door's assertion
// and issues the bearer token serve's authorization server would give that
// person's MCP connector, from serve's own JWT service.
func issueBearerTokenThroughTheDoor(
	t *testing.T,
	srv *httptest.Server,
	door *bootDoor,
	jwtService authapp.JWTService,
	agents authrepos.AgentRepository,
	accounts authrepos.AccountRepository,
) string {
	t.Helper()
	signIn := serveCall(t, srv, http.MethodPost, "/api/auth/assert", door.assertionBody(t, bootOwnerEmail), nil)
	if signIn.status != http.StatusOK {
		t.Fatalf("POST /api/auth/assert answered %d %s, want 200", signIn.status, signIn.body)
	}
	var answer struct {
		Data struct {
			Agent struct {
				ID string `json:"id"`
			} `json:"agent"`
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(signIn.body), &answer); err != nil || answer.Data.Agent.ID == "" || answer.Data.Account.ID == "" {
		t.Fatalf("the sign-in answer names no person and account: %v (%s)", err, signIn.body)
	}
	ctx := context.Background()
	agent, err := agents.FindByID(ctx, answer.Data.Agent.ID)
	if err != nil || agent == nil {
		t.Fatalf("read the person: %v", err)
	}
	account, err := accounts.FindByID(ctx, answer.Data.Account.ID)
	if err != nil || account == nil {
		t.Fatalf("read the account: %v", err)
	}
	token, err := jwtService.IssueToken(ctx, agent, []*authentities.Account{account}, account.GetID(), nil, nil)
	if err != nil {
		t.Fatalf("issue the bearer token: %v", err)
	}
	return token
}
