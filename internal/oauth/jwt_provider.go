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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authjwt "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/jwt"
)

const (
	defaultAccessTokenTTL = 1 * time.Hour
	rsaKeyBits            = 2048
)

// ProvideJWTService creates the pericarp RSAJWTService that signs and checks
// the authorization server's access tokens: the bearer tokens MCP connectors
// present at /api/mcp and /api/agent/*.
//
// JWT_SIGNING_KEY is honored whenever the instance has any sign-in
// (config.Config.AuthEnabled: an OAuth provider, password sign-in, or a
// trusted issuer), because every such instance mounts the authorization
// server and hands out tokens. A PEM-encoded RSA private key keeps those
// tokens valid across a restart, an idle-stop wake and replicas; empty or
// "auto" generates an ephemeral key, so every token dies with the process —
// suitable for development only. A malformed key on such an instance stops
// boot rather than silently logging every connector out at the next restart.
//
// An instance with no sign-in mounts no authorization server and issues no
// tokens, so there the key is ignored and an ephemeral one is used: a
// malformed JWT_SIGNING_KEY left in a development environment does not stop
// the server starting.
func ProvideJWTService(cfg config.Config) (authapp.JWTService, error) {
	keyConfig := cfg.OAuth.JWTSigningKey
	if !cfg.AuthEnabled() {
		keyConfig = "auto"
	}
	key, err := loadOrGenerateKey(keyConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to load JWT signing key: %w", err)
	}

	issuer := cfg.OAuth.BaseURL
	if issuer == "" {
		host := cfg.Server.Host
		// Wildcard bind hosts aren't valid issuers; map to localhost.
		if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
			host = "localhost"
		}
		// net.JoinHostPort handles IPv6 bracketing correctly.
		issuer = "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Server.Port))
	}
	// Normalize to match discovery handlers (which trim trailing slashes).
	issuer = strings.TrimRight(issuer, "/")

	return authjwt.NewRSAJWTService(
		authjwt.WithSigningKey(key),
		authjwt.WithTokenTTL(defaultAccessTokenTTL),
		authjwt.WithIssuer(issuer),
	), nil
}

func loadOrGenerateKey(keyConfig string) (*rsa.PrivateKey, error) {
	if keyConfig == "" || keyConfig == "auto" {
		return rsa.GenerateKey(rand.Reader, rsaKeyBits)
	}
	return parseRSAPrivateKeyPEM([]byte(keyConfig))
}

func parseRSAPrivateKeyPEM(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in JWT signing key")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is not RSA")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}
