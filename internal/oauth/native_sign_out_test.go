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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	gojwt "github.com/golang-jwt/jwt/v5"
)

// nativeSignOutTestKey is a fresh RSA key for a test that signs its own tokens.
func nativeSignOutTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	mustNoErr(t, err, "generate an RSA key")
	return key
}

// signNativeSignOutToken signs a native session's claims with key, expiring at
// exp.
func signNativeSignOutToken(t *testing.T, key *rsa.PrivateKey, exp time.Time) string {
	t.Helper()
	signed, err := gojwt.NewWithClaims(gojwt.SigningMethodRS256, gojwt.MapClaims{
		"agent_id":          "agent-ops",
		"active_account_id": "acct-harbor",
		NativeSessionClaim:  "family-phone",
		"iat":               exp.Add(-time.Hour).Unix(),
		"exp":               exp.Unix(),
	}).SignedString(key)
	mustNoErr(t, err, "sign the token")
	return signed
}

// An app whose hour has passed signs out with the token it holds. The token no
// longer validates, but its signature still proves this instance issued it, so
// the session it names can be ended (wm-ehtnq). A token another key signed, or
// no token at all, names nothing.
func TestSignedClaimsIgnoringExpiry_ReadsAnExpiredTokenThisInstanceSigned(t *testing.T) {
	ctx := context.Background()
	key := nativeSignOutTestKey(t)
	cfg := config.Default()
	cfg.PasswordAuthEnabled = true
	cfg.OAuth.JWTSigningKey = string(pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	svc, err := ProvideJWTService(cfg)
	mustNoErr(t, err, "build the instance's token service")

	expired := signNativeSignOutToken(t, key, time.Now().Add(-time.Minute))
	if _, err := svc.ValidateToken(ctx, expired); !errors.Is(err, authapp.ErrTokenExpired) {
		t.Fatalf("validating the expired token returned %v, want ErrTokenExpired", err)
	}
	claims, err := SignedClaimsIgnoringExpiry(svc, expired)
	mustNoErr(t, err, "read the expired token this instance signed")
	if claims.AgentID != "agent-ops" || NativeSessionOf(claims) != "family-phone" {
		t.Fatalf("read agent %q and session %q, want agent-ops and family-phone", claims.AgentID, NativeSessionOf(claims))
	}

	forged := signNativeSignOutToken(t, nativeSignOutTestKey(t), time.Now().Add(-time.Minute))
	if _, err := SignedClaimsIgnoringExpiry(svc, forged); err == nil {
		t.Fatal("an expired token another key signed was read as this instance's")
	}
	if _, err := SignedClaimsIgnoringExpiry(svc, "not-a-token"); err == nil {
		t.Fatal("a string that is not a token was read as one")
	}
	if _, err := SignedClaimsIgnoringExpiry(nil, expired); err == nil {
		t.Fatal("a token was read with no token service to check its signature")
	}
}
