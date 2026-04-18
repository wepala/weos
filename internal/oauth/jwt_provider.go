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

// ProvideJWTService creates a pericarp RSAJWTService configured for MCP OAuth.
// When JWTSigningKey is empty or "auto", an ephemeral RSA key is generated
// (tokens become invalid across restarts — suitable for development only).
// For production, provide a PEM-encoded RSA private key.
//
// When OAuth is not enabled, this returns a service with an ephemeral key
// regardless of the JWT_SIGNING_KEY value, so a malformed key in an
// OAuth-disabled deployment doesn't prevent server startup.
func ProvideJWTService(cfg config.Config) (authapp.JWTService, error) {
	keyConfig := cfg.OAuth.JWTSigningKey
	if !cfg.OAuthEnabled() {
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
